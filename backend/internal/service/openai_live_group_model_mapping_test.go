package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type liveMappingCreateStore struct{ liveTestStore }

func (s *liveMappingCreateStore) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (s *liveMappingCreateStore) ClaimLiveController(context.Context, string, string, string) (bool, error) {
	return false, nil // This test observes create-time forwarding, not sideband I/O.
}

type liveMappingCreateScheduler struct {
	OpenAIAccountScheduler
	account        *Account
	requestedModel string
}

func (s *liveMappingCreateScheduler) Select(_ context.Context, request OpenAIAccountScheduleRequest) (*AccountSelectionResult, OpenAIAccountScheduleDecision, error) {
	s.requestedModel = request.RequestedModel
	if request.RequestedModel != "" && !s.account.IsModelSupported(request.RequestedModel) {
		return nil, OpenAIAccountScheduleDecision{}, ErrNoAvailableAccounts
	}
	return &AccountSelectionResult{Account: s.account, Acquired: true, ReleaseFunc: func() {}}, OpenAIAccountScheduleDecision{}, nil
}

func TestCreateLiveCallOnlyFiltersModelsForGroupMappings(t *testing.T) {
	defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
	for _, tt := range []struct {
		name           string
		groupMapped    bool
		model          string
		accountMapping map[string]any
		upstreamModel  string
	}{
		{"unmapped text allowlist", false, "gpt-live-test", map[string]any{"gpt-5.5": "gpt-5.5"}, "gpt-live-test"},
		{"unmapped live alias", false, "gpt-live-test", map[string]any{"gpt-live-test": "provider-live"}, "gpt-live-test"},
		{"group target account alias", true, "terra", map[string]any{"terra": "provider-terra"}, "provider-terra"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{JWT: config.JWTConfig{Secret: "live-group-mapping-test"}}
			account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2, Credentials: map[string]any{
				"access_token": "test-access-token", "chatgpt_account_id": "acct_test", "model_mapping": tt.accountMapping,
			}}
			store := &liveMappingCreateStore{}
			upstream := &liveHTTPUpstreamStub{}
			scheduler := &liveMappingCreateScheduler{account: account}
			svc := &OpenAIGatewayService{
				cfg: cfg, cache: store, httpUpstream: upstream, openaiScheduler: scheduler,
				concurrencyService: NewConcurrencyService(&liveTestConcurrencyCache{}),
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
				liveAttestation:    liveAttestationStub{header: "test-attestation"}, liveAttestationCipher: newLiveAttestationCipher(cfg),
			}
			identity := LiveCallIdentity{APIKeyID: 10, UserID: 20}
			if tt.groupMapped {
				identity.ChannelUsageFields = ChannelUsageFields{GroupMapped: true, OriginalModel: "luna", ChannelMappedModel: "terra", ModelMappingChain: "luna→terra"}
			}
			created, err := svc.CreateLiveCall(context.Background(), &LiveCallRequest{SDP: "offer", Session: json.RawMessage(`{"model":"` + tt.model + `","instructions":"hello"}`)}, identity, 2)
			require.NoError(t, err)
			require.Equal(t, "call_test", created.CallID)
			require.Equal(t, tt.upstreamModel, gjson.GetBytes(upstream.body, "session.model").String())
			record, err := store.GetLiveCall(context.Background(), hashLiveCallID(created.CallID))
			require.NoError(t, err)
			if tt.groupMapped {
				require.Equal(t, tt.model, scheduler.requestedModel)
				require.Equal(t, "luna", record.OriginalModel)
				require.Equal(t, "terra", record.ChannelMappedModel)
				require.Equal(t, "luna→terra→provider-terra", record.ModelMappingChain)
			} else {
				require.Empty(t, scheduler.requestedModel, "ordinary Live requests retain capability-only account selection")
			}
		})
	}
}

func TestLiveCallAccountMappingPreservesGroupTargetAcrossRetries(t *testing.T) {
	request := &LiveCallRequest{SDP: "test-sdp", Session: json.RawMessage(`{"model":"terra","instructions":"keep this","audio":{"voice":"marin"}}`)}
	for _, providerModel := range []string{"provider-one", "provider-two"} {
		account := &Account{Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"terra": providerModel}}}
		mapped, upstreamModel, err := liveCallRequestForAccount(request, account)
		require.NoError(t, err)
		require.Equal(t, providerModel, upstreamModel)
		require.Equal(t, request.SDP, mapped.SDP)
		require.JSONEq(t, `{"model":"`+providerModel+`","instructions":"keep this","audio":{"voice":"marin"}}`, string(mapped.Session))
		require.JSONEq(t, `{"model":"terra","instructions":"keep this","audio":{"voice":"marin"}}`, string(request.Session))
	}
}

func TestFinalizeLiveCallPreservesGroupModelMappingSnapshot(t *testing.T) {
	record := &LiveCallRecord{
		CallID: "mapped-live", CallHash: hashLiveCallID("mapped-live"), AccountID: 11, APIKeyID: 22, UserID: 33, GroupID: 44,
		Model: "terra", UpstreamModel: "provider-terra", CreatedAt: time.Now().Add(-time.Second), ExpiresAt: time.Now().Add(time.Hour), Controller: LiveControllerPending,
		ChannelUsageFields: ChannelUsageFields{GroupMapped: true, ChannelID: 55, OriginalModel: "luna", ChannelMappedModel: "terra", BillingModelSource: BillingModelSourceChannelMapped, ModelMappingChain: "luna→terra→provider-terra"},
	}
	store := &liveTestStore{}
	require.NoError(t, store.SaveLiveCall(context.Background(), record, time.Hour))
	usageRepo := &liveTestUsageRepo{}
	svc := &OpenAIGatewayService{cache: store, concurrencyService: NewConcurrencyService(&liveTestConcurrencyCache{}), usageLogRepo: usageRepo}
	svc.finalizeLiveCall(record)
	require.Len(t, usageRepo.logs, 1)
	usageLog := usageRepo.logs[0]
	require.Equal(t, "luna", usageLog.RequestedModel)
	require.Equal(t, "terra", usageLog.Model)
	require.Equal(t, "provider-terra", *usageLog.UpstreamModel)
	require.Equal(t, int64(55), *usageLog.ChannelID)
	require.Equal(t, "luna→terra→provider-terra", *usageLog.ModelMappingChain)
	require.Zero(t, usageLog.ActualCost, "group mappings retain Live's existing zero-cost recording semantics")
}
