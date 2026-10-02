package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAPISyncAcceptsStringUserIDEndToEnd(t *testing.T) {
	const userID = "X1vJHY6Hpd2lZZD3H5o5dS0i"
	var update NewAPISyncConfigUpdate
	err := json.Unmarshal([]byte(`{"newapi_sync_enabled":true,"newapi_user_id":"`+userID+`","newapi_user_access_token":"`+newAPITestAccessToken+`","newapi_base_url":"https://newapi.example.test"}`), &update)
	require.NoError(t, err)
	repo := &newAPISyncTestRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: newAPISyncTestAccount(1, 0.4)},
	}}
	success := newAPITestSuccessHandler(t, "Basic", "VIP", false, "0.0325")
	selfRequests := 0
	doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/user/self" {
			selfRequests++
			if req.Header.Get("New-Api-User") == "" {
				return newAPITestResponse(http.StatusUnauthorized, `{"success":false,"message":"New-Api-User header not provided"}`), nil
			}
		}
		if req.URL.Path != "/api/status" && req.URL.Path != "/api/usage/token/" {
			require.Equal(t, userID, req.Header.Get("New-Api-User"))
		}
		response, err := success(req)
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		_ = response.Body.Close()
		bodyString := strings.ReplaceAll(string(body), `"id":42`, `"id":"`+userID+`"`)
		bodyString = strings.ReplaceAll(bodyString, `"user_id":42`, `"user_id":"`+userID+`"`)
		return newAPITestResponse(response.StatusCode, bodyString), nil
	}}
	svc := newAPISyncTestService(t, repo, func(*Account) (*NewAPIClient, error) {
		return NewNewAPIClient(doer), nil
	})
	saved, err := svc.UpdateNewAPISyncConfig(t.Context(), 1, &update)
	require.NoError(t, err)
	require.Equal(t, userID, fmt.Sprint(saved.UserID))
	// Reload from JSON, as the repository does, before synchronizing.
	raw, err := json.Marshal(repo.accounts[1].Extra)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &repo.accounts[1].Extra))
	result, err := svc.SyncNewAPIAccount(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, NewAPISyncStatusOK, result.Status)
	require.Equal(t, userID, fmt.Sprint(result.BalanceSnapshot.Account.UserID))
	require.Equal(t, 0.0325, *result.NewRatio)
	require.Equal(t, 2, selfRequests)
	loaded, err := svc.GetNewAPISyncConfig(t.Context(), 1)
	require.NoError(t, err)
	require.Equal(t, userID, fmt.Sprint(loaded.UserID))
	require.Equal(t, userID, fmt.Sprint(loaded.BalanceSnapshot.Account.UserID))
}

func TestNewAPIUserIDAcceptsLegacyJSONWithoutPrecisionLoss(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`42`, "42"},
		{`"42"`, "42"},
		{`9007199254740993`, "9007199254740993"},
		{`"9007199254740993"`, "9007199254740993"},
		{`" X1vJHY6Hpd2lZZD3H5o5dS0i "`, "X1vJHY6Hpd2lZZD3H5o5dS0i"},
		{`"user_a-B9"`, "user_a-B9"},
		{`0`, ""},
		{`null`, ""},
		{`""`, ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			var update NewAPISyncConfigUpdate
			require.NoError(t, json.Unmarshal([]byte(`{"newapi_user_id":`+tc.raw+`}`), &update))
			require.Equal(t, NewAPIUserID(tc.want), update.UserID)
			stored := newAPIStoredConfigFromAccount(&Account{Extra: map[string]any{
				NewAPIUserIDExtraKey: json.RawMessage(tc.raw),
			}})
			require.Equal(t, update.UserID, stored.UserID)
			encoded, err := json.Marshal(update.UserID)
			require.NoError(t, err)
			require.Equal(t, `"`+tc.want+`"`, string(encoded))
		})
	}
	// Saving an existing numeric UID as text must preserve the identity hash.
	legacy := newAPIStoredConfigFromAccount(newAPISyncTestAccount(1, 0.4))
	require.Equal(t, legacy.IdentityHash, newAPISyncIdentity(legacy.BaseURL, legacy.UserID, legacy.UserAccessToken))
}

func TestNewAPIUserIDRejectsInvalidJSONAndHeaderValues(t *testing.T) {
	for _, raw := range []string{`-1`, `1.5`, `1e3`, `true`, `{}`, `[]`, `"-1"`, `"0"`, `"a b"`, `"a\r\nb"`, `"` + strings.Repeat("a", 257) + `"`} {
		t.Run(raw, func(t *testing.T) {
			var update NewAPISyncConfigUpdate
			require.Error(t, json.Unmarshal([]byte(`{"newapi_user_id":`+raw+`}`), &update))
		})
	}
}

func TestNewAPIClientStringUIDStillChecksUserAndTokenOwnership(t *testing.T) {
	for _, tc := range []struct{ name, user, token, want string }{
		{"different user", "UserOther", "UserABC", "newapi_user_id_mismatch"},
		{"case sensitive user", "userABC", "UserABC", "newapi_user_id_mismatch"},
		{"different token owner", "UserABC", "UserOther", "newapi_token_user_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connection := newAPITestConnection()
			connection.UserID = "UserABC"
			doer := &newAPITestDoer{handle: func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/api/user/self":
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"id":"`+tc.user+`","group":"Basic"}}`), nil
				case "/api/token/search":
					return newAPITestResponse(http.StatusOK, `{"success":true,"data":{"total":1,"items":[{"user_id":"`+tc.token+`","status":1,"group":"VIP"}]}}`), nil
				default:
					t.Fatalf("must stop before group lookup: %s", req.URL.Path)
					return nil, nil
				}
			}}
			_, err := NewNewAPIClient(doer).Resolve(t.Context(), connection)
			require.EqualError(t, err, tc.want)
		})
	}
}
