package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newAPITestBoolPointer(value bool) *bool {
	return &value
}

func newAPIBalanceHandler(
	t *testing.T,
	userBody string,
	tokenBody string,
) func(*http.Request) (*http.Response, error) {
	t.Helper()
	ratioHandler := newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	return func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/user/self":
			return newAPITestResponse(http.StatusOK, userBody), nil
		case "/api/status":
			require.Empty(t, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("New-Api-User"))
			return newAPITestResponse(
				http.StatusOK,
				`{"success":true,"data":{"display_in_currency":true,"quota_display_type":"USD","quota_per_unit":500000,"usd_exchange_rate":2,"custom_currency_symbol":"¤","custom_currency_exchange_rate":1}}`,
			), nil
		case "/api/usage/token/":
			require.Equal(t, "Bearer "+newAPITestAPIKey, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("New-Api-User"))
			return newAPITestResponse(http.StatusOK, tokenBody), nil
		default:
			return ratioHandler(req)
		}
	}
}

func validNewAPIUserBalanceBody() string {
	return `{"success":true,"data":{"id":42,"group":"Basic","quota":8000000,"used_quota":2000000}}`
}

func validNewAPITokenBalanceBody() string {
	return `{"code":true,"data":{"name":"gpt-plus","total_granted":10000000,"total_used":2000000,"total_available":8000000,"unlimited_quota":false,"expires_at":0}}`
}

func TestNewAPIBalanceParsesRawFiniteQuota(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(t, validNewAPIUserBalanceBody(), validNewAPITokenBalanceBody())

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Equal(t, int64(8000000), balance.Account.RemainingQuota)
	require.Equal(t, int64(10000000), balance.Account.TotalQuota)
	require.Equal(t, int64(8000000), balance.Token.RemainingQuota)
	require.Equal(t, int64(10000000), balance.Token.TotalQuota)
	require.Equal(t, &NewAPIQuotaDisplay{
		DisplayType:  "USD",
		Symbol:       "$",
		QuotaPerUnit: 500000,
		ExchangeRate: 1,
	}, balance.QuotaDisplay)
	require.True(t, balance.TokenAvailable)
	require.True(t, balance.AccountAvailable)
	require.True(t, balance.OverallAvailable)
	require.Empty(t, balance.Warnings)
}

func TestNewAPIQuotaDisplayNormalizesConfiguredCurrencies(t *testing.T) {
	tests := []struct {
		name string
		data newAPIStatusData
		want *NewAPIQuotaDisplay
	}{
		{
			name: "CNY",
			data: newAPIStatusData{
				QuotaDisplayType: "CNY",
				QuotaPerUnit:     json.RawMessage(`500000`),
				USDExchangeRate:  json.RawMessage(`7.2`),
			},
			want: &NewAPIQuotaDisplay{
				DisplayType: "CNY", Symbol: "¥", QuotaPerUnit: 500000, ExchangeRate: 7.2,
			},
		},
		{
			name: "custom numeric strings",
			data: newAPIStatusData{
				QuotaDisplayType:           "CUSTOM",
				QuotaPerUnit:               json.RawMessage(`"1000"`),
				CustomCurrencySymbol:       " € ",
				CustomCurrencyExchangeRate: json.RawMessage(`"0.9"`),
			},
			want: &NewAPIQuotaDisplay{
				DisplayType: "CUSTOM", Symbol: "€", QuotaPerUnit: 1000, ExchangeRate: 0.9,
			},
		},
		{
			name: "legacy raw quota",
			data: newAPIStatusData{
				DisplayInCurrency: newAPITestBoolPointer(false),
				QuotaPerUnit:      json.RawMessage(`500000`),
			},
			want: &NewAPIQuotaDisplay{
				DisplayType: "TOKENS", QuotaPerUnit: 500000, ExchangeRate: 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeNewAPIQuotaDisplay(&tt.data))
		})
	}
}

func TestNewAPIQuotaDisplayRejectsUnreliableConversionRules(t *testing.T) {
	tests := []newAPIStatusData{
		{QuotaDisplayType: "USD", QuotaPerUnit: json.RawMessage(`0`)},
		{QuotaDisplayType: "CNY", QuotaPerUnit: json.RawMessage(`500000`), USDExchangeRate: json.RawMessage(`null`)},
		{QuotaDisplayType: "CUSTOM", QuotaPerUnit: json.RawMessage(`500000`), CustomCurrencyExchangeRate: json.RawMessage(`1`)},
		{QuotaDisplayType: "UNKNOWN", QuotaPerUnit: json.RawMessage(`500000`)},
	}
	for _, data := range tests {
		require.Nil(t, normalizeNewAPIQuotaDisplay(&data))
	}
}

func TestNewAPIBalanceKeepsRawQuotaWhenStatusIsUnavailable(t *testing.T) {
	baseHandler := newAPIBalanceHandler(t, validNewAPIUserBalanceBody(), validNewAPITokenBalanceBody())
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/status" {
			return newAPITestResponse(http.StatusNotFound, `{"success":false}`), nil
		}
		return baseHandler(req)
	}}

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Nil(t, balance.QuotaDisplay)
	require.Equal(t, int64(8000000), balance.Account.RemainingQuota)
}

