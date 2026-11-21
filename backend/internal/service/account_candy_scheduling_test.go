//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCandySchedulingOnlyWrongAnswersBlock(t *testing.T) {
	account := automaticCandyAccount()
	repo := &candyAccountRepo{account: account}
	upstream := &queuedHTTPUpstream{}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	for _, step := range []struct {
		name, answer, verdict string
		status                int
		blocked               bool
	}{
		{"initial API error", "unavailable", "", 503, false},
		{"wrong answer", "34", "failed", 200, true},
		{"API error after wrong answer", "unavailable", "failed", 500, true},
		{"passing retest", "最少 21 个", "passed", 200, false},
		{"API error after passing", "unavailable", "passed", 502, false},
	} {
		t.Run(step.name, func(t *testing.T) {
			response := newJSONResponse(step.status, step.answer)
			if step.status == 200 {
				response = candyResponse(step.answer)
			}
			upstream.responses = []*http.Response{response}
			result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
			require.NoError(t, err)
			require.Equal(t, step.verdict, result.LastVerdict)
			require.Equal(t, step.blocked, account.Extra[CandyTestBlockedExtraKey])
			require.Equal(t, step.blocked, account.IsCandyTestBlocked())
			require.Equal(t, !step.blocked, account.IsSchedulable())
			require.True(t, account.Schedulable, "manual switch must be preserved")
			require.Equal(t, StatusActive, account.Status)
		})
	}
	// A correct answer never turns an administrator's switch back on.
	account.Schedulable = false
	upstream.responses = []*http.Response{candyResponse("21")}
	_, err := svc.RunCandyTest(context.Background(), account.ID, 0)
	require.NoError(t, err)
	require.False(t, account.IsCandyTestBlocked())
	require.False(t, account.Schedulable)
	require.False(t, account.IsSchedulable())
}

func TestCandySchedulingTransportAndAPIErrorDoNotMutateRuntimeState(t *testing.T) {
	for _, status := range []int{0, 401, 403, 429, 500, 502, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			account := automaticCandyAccount()
			repo := &candyAccountRepo{account: account}
			upstream := &queuedHTTPUpstream{}
			if status > 0 {
				response := newJSONResponse(status, `{"error":{"type":"usage_limit_reached","message":"21 is not an answer","resets_in_seconds":3600}}`)
				response.Header.Set("x-codex-primary-used-percent", "100")
				response.Header.Set("x-codex-primary-window-minutes", "300")
				response.Header.Set("x-codex-primary-reset-after-seconds", "18000")
				upstream.responses = []*http.Response{response}
			}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
			result, err := svc.RunCandyTest(context.Background(), account.ID, 0)
			require.NoError(t, err)
			require.Equal(t, 1, result.Errors)
			require.Zero(t, result.Failed)
			require.Zero(t, result.Passed)
			require.Empty(t, result.LastVerdict)
			require.True(t, account.IsSchedulable())
			require.Nil(t, account.RateLimitResetAt)
			require.Empty(t, account.ErrorMessage)
			require.NotContains(t, account.Extra, "codex_5h_used_percent")
			require.Equal(t, 1, repo.writes, "only the candy result should be persisted")
		})
	}
}

func TestCandySchedulingVerdictSurvivesErrorHistoryRollover(t *testing.T) {
	snapshot := CandyTestSnapshot{}
	appendCandyTestSample(&snapshot, CandyTestSample{Status: "failed"})
	for i := 0; i < candyTestHistoryLimit+1; i++ {
		appendCandyTestSample(&snapshot, CandyTestSample{Status: "error"})
	}
	require.Len(t, snapshot.Recent, candyTestHistoryLimit)
	for _, sample := range snapshot.Recent {
		require.Equal(t, "error", sample.Status)
	}
	account := automaticCandyAccount()
	account.Extra[CandyTestExtraKey] = snapshot
	require.True(t, account.IsCandyTestBlocked())
	appendCandyTestSample(&snapshot, CandyTestSample{Status: "passed"})
	account.Extra[CandyTestExtraKey] = snapshot
	require.True(t, account.IsSchedulable())
}

func TestCandySchedulingLegacyHistoryAndPlatformScope(t *testing.T) {
	account := automaticCandyAccount()
	account.Extra[CandyTestExtraKey] = CandyTestSnapshot{
		Total: 2, Failed: 1, Errors: 1,
		Recent: []CandyTestSample{{Status: "failed"}, {Status: "error"}},
	}
	require.True(t, account.IsCandyTestBlocked())
	require.False(t, account.IsSchedulable())
	account.Extra[CandyTestBlockedExtraKey] = true
	for _, pair := range [][2]string{{PlatformOpenAI, AccountTypeAPIKey}, {PlatformAnthropic, AccountTypeOAuth}, {PlatformGemini, AccountTypeOAuth}} {
		account.Platform, account.Type = pair[0], pair[1]
		require.False(t, account.IsCandyTestBlocked())
		require.True(t, account.IsSchedulable())
	}
}

func TestScheduledCandyTestRecoversBlockedAccount(t *testing.T) {
	account := automaticCandyAccount()
	repo := &candyAccountRepo{account: account}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: &queuedHTTPUpstream{responses: []*http.Response{candyResponse("34"), candyResponse("21")}}}
	result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.False(t, account.IsSchedulable())
	require.True(t, candyAutomaticAccountEligible(account, time.Now()), "blocked accounts must still be retested")
	result.Schedules[7] = CandyTestSchedule{Model: defaultCandyTestModel, CheckedAt: time.Now().Add(-time.Hour)}
	account.Extra[CandyTestExtraKey] = result
	result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "passed", result.LastVerdict)
	require.True(t, account.IsSchedulable())
}

func TestCandySchedulingSkipsWrongAnswerEvenWithStaleStickyCache(t *testing.T) {
	for _, advanced := range []string{"true", "false"} {
		for _, verdict := range []string{"failed", "passed", "error"} {
			t.Run(advanced+"/"+verdict, func(t *testing.T) {
				ctx := context.Background()
				groupID := int64(10103)
				primary := &Account{ID: 33001, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0, GroupIDs: []int64{groupID}}
				backup := &Account{ID: 33002, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
				dbPrimary := *primary
				snapshot := CandyTestSnapshot{}
				appendCandyTestSample(&snapshot, CandyTestSample{Status: verdict})
				dbPrimary.Extra = map[string]any{CandyTestExtraKey: snapshot, CandyTestBlockedExtraKey: verdict == "failed"}
				svc := &OpenAIGatewayService{
					accountRepo:      schedulerTestOpenAIAccountRepo{accounts: []Account{dbPrimary, *backup}},
					cache:            &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:candy_session": primary.ID}},
					cfg:              &config.Config{},
					rateLimitService: newOpenAIAdvancedSchedulerRateLimitService(advanced),
					schedulerSnapshot: &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{
						snapshotAccounts: []*Account{primary, backup},
						accountsByID:     map[int64]*Account{primary.ID: primary, backup.ID: backup},
					}},
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
				}
				expected := primary.ID
				if verdict == "failed" {
					expected = backup.ID
				}
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "candy_session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, expected, selection.Account.ID)
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				// The legacy model selector must also recheck stale candidate metadata.
				selected, err := svc.SelectAccountForModelWithExclusions(ctx, &groupID, "", "gpt-5.1", nil)
				require.NoError(t, err)
				require.Equal(t, expected, selected.ID)
			})
		}
	}
}
