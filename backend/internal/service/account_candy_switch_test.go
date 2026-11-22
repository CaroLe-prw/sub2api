//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type candySwitchAccountRepo struct {
	*candyAccountRepo
	reads, disableOnRead int
}

func (r *candySwitchAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.reads++
	if r.reads == r.disableOnRead {
		r.account.Schedulable = false
	}
	return r.candyAccountRepo.GetByID(ctx, id)
}

func TestCandySchedulingSwitchSkipsWithoutChangingHistoryAndResumes(t *testing.T) {
	for _, protocol := range []string{"oauth", "responses", "chat_completions"} {
		for _, mode := range []string{"manual", "scheduled"} {
			for _, phase := range []string{"before_test", "before_dispatch"} {
				t.Run(protocol+"/"+mode+"/"+phase, func(t *testing.T) {
					account := automaticCandyAccount()
					response := candyResponse("21")
					if protocol != "oauth" {
						responses := protocol == "responses"
						account = candyAPIKeyAccount(responses)
						response = candyAPIKeyResponse(responses, "21")
					}
					account.Extra[CandyTestExtraKey] = CandyTestSnapshot{LastVerdict: "failed", Total: 2, Failed: 2, ConsecutiveFailures: 2,
						Recent: []CandyTestSample{{Status: "failed", Model: defaultCandyTestModel, ResponseText: "15"}}}
					account.Extra[CandyTestBlockedExtraKey] = true
					before, err := json.Marshal(account.Extra)
					require.NoError(t, err)
					repo := &candySwitchAccountRepo{candyAccountRepo: &candyAccountRepo{account: account}}
					if phase == "before_test" {
						account.Schedulable = false
					} else {
						// The executor reloads the account after waiting for a probe slot.
						// Simulate an administrator switching it off after it was queued.
						repo.disableOnRead = 2
					}
					upstream := &queuedHTTPUpstream{responses: []*http.Response{response}}
					svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
					run := svc.RunCandyTest
					if mode == "scheduled" {
						run = svc.RunScheduledCandyTest
					}
					result, err := run(context.Background(), account.ID, 7)
					if mode == "manual" {
						require.ErrorIs(t, err, errCandyTestSchedulingDisabled)
					} else {
						require.NoError(t, err)
					}
					require.Nil(t, result)
					require.Empty(t, upstream.requests, "disabled accounts must not spend upstream quota")
					require.Zero(t, repo.writes, "a skipped test is not an API error or a wrong answer")
					after, err := json.Marshal(account.Extra)
					require.NoError(t, err)
					require.JSONEq(t, string(before), string(after))
					require.False(t, account.Schedulable)
					require.True(t, account.IsCandyTestBlocked())
					// The manual switch, not effective schedulability, allows recovery.
					account.Schedulable, repo.disableOnRead = true, 0
					result, err = run(context.Background(), account.ID, 7)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, "passed", result.LastVerdict)
					require.Len(t, upstream.requests, 1)
					require.Equal(t, 1, repo.writes)
					require.True(t, account.Schedulable)
					require.True(t, account.IsSchedulable())
				})
			}
		}
	}
}
