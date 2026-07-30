package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	newAPITestAccessToken = "pat-obviously-fake-access-token"
	newAPITestAPIKey      = "sk-obviously-fake-model-key"
)

type newAPITestDoer struct {
	mu       sync.Mutex
	requests []*http.Request
	handle   func(*http.Request) (*http.Response, error)
}

func (d *newAPITestDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	d.requests = append(d.requests, req.Clone(req.Context()))
	d.mu.Unlock()
	return d.handle(req)
}

func newAPITestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newAPITestConnection() NewAPIConnection {
	return NewAPIConnection{
		BaseURL:         "https://newapi.example.test",
		UserAccessToken: newAPITestAccessToken,
		UserID:          42,
		APIKey:          newAPITestAPIKey,
	}
}

func newAPITestSuccessHandler(
	t *testing.T,
	userGroup string,
	tokenGroup string,
	crossGroupRetry bool,
	groupRatio string,
) func(*http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "Bearer "+newAPITestAccessToken, req.Header.Get("Authorization"))
		switch req.URL.Path {
		case "/api/user/self":
			return newAPITestResponse(http.StatusOK,
				`{"success":true,"data":{"id":42,"group":"`+userGroup+`"}}`), nil
		case "/api/token/search":
			require.Equal(t, newAPITestAPIKey, req.URL.Query().Get("token"))
			require.Equal(t, "1", req.URL.Query().Get("p"))
			require.Equal(t, "10", req.URL.Query().Get("size"))
			return newAPITestResponse(http.StatusOK,
				`{"success":true,"data":{"total":1,"items":[{"user_id":42,"status":1,"group":"`+
					tokenGroup+`","cross_group_retry":`+boolJSON(crossGroupRetry)+`}]}}`), nil
		case "/api/user/self/groups":
			return newAPITestResponse(http.StatusOK,
				`{"success":true,"data":{"`+userGroup+`":{"ratio":0.04},"`+
					tokenGroup+`":{"ratio":`+groupRatio+`}}}`), nil
		default:
			t.Fatalf("unexpected NewAPI endpoint: %s", req.URL.Path)
			return nil, nil
		}
	}
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func TestNewAPIClientUsesAuthorizationOnlyForInitialSelfRequest(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")

	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, 0.0325, *result.Ratio)

	selfRequests := 0
	for _, request := range doer.requests {
		if request.URL.Path == "/api/user/self" {
			selfRequests++
			require.Empty(t, request.Header.Get("New-Api-User"))
		} else {
			require.Equal(t, "42", request.Header.Get("New-Api-User"))
		}
	}
	require.Equal(t, 1, selfRequests)
	require.Len(t, doer.requests, 3)
}

func TestNewAPIClientRetriesLegacyUserHeaderExactlyOnce(t *testing.T) {
	doer := &newAPITestDoer{}
	selfRequests := 0
	success := newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	doer.handle = func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/user/self" {
			selfRequests++
			if selfRequests == 1 {
				require.Empty(t, req.Header.Get("New-Api-User"))
				return newAPITestResponse(
					http.StatusUnauthorized,
					`{"success":false,"message":"Unauthorized, New-Api-User header not provided"}`,
				), nil
			}
		}
		require.Equal(t, "42", req.Header.Get("New-Api-User"))
		return success(req)
	}

	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, 0.0325, *result.Ratio)
	require.Equal(t, 2, selfRequests)
}

func TestNewAPIClientRejectsUIDMismatchBeforeTokenLookup(t *testing.T) {
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/api/user/self", req.URL.Path)
		return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":99,"group":"Basic"}}`), nil
	}}

	_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.EqualError(t, err, "newapi_user_id_mismatch")
	require.Len(t, doer.requests, 1)
}

func TestNewAPIClientRejectsTokenOwnedByAnotherUser(t *testing.T) {
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/user/self":
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":42,"group":"Basic"}}`), nil
		case "/api/token/search":
			return newAPITestResponse(http.StatusOK,
				`{"success":true,"data":{"total":1,"items":[{"user_id":99,"status":1,"group":"VIP"}]}}`), nil
		default:
			t.Fatalf("must stop after token ownership mismatch")
			return nil, nil
		}
	}}

	_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.EqualError(t, err, "newapi_token_user_mismatch")
}

func TestNewAPIClientRejectsNonUniqueTokenSearch(t *testing.T) {
	for name, data := range map[string]string{
		"zero":     `{"total":0,"items":[]}`,
		"multiple": `{"total":2,"items":[{"user_id":42,"status":1},{"user_id":42,"status":1}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/user/self" {
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":42,"group":"Basic"}}`), nil
				}
				return newAPITestResponse(http.StatusOK, `{"success":true,"data":`+data+`}`), nil
			}}
			_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
			require.EqualError(t, err, "newapi_token_search_not_unique")
		})
	}
}

func TestNewAPIClientUsesUserGroupWhenTokenGroupIsEmpty(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "", false, "0.0325")
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, "Basic", result.ActualGroup)
	require.Equal(t, 0.04, *result.Ratio)
	require.Equal(t, NewAPIRatioSourceConfiguredGroup, result.RatioSource)
}