func TestNewAPIBalanceUnlimitedTokenWithZeroStillAvailable(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		validNewAPIUserBalanceBody(),
		`{"code":true,"data":{"name":"unlimited","total_granted":0,"total_used":0,"total_available":0,"unlimited_quota":true,"expires_at":0}}`,
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.True(t, balance.TokenAvailable)
	require.True(t, balance.OverallAvailable)
	require.Zero(t, balance.Token.RemainingQuota)
}

func TestNewAPIBalanceUnlimitedTokenAcceptsNegativeReportedRemaining(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		validNewAPIUserBalanceBody(),
		`{"code":true,"data":{"name":"unlimited","total_granted":1997191,"total_used":17530360,"total_available":-15533169,"unlimited_quota":true,"expires_at":0}}`,
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Equal(t, int64(-15533169), balance.Token.RemainingQuota)
	require.True(t, balance.TokenAvailable)
	require.True(t, balance.OverallAvailable)
	require.Empty(t, balance.Warnings)
}

func TestNewAPIBalanceUnlimitedTokenAcceptsNegativeReportedTotal(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		validNewAPIUserBalanceBody(),
		`{"code":true,"data":{"name":"unlimited","total_granted":-100,"total_used":20,"total_available":-120,"unlimited_quota":true,"expires_at":0}}`,
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Equal(t, int64(-100), balance.Token.TotalQuota)
	require.True(t, balance.TokenAvailable)
	require.True(t, balance.OverallAvailable)
	require.Empty(t, balance.Warnings)
}

func TestNewAPIBalanceFiniteTokenRejectsNegativeReportedTotal(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		validNewAPIUserBalanceBody(),
		`{"code":true,"data":{"name":"finite","total_granted":-100,"total_used":20,"total_available":0,"unlimited_quota":false,"expires_at":0}}`,
	)

	_, _, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.EqualError(t, err, "newapi_token_total_quota_invalid")
}

func TestNewAPIBalanceUnlimitedTokenStillRequiresAccountBalance(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		`{"success":true,"data":{"id":42,"group":"Basic","quota":0,"used_quota":2000000}}`,
		`{"code":true,"data":{"name":"unlimited","total_granted":0,"total_used":0,"total_available":0,"unlimited_quota":true,"expires_at":0}}`,
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.True(t, balance.TokenAvailable)
	require.False(t, balance.AccountAvailable)
	require.False(t, balance.OverallAvailable)
}

func TestNewAPIBalanceAcceptsOverdrawnAccount(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		`{"success":true,"data":{"id":42,"group":"Basic","quota":-500000,"used_quota":2500000}}`,
		validNewAPITokenBalanceBody(),
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Equal(t, int64(-500000), balance.Account.RemainingQuota)
	require.Equal(t, int64(2000000), balance.Account.TotalQuota)
	require.False(t, balance.AccountAvailable)
	require.False(t, balance.OverallAvailable)
}

func TestNewAPIBalanceQuotaMismatchIsWarning(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		validNewAPIUserBalanceBody(),
		`{"code":true,"data":{"total_granted":99,"total_used":20,"total_available":80,"unlimited_quota":false,"expires_at":0}}`,
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Equal(t, int64(99), balance.Token.TotalQuota)
	require.Equal(t, []string{"newapi_token_quota_mismatch"}, balance.Warnings)
}

func TestNewAPIBalanceAcceptsZeroAndNumericStrings(t *testing.T) {
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(
		t,
		`{"success":true,"data":{"id":42,"group":"Basic","quota":"0","used_quota":"0"}}`,
		`{"code":true,"data":{"total_granted":"0","total_used":"0","total_available":"0","unlimited_quota":false,"expires_at":"0"}}`,
	)

	_, balance, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.NoError(t, err)
	require.Zero(t, balance.Account.TotalQuota)
	require.Zero(t, balance.Token.TotalQuota)
	require.False(t, balance.OverallAvailable)
}

func TestNewAPIBalanceRejectsUnsafeQuotaValues(t *testing.T) {
	tests := map[string]string{
		"negative":     "-1",
		"fraction":     "1.5",
		"infinity":     "1e9999",
		"outside_safe": "9007199254740992",
		"wrong_type":   `{"value":1}`,
		"missing":      "null",
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			doer := &newAPITestDoer{}
			doer.handle = newAPIBalanceHandler(
				t,
				validNewAPIUserBalanceBody(),
				`{"code":true,"data":{"total_granted":10,"total_used":0,"total_available":`+value+`,"unlimited_quota":false,"expires_at":0}}`,
			)
			_, _, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())
			require.EqualError(t, err, "newapi_token_remaining_quota_invalid")
		})
	}
}

func TestNewAPIUserAuthDoesNotRetryUnrelatedForbidden(t *testing.T) {
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		require.Empty(t, req.Header.Get("New-Api-User"))
		return newAPITestResponse(http.StatusForbidden, `{"success":false,"message":"account disabled"}`), nil
	}}

	_, _, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.EqualError(t, err, "newapi_user_self_http_403")
	require.Len(t, doer.requests, 1)
}

