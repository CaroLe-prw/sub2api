package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAPIClientDiscoversMaskedTokenWithoutUserID(t *testing.T) {
	connection := newAPITestConnection()
	connection.UserID = "PublicUser123"
	connection.APIKey = "sk-eaVf-test-key-b0rU"
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/user/self":
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":"PublicUser123","group":"default"}}`), nil
		case "/api/token/search":
			return newAPITestResponse(http.StatusOK, `{"success":false,"message":"search unavailable"}`), nil
		case "/api/token/":
			require.Equal(t, "Bearer "+newAPITestAccessToken, req.Header.Get("Authorization"))
			require.Equal(t, "PublicUser123", req.Header.Get("New-Api-User"))
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"page":1,"page_size":10,"total":2,"items":[{"kind":"api_key","id":13069,"key":"ePDJ**********kqiS","status":1,"group":"GPT Pro Max","cross_group_retry":false},{"kind":"api_key","id":12991,"key":"eaVf**********b0rU","status":1,"group":"GPT K12","cross_group_retry":false}]}}`), nil
		case "/api/token/12991/key":
			require.Equal(t, http.MethodPost, req.Method)
			require.Equal(t, "Bearer "+newAPITestAccessToken, req.Header.Get("Authorization"))
			require.Equal(t, "PublicUser123", req.Header.Get("New-Api-User"))
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"key":"eaVf-test-key-b0rU"}}`), nil
		case "/api/user/self/groups":
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"default":{"ratio":1},"GPT Pro Max":{"ratio":0.2},"GPT K12":{"ratio":0.08}}}`), nil
		default:
			t.Fatalf("unexpected request: %s", req.URL.Path)
			return nil, nil
		}
	}}
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), connection)
	require.NoError(t, err)
	require.Equal(t, "GPT K12", result.ActualGroup)
	require.InDelta(t, 0.08, *result.Ratio, 1e-9)
}

func TestNewAPIClientTokenListRequiresExactUniqueKey(t *testing.T) {
	for _, tc := range []struct {
		name, list, revealed, want string
	}{
		{
			name:     "same mask but different full key",
			list:     `{"total":1,"items":[{"id":9,"key":"eaVf****b0rU","status":1,"group":"VIP"}]}`,
			revealed: "eaVf-other-key-b0rU", want: "newapi_token_key_not_matched",
		},
		{
			name:     "masked key cannot prove identity",
			list:     `{"total":1,"items":[{"id":9,"key":"eaVf****b0rU","status":1,"group":"VIP"}]}`,
			revealed: "eaVf****b0rU", want: "newapi_token_key_unavailable",
		},
		{
			name: "missing token id cannot reveal key",
			list: `{"total":1,"items":[{"key":"eaVf****b0rU","status":1,"group":"VIP"}]}`,
			want: "newapi_token_key_unavailable",
		},
		{
			name: "duplicate full key has ambiguous group",
			list: `{"total":2,"items":[{"id":9,"key":"eaVf-test-key-b0rU","status":1,"group":"VIP"},{"id":10,"key":"sk-eaVf-test-key-b0rU","status":1,"group":"Other"}]}`,
			want: "newapi_token_search_not_unique",
		},
		{
			name: "explicit ownership mismatch still fails",
			list: `{"total":1,"items":[{"id":9,"key":"eaVf-test-key-b0rU","user_id":99,"status":1,"group":"VIP"}]}`,
			want: "newapi_token_user_mismatch",
		},
		{
			name: "disabled exact token still fails",
			list: `{"total":1,"items":[{"id":9,"key":"eaVf-test-key-b0rU","status":2,"group":"VIP"}]}`,
			want: "newapi_token_not_enabled",
		},
		{
			name: "incomplete listing cannot choose first token",
			list: `{"total":2,"items":[]}`,
			want: "newapi_token_list_invalid_response",
		},
		{
			name: "bounded listing",
			list: `{"total":501,"items":[{"id":9,"key":"eaVf-test-key-b0rU","status":1,"group":"VIP"}]}`,
			want: "newapi_token_discovery_limit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connection := newAPITestConnection()
			connection.APIKey = "sk-eaVf-test-key-b0rU"
			doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/api/user/self":
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":42,"group":"Basic"}}`), nil
				case "/api/token/search":
					return newAPITestResponse(http.StatusNotFound, `{"success":false}`), nil
				case "/api/token/":
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":`+tc.list+`}`), nil
				case "/api/token/9/key":
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"key":"`+tc.revealed+`"}}`), nil
				default:
					t.Fatalf("must stop before group lookup: %s", req.URL.Path)
					return nil, nil
				}
			}}
			_, err := NewNewAPIClient(doer).Resolve(t.Context(), connection)
			require.EqualError(t, err, tc.want)
			require.NotContains(t, err.Error(), connection.APIKey)
			require.NotContains(t, err.Error(), connection.UserAccessToken)
		})
	}
}

func TestNewAPIClientVerifiesSearchResultWithoutOwner(t *testing.T) {
	success := newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/token/search":
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"total":1,"items":[{"id":9,"key":"obviously****key","status":1,"group":"VIP"}]}}`), nil
		case "/api/token/9/key":
			require.Equal(t, http.MethodPost, req.Method)
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"key":"`+newAPITestAPIKey+`"}}`), nil
		default:
			return success(req)
		}
	}}
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, "VIP", result.ActualGroup)
}

func TestNewAPIClientTokenListChecksAllPagesBeforeSelectingGroup(t *testing.T) {
	success := newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	pages := []string{}
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/token/search":
			return newAPITestResponse(http.StatusMethodNotAllowed, ""), nil
		case "/api/token/":
			page := req.URL.Query().Get("p")
			pages = append(pages, page)
			require.Equal(t, "100", req.URL.Query().Get("size"))
			if page == "1" {
				return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"page":1,"total":2,"items":[{"id":8,"key":"other-token","status":1,"group":"Other"}]}}`), nil
			}
			return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"page":2,"total":2,"items":[{"id":9,"key":"`+newAPITestAPIKey+`","status":1,"group":"VIP"}]}}`), nil
		default:
			return success(req)
		}
	}}
	result, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, pages)
	require.Equal(t, "VIP", result.ActualGroup)
}

func TestNewAPIClientTokenDiscoveryDoesNotLeakRejectedResponse(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/api/user/self" {
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":42,"group":"Basic"}}`), nil
				}
				if status != http.StatusOK {
					require.Equal(t, "/api/token/search", req.URL.Path, "HTTP auth failures must not trigger fallback")
				}
				return newAPITestResponse(status, `{"success":false,"message":"`+newAPITestAccessToken+` `+newAPITestAPIKey+`"}`), nil
			}}
			_, err := NewNewAPIClient(doer).Resolve(t.Context(), newAPITestConnection())
			require.Error(t, err)
			require.NotContains(t, err.Error(), newAPITestAccessToken)
			require.NotContains(t, err.Error(), newAPITestAPIKey)
			if status == http.StatusOK {
				require.EqualError(t, err, "newapi_token_list_rejected")
			} else {
				require.True(t, strings.HasPrefix(err.Error(), "newapi_token_search_http_"))
			}
		})
	}
}