func TestNewAPIClientFixedGroupUsesEffectiveUserGroupRatioAndIgnoresPricing(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(
		t,
		"GPT Lite",
		"GPT Lite大户组",
		false,
		"0.0325",
	)
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, 0.0325, *result.Ratio)
	require.Equal(t, "GPT Lite大户组", result.ActualGroup)
	require.Equal(t, NewAPIRatioSourceConfiguredGroup, result.RatioSource)
	for _, request := range doer.requests {
		require.Contains(t, []string{
			"/api/user/self",
			"/api/token/search",
			"/api/user/self/groups",
		}, request.URL.Path)
	}
	require.Len(t, doer.requests, 3)
}

func TestNewAPIClientPrivateGroupAbsentFromPricingStillResolves(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "GPT Lite", "VIP", false, "0.0325")
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, 0.0325, *result.Ratio)
}

func TestNewAPIClientRejectsAutoGroupWithoutStaticResolution(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(t, "Basic", "auto", false, "0.04")
	_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.EqualError(t, err, "newapi_auto_group_unsupported")
	require.Len(t, doer.requests, 3)
}

func TestNewAPIClientCrossGroupRetryStillUsesConfiguredTokenGroupRatio(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(
		t,
		"Basic",
		"VIP",
		true,
		"0.0325",
	)
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.True(t, result.CrossGroupRetry)
	require.Equal(t, "VIP", result.ActualGroup)
	require.Equal(t, 0.0325, *result.Ratio)
	require.Equal(t, NewAPIRatioSourceConfiguredGroup, result.RatioSource)
}

func TestNewAPIClientHandlesMalformedTimeoutHTTPAndSuccessFalse(t *testing.T) {
	tests := map[string]struct {
		handle func(*http.Request) (*http.Response, error)
		code   string
	}{
		"malformed": {
			handle: func(*http.Request) (*http.Response, error) {
				return newAPITestResponse(http.StatusOK, `{not-json`), nil
			},
			code: "newapi_user_self_invalid_response",
		},
		"timeout": {
			handle: func(*http.Request) (*http.Response, error) {
				return nil, context.DeadlineExceeded
			},
			code: "newapi_request_timeout",
		},
		"non_2xx": {
			handle: func(*http.Request) (*http.Response, error) {
				return newAPITestResponse(http.StatusBadGateway, `upstream unavailable`), nil
			},
			code: "newapi_user_self_http_502",
		},
		"success_false": {
			handle: func(*http.Request) (*http.Response, error) {
				return newAPITestResponse(http.StatusOK, `{"success":false,"message":"secret details"}`), nil
			},
			code: "newapi_user_self_invalid_response",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			doer := &newAPITestDoer{handle: test.handle}
			_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
			require.EqualError(t, err, test.code)
		})
	}
}

func TestNewAPIClientRejectsInvalidRatios(t *testing.T) {
	for name, ratio := range map[string]string{
		"missing":  "null",
		"string":   `"0.03"`,
		"zero":     "0",
		"negative": "-0.2",
		"nan":      `"NaN"`,
		"infinity": "1e9999",
	} {
		t.Run(name, func(t *testing.T) {
			doer := &newAPITestDoer{}
			doer.handle = newAPITestSuccessHandler(t, "Basic", "VIP", false, ratio)
			_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
			require.EqualError(t, err, "newapi_effective_ratio_invalid")
		})
	}
}

func TestNewAPIClientErrorsNeverContainCredentialsOrQuery(t *testing.T) {
	connection := newAPITestConnection()
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/user/self" {
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":42,"group":"Basic"}}`), nil
		}
		return nil, errors.New("Get " + req.URL.String() + ": authorization " + connection.UserAccessToken)
	}}
	_, err := NewNewAPIClient(doer).Resolve(t.Context(), connection)
	require.Error(t, err)
	require.NotContains(t, err.Error(), connection.UserAccessToken)
	require.NotContains(t, err.Error(), connection.APIKey)
	require.NotContains(t, err.Error(), "token=")
}

func TestNewAPIClientCurrentKnownCaseResolvesPointZeroThreeTwoFive(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPITestSuccessHandler(
		t,
		"GPT Lite",
		"GPT Lite大户组",
		false,
		"0.0325",
	)
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, 0.0325, *result.Ratio)
	require.NotEqual(t, 0.04, *result.Ratio)
}

func TestNewAPIClientRejectsOversizedResponse(t *testing.T) {
	doer := &newAPITestDoer{handle: func(*http.Request) (*http.Response, error) {
		return newAPITestResponse(http.StatusOK, strings.Repeat("x", 256)), nil
	}}
	client := NewNewAPIClient(doer)
	client.requestLimit = 128

	_, err := client.Resolve(t.Context(), newAPITestConnection())
	require.EqualError(t, err, "newapi_response_too_large")
}

func TestNewAPIClientRequestTimeoutIsBounded(t *testing.T) {
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	client := NewNewAPIClient(doer)
	client.timeout = time.Millisecond
	_, err := client.Resolve(t.Context(), newAPITestConnection())
	require.EqualError(t, err, "newapi_request_timeout")
}