func TestNewAPIUserAuthDoesNotRetryHeaderHintOnSuccessfulHTTPStatus(t *testing.T) {
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		require.Empty(t, req.Header.Get("New-Api-User"))
		return newAPITestResponse(
			http.StatusOK,
			`{"success":false,"message":"New-Api-User header not provided"}`,
		), nil
	}}

	_, _, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.EqualError(t, err, "newapi_user_self_invalid_response")
	require.Len(t, doer.requests, 1)
}

func TestNewAPIBalanceRejectsInvalidEnvelopes(t *testing.T) {
	tests := map[string]string{
		"malformed":  `{not-json`,
		"empty_data": `{"code":true,"data":null}`,
		"code_false": `{"code":false,"message":"denied","data":{"total_granted":1,"total_used":0,"total_available":1}}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			doer := &newAPITestDoer{}
			doer.handle = newAPIBalanceHandler(t, validNewAPIUserBalanceBody(), body)
			_, _, err := NewNewAPIClient(doer).ResolveWithBalance(t.Context(), newAPITestConnection())
			require.EqualError(t, err, "newapi_token_usage_invalid_response")
		})
	}
}

func TestNewAPIBalanceFailurePreservesLastCompleteSnapshotAndMarksStale(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	old := &NewAPIBalanceSnapshot{
		Account:    NewAPIBalanceAccount{UserID: 42, RemainingQuota: 7},
		Token:      NewAPIBalanceToken{RemainingQuota: 6},
		SyncedAt:   time.Now().Add(-time.Hour),
		FreshUntil: time.Now().Add(time.Hour),
	}
	account.Extra[NewAPIBalanceSnapshotExtraKey] = old
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/user/self" {
			return newAPITestResponse(http.StatusOK, validNewAPIUserBalanceBody()), nil
		}
		return nil, errors.New("transport failed with hidden credentials")
	}
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})

	_, err := service.SyncNewAPIAccount(t.Context(), account.ID)

	require.Error(t, err)
	require.Same(t, old, account.Extra[NewAPIBalanceSnapshotExtraKey])
	config, configErr := service.GetNewAPISyncConfig(t.Context(), account.ID)
	require.NoError(t, configErr)
	require.True(t, config.BalanceStale)
	require.Equal(t, int64(7), config.BalanceSnapshot.Account.RemainingQuota)
}

func TestNewAPIBalanceSuccessAtomicallyStoresCompleteSnapshot(t *testing.T) {
	account := newAPISyncTestAccount(1, 0.4)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	doer := &newAPITestDoer{}
	doer.handle = newAPIBalanceHandler(t, validNewAPIUserBalanceBody(), validNewAPITokenBalanceBody())
	service := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})
	now := time.Date(2026, 7, 31, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	result, err := service.SyncNewAPIAccount(t.Context(), account.ID)

	require.NoError(t, err)
	require.NotNil(t, result.BalanceSnapshot)
	require.Equal(t, now, result.BalanceSnapshot.SyncedAt)
	require.Equal(t, int64(8000000), result.BalanceSnapshot.Account.RemainingQuota)
	require.Equal(t, int64(8000000), result.BalanceSnapshot.Token.RemainingQuota)
	require.Same(t, result.BalanceSnapshot, account.Extra[NewAPIBalanceSnapshotExtraKey])
}

type newAPIRoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn newAPIRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNewAPIClientStopsCrossOriginRedirectBeforeForwardingAuthorization(t *testing.T) {
	requests := make([]*http.Request, 0, 2)
	httpClient := &http.Client{Transport: newAPIRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		if req.URL.Host != "newapi.example.test" {
			t.Fatalf("request followed cross-origin redirect with authorization=%q", req.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"https://other.example.test/api/user/self"}},
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    req,
		}, nil
	})}

	_, _, err := NewNewAPIClient(httpClient).ResolveWithBalance(t.Context(), newAPITestConnection())

	require.EqualError(t, err, "newapi_user_self_http_302")
	require.Len(t, requests, 1)
}

func TestNewAPIClientFollowsSameOriginSlashRedirect(t *testing.T) {
	requests := make([]*http.Request, 0, 2)
	httpClient := &http.Client{Transport: newAPIRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		require.Equal(t, "Bearer "+newAPITestAPIKey, req.Header.Get("Authorization"))
		if req.URL.Path == "/api/usage/token/" {
			return &http.Response{
				StatusCode: http.StatusPermanentRedirect,
				Header:     http.Header{"Location": []string{"/api/usage/token"}},
				Body:       io.NopCloser(strings.NewReader("redirect")),
				Request:    req,
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(validNewAPITokenBalanceBody())),
			Request:    req,
		}, nil
	})}
	client := NewNewAPIClient(httpClient)

	status, _, err := client.get(
		t.Context(),
		newAPITestConnection().BaseURL,
		"/api/usage/token/",
		nil,
		newAPITestAPIKey,
		0,
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, requests, 2)
	require.Equal(t, "/api/usage/token", requests[1].URL.Path)
}
