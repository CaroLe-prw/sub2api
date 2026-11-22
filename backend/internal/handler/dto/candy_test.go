package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCandyModelOnlyAppearsInAdminGroup(t *testing.T) {
	group := &service.Group{ID: 7, Platform: service.PlatformOpenAI, CandyTestModel: "gpt-6.1-sol", CandyTestIntervalSeconds: 60, CandyTestReasoningEffort: "high"}
	admin, err := json.Marshal(GroupFromServiceAdmin(group))
	require.NoError(t, err)
	require.Contains(t, string(admin), `"candy_test_model":"gpt-6.1-sol"`)
	user, err := json.Marshal(GroupFromService(group))
	require.NoError(t, err)
	require.NotContains(t, string(user), "candy_test_model")
	require.NotContains(t, string(user), "candy_test_reasoning_effort")
	require.Contains(t, string(admin), `"candy_test_reasoning_effort":"high"`)
	require.NotContains(t, string(user), "candy_test_interval_seconds")
	require.Contains(t, string(admin), `"candy_test_interval_seconds":60`)
}

func TestCandyHistorySurvivesLiteAccountProjection(t *testing.T) {
	account := &service.Account{ID: 42, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Extra: map[string]any{service.CandyTestExtraKey: service.CandyTestSnapshot{Total: 1, Passed: 1, Recent: []service.CandyTestSample{{Status: "passed", Model: "gpt-6.2-sol", ResponseText: "保存的回答：21", ResponseTruncated: true}}}}}
	item := AccountListItemFromAccount(AccountFromServiceShallow(account))
	encoded, err := json.Marshal(item)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"candy_test":{"total":1,"passed":1`)
	require.Contains(t, string(encoded), `"response_text":"保存的回答：21"`)
	require.Contains(t, string(encoded), `"response_truncated":true`)
}
