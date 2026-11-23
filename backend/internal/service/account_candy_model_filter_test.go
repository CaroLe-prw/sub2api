//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCandyTestRejectsModelOutsideAccountWhitelist(t *testing.T) {
	for _, protocol := range []string{"oauth", "responses", "chat_completions"} {
		for _, automatic := range []bool{false, true} {
			mode := "manual"
			if automatic {
				mode = "scheduled"
			}
			t.Run(protocol+"/"+mode, func(t *testing.T) {
				account := automaticCandyAccount()
				response := candyResponse("29")
				if protocol != "oauth" {
					account = candyAPIKeyAccount(protocol == "responses")
					response = candyAPIKeyResponse(protocol == "responses", "29")
				}
				account.Groups[0].CandyTestReasoningEffort = "low"
				account.Credentials["model_mapping"] = map[string]any{
					"gpt-5.6-sol": "gpt-5.6-sol", "gpt-6-astra": "gpt-6-astra",
					"gpt-6-sol": "gpt-6-sol", "gpt-6.1-sol": "gpt-6.1-sol",
				}
				require.False(t, account.IsModelSupported(defaultCandyTestModel))
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
				require.Empty(t, upstream.requests, "candy checks must not request gpt-6.2-sol when the account whitelist excludes it")
				require.Nil(t, result)
				require.Zero(t, repo.writes, "a configuration mismatch must not change the answer history or scheduling verdict")
				require.True(t, account.Schedulable)
				require.False(t, account.IsCandyTestBlocked())
				require.True(t, infraerrors.IsBadRequest(err), "checks must explain the model restriction: %v", err)
				require.Equal(t, "CANDY_TEST_MODEL_NOT_ALLOWED", infraerrors.Reason(err))
			})
		}
	}
}

func TestCandyTestAllowedModelsKeepExactUpstreamRequest(t *testing.T) {
	for _, protocol := range []string{"oauth", "responses", "chat_completions"} {
		for _, tc := range []struct {
			name, model string
			mapping     map[string]any
		}{
			{"no whitelist", defaultCandyTestModel, nil},
			{"exact whitelist", "gpt-6.1-sol", map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}},
			{"wildcard whitelist", defaultCandyTestModel, map[string]any{"gpt-6.*": "gpt-6.*"}},
			{"mapping targets stay ignored", defaultCandyTestModel, map[string]any{defaultCandyTestModel: "gpt-5.5"}},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				account := automaticCandyAccount()
				response := candyResponse("21")
				if protocol != "oauth" {
					account = candyAPIKeyAccount(protocol == "responses")
					response = candyAPIKeyResponse(protocol == "responses", "21")
				}
				account.Groups[0].CandyTestModel = tc.model
				if tc.mapping != nil {
					account.Credentials["model_mapping"] = tc.mapping
				}
				upstream := &queuedHTTPUpstream{responses: []*http.Response{response}}
				svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}, httpUpstream: upstream, cfg: &config.Config{}}
				result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
				require.NoError(t, err)
				require.Equal(t, 1, result.Passed)
				require.Len(t, upstream.requests, 1)
				body, err := io.ReadAll(upstream.requests[0].Body)
				require.NoError(t, err)
				require.Equal(t, tc.model, gjson.GetBytes(body, "model").String())
				require.Equal(t, tc.model, result.Recent[0].Model)
			})
		}
	}
}

func TestCandyTestPreservesPassthroughFilterSetting(t *testing.T) {
	for _, filtered := range []bool{false, true} {
		name := "unrestricted passthrough ignores leftover whitelist"
		if filtered {
			name = "explicit passthrough filter enforces whitelist"
		}
		t.Run(name, func(t *testing.T) {
			account := automaticCandyAccount()
			account.Credentials["model_mapping"] = map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}
			account.Extra["openai_passthrough"] = true
			account.Extra["openai_passthrough_model_filter"] = filtered
			repo := &candyAccountRepo{account: account}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
			result, err := svc.RunCandyTest(context.Background(), account.ID, 7)
			if filtered {
				require.Equal(t, "CANDY_TEST_MODEL_NOT_ALLOWED", infraerrors.Reason(err))
				require.Nil(t, result)
				require.Empty(t, upstream.requests)
				require.Zero(t, repo.writes)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, result.Passed)
				require.Len(t, upstream.requests, 1)
			}
		})
	}
}

type candyWhitelistReloadRepo struct {
	*candyAccountRepo
	reads int
}

func (r *candyWhitelistReloadRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.reads++
	if r.reads == 2 {
		r.account.Credentials["model_mapping"] = map[string]any{"gpt-6.1-sol": "gpt-6.1-sol"}
	}
	return r.candyAccountRepo.GetByID(ctx, id)
}

func TestCandyTestRechecksWhitelistBeforeDispatch(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		mode := "manual"
		if automatic {
			mode = "scheduled"
		}
		t.Run(mode, func(t *testing.T) {
			account := automaticCandyAccount()
			prior := CandyTestSnapshot{}
			appendCandyTestSample(&prior, CandyTestSample{Status: "failed", Model: "gpt-6.1-sol"})
			account.Extra[CandyTestExtraKey] = prior
			before, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			repo := &candyWhitelistReloadRepo{candyAccountRepo: &candyAccountRepo{account: account}}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("29")}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
			var result *CandyTestSnapshot
			if automatic {
				result, err = svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
			} else {
				result, err = svc.RunCandyTest(context.Background(), account.ID, 7)
			}
			require.Empty(t, upstream.requests)
			require.Nil(t, result)
			require.Equal(t, "CANDY_TEST_MODEL_NOT_ALLOWED", infraerrors.Reason(err))
			require.Zero(t, repo.writes)
			after, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
			require.False(t, account.IsCandyTestBlocked())
		})
	}
}
