//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorModelProbeConcurrencyIsTwoPerChannel(t *testing.T) {
	require.Equal(t, 2, channelMonitorModelProbeConcurrency)
}

type autoMonitorAccountReaderStub struct {
	accounts []Account
}

func (s autoMonitorAccountReaderStub) ListModelAvailabilityCandidates(
	_ context.Context,
	_ *int64,
	platforms []string,
	_ bool,
) ([]Account, error) {
	platform := ""
	if len(platforms) > 0 {
		platform = platforms[0]
	}
	out := make([]Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

type autoMonitorSettingStoreStub struct {
	values map[string]string
}

func (s *autoMonitorSettingStoreStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = s.values[key]
	}
	return out, nil
}

func (s *autoMonitorSettingStoreStub) SetMultiple(_ context.Context, values map[string]string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

func TestResolveMonitorModelsAddsWhitelistedPoolModelsWithoutDroppingManualModels(t *testing.T) {
	svc := NewChannelMonitorService(nil, nil)
	svc.SetAutoModelDependencies(autoMonitorAccountReaderStub{accounts: []Account{
		{
			Platform: PlatformOpenAI,
			Credentials: map[string]any{"model_mapping": map[string]any{
				"gpt-5.6":     "internal-upstream-model",
				"gpt-4o-mini": "gpt-4o-mini",
			}},
		},
	}}, &autoMonitorSettingStoreStub{values: map[string]string{
		SettingKeyChannelMonitorAutoModelsEnabled:   "true",
		SettingKeyChannelMonitorAutoModelsWhitelist: `["gpt-5.*"]`,
	}})

	models := svc.resolveMonitorModels(context.Background(), &ChannelMonitor{
		ID: 1, Provider: MonitorProviderOpenAI, PrimaryModel: "manual-primary", ExtraModels: []string{"manual-extra"},
	})

	require.Equal(t, []string{"manual-primary", "manual-extra", "gpt-5.6"}, models)
}

func TestAutoMonitorPolicyDefaultsToEnabledAndEmptyWhitelist(t *testing.T) {
	svc := NewChannelMonitorService(nil, nil)
	svc.SetAutoModelDependencies(autoMonitorAccountReaderStub{}, &autoMonitorSettingStoreStub{values: map[string]string{}})

	policy, err := svc.GetAutoModelPolicy(context.Background(), false)

	require.NoError(t, err)
	require.True(t, policy.Enabled)
	require.Empty(t, policy.Whitelist)
}

func TestUpdateAutoMonitorPolicyRejectsMidPatternWildcard(t *testing.T) {
	svc := NewChannelMonitorService(nil, nil)
	svc.SetAutoModelDependencies(autoMonitorAccountReaderStub{}, &autoMonitorSettingStoreStub{})

	policy, err := svc.UpdateAutoModelPolicy(context.Background(), ChannelMonitorAutoModelPolicy{
		Enabled: true, Whitelist: []string{"gpt-*-mini"},
	})

	require.Nil(t, policy)
	require.ErrorIs(t, err, ErrChannelMonitorInvalidAutoModelWhitelist)
}

func TestDisabledAutoMonitorPolicyUsesOnlyManualModels(t *testing.T) {
	svc := NewChannelMonitorService(nil, nil)
	svc.SetAutoModelDependencies(autoMonitorAccountReaderStub{accounts: []Account{{
		Platform:    PlatformOpenAI,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.6": "gpt-5.6"}},
	}}}, &autoMonitorSettingStoreStub{values: map[string]string{
		SettingKeyChannelMonitorAutoModelsEnabled: "false",
	}})

	models := svc.resolveMonitorModels(context.Background(), &ChannelMonitor{
		Provider: MonitorProviderOpenAI, PrimaryModel: "manual-primary",
	})

	require.Equal(t, []string{"manual-primary"}, models)
}

func TestChannelMonitorModelsForAccountUsesRequestedAliasesInStableOrder(t *testing.T) {
	models := channelMonitorModelsForAccount(&Account{
		Platform: PlatformOpenAI,
		Credentials: map[string]any{"model_mapping": map[string]any{
			"gpt-5.6-terra": "upstream-terra",
			"gpt-5.6-*":     "wildcard-must-not-be-probed",
			"gpt-5.6-sol":   "upstream-sol",
		}},
	})

	require.Equal(t, []string{"gpt-5.6-sol", "gpt-5.6-terra"}, models)
}
