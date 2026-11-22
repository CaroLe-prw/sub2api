//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func candyAPIKeyAccount(responses bool) *Account {
	account := automaticCandyAccount()
	account.Type = AccountTypeAPIKey
	account.Credentials = map[string]any{
		"api_key": "test-api-key", "base_url": "https://candy-upstream.example/v1",
		"model_mapping": map[string]any{defaultCandyTestModel: "gpt-5.4"},
	}
	account.Extra[openai_compat.ExtraKeyResponsesSupported] = responses
	return account
}

func candyAPIKeyResponse(responses bool, text string) *http.Response {
	if responses {
		return candyResponse(text)
	}
	chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}}}})
	return newJSONResponse(http.StatusOK, "data: "+string(chunk)+"\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
}

func TestCandyAPIKeyManualScheduledAndOrdinaryRequests(t *testing.T) {
	for _, protocol := range []string{"responses", "chat_completions"} {
		for _, mode := range []string{"manual", "scheduled", "ordinary"} {
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				responses := protocol == "responses"
				account := candyAPIKeyAccount(responses)
				upstream := &queuedHTTPUpstream{responses: []*http.Response{candyAPIKeyResponse(responses, "最少 21 个")}}
				svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream, cfg: &config.Config{}}
				switch mode {
				case "manual":
					result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
					require.NoError(t, err)
					require.Equal(t, 1, result.Passed)
					require.Equal(t, "最少 21 个", result.Recent[0].ResponseText)
				case "scheduled":
					result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, 1, result.Passed)
					require.Equal(t, defaultCandyTestModel, result.Schedules[7].Model)
				case "ordinary":
					result, err := svc.runTestBackgroundWithPrompt(context.Background(), account.ID, defaultCandyTestModel, "hi", false)
					require.NoError(t, err)
					require.Equal(t, "success", result.Status)
				}
				require.Len(t, upstream.requests, 1)
				req := upstream.requests[0]
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))
				require.Equal(t, "candy-upstream.example", req.URL.Host)
				promptPath, effortPath := "input.0.content.0.text", "reasoning.effort"
				if responses {
					require.Equal(t, "/v1/responses", req.URL.Path)
					require.False(t, gjson.GetBytes(body, "reasoning_effort").Exists())
				} else {
					promptPath, effortPath = "messages.0.content", "reasoning_effort"
					require.Equal(t, "/v1/chat/completions", req.URL.Path)
					require.False(t, gjson.GetBytes(body, "reasoning").Exists())
				}
				if mode == "ordinary" {
					require.Equal(t, "hi", gjson.GetBytes(body, promptPath).String())
					require.Equal(t, "gpt-5.4", gjson.GetBytes(body, "model").String())
					require.False(t, gjson.GetBytes(body, effortPath).Exists())
				} else {
					require.Equal(t, candyTestPrompt, gjson.GetBytes(body, promptPath).String())
					require.Equal(t, defaultCandyTestModel, gjson.GetBytes(body, "model").String())
					require.Equal(t, "medium", gjson.GetBytes(body, effortPath).String())
				}
			})
		}
	}
}

func TestCandyAPIKeyErrorsNeverMutateRuntimeState(t *testing.T) {
	for _, responses := range []bool{false, true} {
		for _, status := range []int{0, 400, 401, 403, 429, 500, 502, 503} {
			t.Run(map[bool]string{true: "responses/", false: "chat/"}[responses]+http.StatusText(status), func(t *testing.T) {
				account := candyAPIKeyAccount(responses)
				repo := &candyAccountRepo{account: account}
				upstream := &queuedHTTPUpstream{}
				if status != 0 {
					upstream.responses = []*http.Response{newJSONResponse(status, `{"error":{"type":"usage_limit_reached","message":"21 is not an answer","resets_in_seconds":3600}}`)}
				}
				svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
				result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
				require.NoError(t, err)
				require.Equal(t, 1, result.Errors)
				require.Zero(t, result.Passed)
				require.Zero(t, result.Failed)
				require.Empty(t, result.LastVerdict)
				require.True(t, account.IsSchedulable())
				require.Equal(t, StatusActive, account.Status)
				require.Nil(t, account.RateLimitResetAt)
				require.Empty(t, account.ErrorMessage)
				require.Equal(t, 1, repo.writes)
			})
		}
	}
}

func TestCandyAPIKeyWrongAnswerBlocksAndScheduledRetestRecovers(t *testing.T) {
	for _, responses := range []bool{false, true} {
		t.Run(map[bool]string{true: "responses", false: "chat"}[responses], func(t *testing.T) {
			account := candyAPIKeyAccount(responses)
			upstream := &queuedHTTPUpstream{responses: []*http.Response{
				candyAPIKeyResponse(responses, "15"), newJSONResponse(429, "rate limited"), candyAPIKeyResponse(responses, "21"),
			}}
			svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream, cfg: &config.Config{}}
			result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
			require.NoError(t, err)
			require.Equal(t, "failed", result.LastVerdict)
			require.False(t, account.IsSchedulable())
			require.True(t, account.Schedulable)
			require.True(t, candyAutomaticAccountEligible(account, time.Now()))
			result, err = svc.RunCandyTest(context.Background(), account.ID, 7)
			require.NoError(t, err)
			require.Equal(t, "failed", result.LastVerdict)
			require.Equal(t, "error", result.Recent[1].Status)
			require.False(t, account.IsSchedulable(), "API errors must not erase a previous wrong answer")
			result.Schedules[7] = CandyTestSchedule{Model: defaultCandyTestModel, CheckedAt: time.Now().Add(-time.Hour)}
			account.Extra[CandyTestExtraKey] = result
			result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "passed", result.LastVerdict)
			require.True(t, account.IsSchedulable())
		})
	}
}
