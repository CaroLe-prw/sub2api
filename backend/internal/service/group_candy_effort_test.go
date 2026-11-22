//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCandyTestReasoningEffort(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
		got, err := normalizeCandyTestReasoningEffort(PlatformOpenAI, effort)
		require.NoError(t, err)
		require.Equal(t, effort, got)
	}
	for input, expected := range map[string]string{"": "medium", "  ": "medium", " HIGH ": "high"} {
		got, err := normalizeCandyTestReasoningEffort(PlatformOpenAI, input)
		require.NoError(t, err)
		require.Equal(t, expected, got)
	}
	for _, input := range []string{"none", "auto", "ultra", "high low"} {
		_, err := normalizeCandyTestReasoningEffort(PlatformOpenAI, input)
		require.True(t, infraerrors.IsBadRequest(err))
	}
	got, err := normalizeCandyTestReasoningEffort(PlatformAnthropic, "max")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCandyConfigRequiresGroupWhenOnlyEffortsDiffer(t *testing.T) {
	account := candyAccount()
	got, err := resolveCandyTestConfig(account, 0)
	require.NoError(t, err)
	require.Equal(t, "medium", got.ReasoningEffort)
	account.Groups = []*Group{
		{ID: 1, Platform: PlatformOpenAI, CandyTestReasoningEffort: "low"},
		{ID: 2, Platform: PlatformOpenAI, CandyTestReasoningEffort: "high"},
		{ID: 3, Platform: PlatformAnthropic, CandyTestReasoningEffort: "max"},
	}
	_, err = resolveCandyTestConfig(account, 0)
	require.ErrorContains(t, err, "Select a group")
	require.True(t, infraerrors.IsBadRequest(err))
	for id, expected := range map[int64]string{1: "low", 2: "high"} {
		got, err = resolveCandyTestConfig(account, id)
		require.NoError(t, err)
		require.Equal(t, expected, got.ReasoningEffort)
	}
	_, err = resolveCandyTestConfig(account, 3)
	require.True(t, infraerrors.IsBadRequest(err))
	account.Groups[0].CandyTestReasoningEffort = ""
	account.Groups[1].CandyTestReasoningEffort = "medium"
	got, err = resolveCandyTestConfig(account, 0)
	require.NoError(t, err)
	require.Equal(t, "medium", got.ReasoningEffort)
	account.Groups[1].CandyTestReasoningEffort = "invalid"
	_, err = resolveCandyTestConfig(account, 2)
	require.True(t, infraerrors.IsBadRequest(err))
}

func TestCandyConfiguredEffortReachesEveryAccountProtocol(t *testing.T) {
	for _, protocol := range []string{"oauth", "responses", "chat_completions"} {
		for _, mode := range []string{"manual", "scheduled"} {
			for _, effort := range []string{"low", "medium", "high", "xhigh", "max"} {
				t.Run(protocol+"/"+mode+"/"+effort, func(t *testing.T) {
					account := automaticCandyAccount()
					response := candyResponse("最少 21 个")
					effortPath := "reasoning.effort"
					if protocol != "oauth" {
						responses := protocol == "responses"
						account = candyAPIKeyAccount(responses)
						response = candyAPIKeyResponse(responses, "最少 21 个")
						if !responses {
							effortPath = "reasoning_effort"
						}
					}
					account.Groups[0].CandyTestReasoningEffort = effort
					upstream := &queuedHTTPUpstream{responses: []*http.Response{response}}
					svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream, cfg: &config.Config{}}
					var result *CandyTestSnapshot
					var err error
					if mode == "manual" {
						result, err = svc.RunCandyTest(context.Background(), account.ID, 7)
					} else {
						result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 1, result.Passed)
					require.Equal(t, effort, result.Recent[0].ReasoningEffort)
					require.Equal(t, effort, result.Schedules[7].ReasoningEffort)
					saved, err := readCandyTestSnapshot(account.Extra)
					require.NoError(t, err)
					require.Equal(t, effort, saved.Recent[0].ReasoningEffort)
					require.Len(t, upstream.requests, 1)
					body, err := io.ReadAll(upstream.requests[0].Body)
					require.NoError(t, err)
					require.Equal(t, effort, gjson.GetBytes(body, effortPath).String())
				})
			}
		}
	}
}

func TestCandyEffortChangesInvalidateScheduleWithoutRewritingHistory(t *testing.T) {
	account := automaticCandyAccount()
	checkedAt := time.Now()
	account.Extra[CandyTestExtraKey] = CandyTestSnapshot{
		Total: 1, Passed: 1,
		Recent:    []CandyTestSample{{Status: "passed", Model: defaultCandyTestModel, CheckedAt: checkedAt}},
		Schedules: map[int64]CandyTestSchedule{7: {Model: defaultCandyTestModel, CheckedAt: checkedAt}},
	}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
	svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream}
	// Legacy schedule rows represent medium, not an unspecified new effort.
	result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Nil(t, result)
	require.Empty(t, upstream.requests)
	account.Groups[0].CandyTestReasoningEffort = "high"
	result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Recent, 2)
	require.Empty(t, result.Recent[0].ReasoningEffort, "do not invent effort for old history")
	require.Equal(t, "high", result.Recent[1].ReasoningEffort)
	require.Equal(t, "high", result.Schedules[7].ReasoningEffort)
	result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
}

func TestAdminGroupCandyEffortCreateUpdateAndValidation(t *testing.T) {
	for _, simple := range []bool{false, true} {
		repo := &groupRepoStubForAdmin{}
		svc := &adminServiceImpl{groupRepo: repo}
		if simple {
			svc.cfg = &config.Config{RunMode: config.RunModeSimple}
		}
		_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Candy", Platform: PlatformOpenAI, RateMultiplier: 1})
		require.NoError(t, err)
		require.Equal(t, "medium", repo.created.CandyTestReasoningEffort)
		_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Custom", Platform: PlatformOpenAI, RateMultiplier: 1, CandyTestReasoningEffort: " HIGH "})
		require.NoError(t, err)
		require.Equal(t, "high", repo.created.CandyTestReasoningEffort)
		repo.getByID = repo.created
		_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Name: "Renamed"})
		require.NoError(t, err)
		require.Equal(t, "high", repo.updated.CandyTestReasoningEffort)
		for _, effort := range []string{"low", "medium", "high", "xhigh", "max", ""} {
			_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestReasoningEffort: &effort})
			require.NoError(t, err)
			expected := effort
			if expected == "" {
				expected = "medium"
			}
			require.Equal(t, expected, repo.updated.CandyTestReasoningEffort)
		}
		repo.created, repo.updated = nil, nil
		invalid := "auto"
		_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Invalid", Platform: PlatformOpenAI, RateMultiplier: 1, CandyTestReasoningEffort: invalid})
		require.True(t, infraerrors.IsBadRequest(err))
		require.Nil(t, repo.created)
		_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{CandyTestReasoningEffort: &invalid})
		require.True(t, infraerrors.IsBadRequest(err))
		require.Nil(t, repo.updated)
		_, err = svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "Other", Platform: PlatformAnthropic, RateMultiplier: 1, CandyTestReasoningEffort: "high"})
		require.NoError(t, err)
		require.Empty(t, repo.created.CandyTestReasoningEffort)
	}
}
