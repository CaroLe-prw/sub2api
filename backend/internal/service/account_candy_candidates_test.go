//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCandyCandidatesSkipDisallowedAndSendOneRequest(t *testing.T) {
	account := automaticCandyAccount()
	account.Groups[0].CandyTestModel = "gpt-6.2-sol\ngpt-6.1-sol\ngpt-6-sol"
	account.Credentials["model_mapping"] = map[string]any{
		"gpt-6.1-sol": "gpt-6.1-sol", "gpt-6-sol": "gpt-6-sol",
	}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
	svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream}
	result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	body, err := io.ReadAll(upstream.requests[0].Body)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(body, "model").String())
	require.Equal(t, "gpt-6.1-sol", result.Recent[0].Model)
	require.Equal(t, 1, result.Total)
}

func TestCandyCandidatesScheduleKeepsSelectedModelAndRespectsInterval(t *testing.T) {
	account := automaticCandyAccount()
	account.Groups[0].CandyTestModel = "gpt-6.2-sol\ngpt-6.1-sol"
	account.Credentials["model_mapping"] = map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}
	repo := &candyAccountRepo{account: account}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21"), candyResponse("21")}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", result.Schedules[7].Model)
	// The configured list differs from the actual model, but must not cause
	// another upstream request on every scheduler tick or after a restart.
	restarted := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err = restarted.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
	// Editing the priority list invalidates the old config's schedule.
	account.Groups[0].CandyTestModel = "gpt-6.1-sol\ngpt-6.2-sol"
	result, err = restarted.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-6.1-sol", result.Schedules[7].Model)
	require.Len(t, upstream.requests, 2)
}

func TestCandyCandidatesOnlyTestFirstAllowedAcrossProtocols(t *testing.T) {
	for _, protocol := range []string{"oauth", "responses", "chat_completions"} {
		for _, automatic := range []bool{false, true} {
			for _, outcome := range []string{"passed", "failed", "error", "none_allowed"} {
				t.Run(fmt.Sprintf("%s/automatic=%t/%s", protocol, automatic, outcome), func(t *testing.T) {
					account := automaticCandyAccount()
					answer := "21"
					if outcome == "failed" {
						answer = "29"
					}
					response := candyResponse(answer)
					if protocol != "oauth" {
						account = candyAPIKeyAccount(protocol == "responses")
						response = candyAPIKeyResponse(protocol == "responses", answer)
					}
					if outcome == "error" {
						response = newJSONResponse(500, "upstream unavailable")
					}
					account.Groups[0].CandyTestModel = "gpt-6.2-sol,gpt-6.1-sol"
					account.Credentials["model_mapping"] = map[string]any{
						"gpt-6.2-sol": "gpt-6.2-sol", "gpt-6.1-sol": "gpt-6.1-sol",
					}
					if outcome == "none_allowed" {
						account.Credentials["model_mapping"] = map[string]any{"gpt-6-sol": "gpt-6-sol"}
					}
					repo := &candyAccountRepo{account: account}
					upstream := &queuedHTTPUpstream{responses: []*http.Response{response}}
					svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
					var result *CandyTestSnapshot
					var err error
					if automatic {
						result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
					} else {
						result, err = svc.RunCandyTest(context.Background(), account.ID, 7)
					}
					if outcome == "none_allowed" {
						require.Equal(t, "CANDY_TEST_MODEL_NOT_ALLOWED", infraerrors.Reason(err))
						require.Nil(t, result)
						require.Empty(t, upstream.requests)
						require.Zero(t, repo.writes)
						return
					}
					require.NoError(t, err)
					require.Len(t, upstream.requests, 1, "wrong answers and API errors must not cause another model request")
					body, err := io.ReadAll(upstream.requests[0].Body)
					require.NoError(t, err)
					require.Equal(t, "gpt-6.2-sol", gjson.GetBytes(body, "model").String())
					require.Equal(t, "gpt-6.2-sol", result.Recent[0].Model)
					require.Equal(t, outcome, result.Recent[0].Status)
					require.Equal(t, 1, result.Total)
				})
			}
		}
	}
}

func TestCandyCandidatesRecheckWhitelistBeforeChoosing(t *testing.T) {
	account := automaticCandyAccount()
	account.Groups[0].CandyTestModel = "gpt-6.2-sol\ngpt-6.1-sol"
	// The latest account allows only the second candidate by the time a probe
	// slot opens, even though the initial account load was unrestricted.
	repo := &candyWhitelistReloadRepo{candyAccountRepo: &candyAccountRepo{account: account}}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	body, err := io.ReadAll(upstream.requests[0].Body)
	require.NoError(t, err)
	require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(body, "model").String())
	require.Equal(t, "gpt-6.1-sol", result.Recent[0].Model)
}

func TestAdminGroupCandyCandidatesPreserveOrderAndValidateLimits(t *testing.T) {
	for _, simple := range []bool{false, true} {
		t.Run(fmt.Sprintf("simple=%t", simple), func(t *testing.T) {
			repo := &groupRepoStubForAdmin{}
			svc := &adminServiceImpl{groupRepo: repo}
			if simple {
				svc.cfg = &config.Config{RunMode: config.RunModeSimple}
			}
			_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
				Name: "Candidates", Platform: PlatformOpenAI, RateMultiplier: 1,
				CandyTestModel: " gpt-6.2-sol, gpt-6.1-sol\r\n gpt-6.2-sol，gpt-6-sol ",
			})
			require.NoError(t, err)
			require.Equal(t, "gpt-6.2-sol\ngpt-6.1-sol\ngpt-6-sol", repo.created.CandyTestModel)
			repo.getByID = repo.created
			models := "gpt-6.1-sol\ngpt-6.2-sol"
			_, err = svc.UpdateGroup(context.Background(), repo.created.ID, &UpdateGroupInput{CandyTestModel: &models})
			require.NoError(t, err)
			require.Equal(t, models, repo.updated.CandyTestModel)
			models = strings.Repeat("a", 100) + "\n" + strings.Repeat("b", 100)
			_, err = svc.UpdateGroup(context.Background(), repo.created.ID, &UpdateGroupInput{CandyTestModel: &models})
			require.NoError(t, err)
			require.Equal(t, models, repo.updated.CandyTestModel)
			tooMany := make([]string, 21)
			for i := range tooMany {
				tooMany[i] = fmt.Sprintf("model-%d", i)
			}
			for _, invalid := range []string{
				"gpt-6.2-sol\ngpt-image-1", "gpt-6.2-sol\nbad model",
				"gpt-6.2-sol\n" + strings.Repeat("a", 101), strings.Join(tooMany, "\n"), strings.Repeat("a", 2049),
			} {
				_, err = svc.UpdateGroup(context.Background(), repo.created.ID, &UpdateGroupInput{CandyTestModel: &invalid})
				require.Equal(t, "INVALID_CANDY_TEST_MODEL", infraerrors.Reason(err))
			}
		})
	}
}
