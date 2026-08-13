package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Never let one v1 monitor occupy more than two upstream slots at once.
const channelMonitorModelProbeConcurrency = channelMonitorMaxConcurrentModelsPerChannel

const SchedulerProbeAccountModelWhitelistExtraKey = "channel_monitor_model_whitelist"

// ChannelMonitorAccountModelWhitelistExtraKey remains an on-disk compatibility
// alias; scheduler probes no longer depend on the channel-monitor feature flag.
const ChannelMonitorAccountModelWhitelistExtraKey = SchedulerProbeAccountModelWhitelistExtraKey

type channelMonitorAccountModelReader interface {
	// This persistent candidate view intentionally keeps temporarily unhealthy,
	// rate-limited, or overloaded accounts in the inventory. Otherwise a model
	// would disappear from monitoring at the exact moment it becomes unhealthy.
	ListModelAvailabilityCandidates(ctx context.Context, groupID *int64, platforms []string, includeGrouped bool) ([]Account, error)
}

type channelMonitorAutoModelSettingStore interface {
	GetMultiple(ctx context.Context, keys []string) (map[string]string, error)
	SetMultiple(ctx context.Context, settings map[string]string) error
}

// ChannelMonitorAutoModelPolicy controls scheduler account-pool probes. Its
// legacy name is retained for storage and handler compatibility.
type ChannelMonitorAutoModelPolicy struct {
	Enabled              bool                `json:"enabled"`
	Whitelist            []string            `json:"whitelist"`
	DiscoveredByProvider map[string][]string `json:"discovered_by_provider,omitempty"`
	EligibleByProvider   map[string][]string `json:"eligible_by_provider,omitempty"`
}

type ChannelMonitorAccountModelPolicy struct {
	AccountID        int64    `json:"account_id"`
	Whitelist        []string `json:"whitelist"`
	DiscoveredModels []string `json:"discovered_models"`
	EffectiveModels  []string `json:"effective_models"`
}

func defaultChannelMonitorAutoModelPolicy() ChannelMonitorAutoModelPolicy {
	return ChannelMonitorAutoModelPolicy{Enabled: true, Whitelist: []string{}}
}

func (s *ChannelMonitorService) GetAutoModelPolicy(ctx context.Context, includeInventory bool) (*ChannelMonitorAutoModelPolicy, error) {
	policy, err := s.loadAutoModelPolicy(ctx)
	if err != nil {
		return nil, err
	}
	if !includeInventory {
		return &policy, nil
	}
	policy.DiscoveredByProvider = make(map[string][]string, 4)
	policy.EligibleByProvider = make(map[string][]string, 4)
	for _, provider := range []string{MonitorProviderOpenAI, MonitorProviderAnthropic, MonitorProviderGemini, MonitorProviderGrok} {
		models, discoverErr := s.discoverPoolModels(ctx, provider)
		if discoverErr != nil {
			return nil, discoverErr
		}
		policy.DiscoveredByProvider[provider] = models
		policy.EligibleByProvider[provider] = filterAutoMonitorModels(models, policy.Whitelist)
	}
	return &policy, nil
}

func (s *ChannelMonitorService) UpdateAutoModelPolicy(ctx context.Context, input ChannelMonitorAutoModelPolicy) (*ChannelMonitorAutoModelPolicy, error) {
	if s == nil || s.settings == nil {
		return nil, fmt.Errorf("scheduler probe settings are unavailable")
	}
	whitelist, err := normalizeAutoMonitorWhitelist(input.Whitelist)
	if err != nil {
		return nil, err
	}
	rawWhitelist, err := json.Marshal(whitelist)
	if err != nil {
		return nil, fmt.Errorf("marshal scheduler probe model whitelist: %w", err)
	}
	if err := s.settings.SetMultiple(ctx, map[string]string{
		SettingKeySchedulerProbesEnabled:   fmt.Sprintf("%t", input.Enabled),
		SettingKeySchedulerProbesWhitelist: string(rawWhitelist),
	}); err != nil {
		return nil, fmt.Errorf("save scheduler probe policy: %w", err)
	}
	return s.GetAutoModelPolicy(ctx, true)
}

func (s *ChannelMonitorService) GetAccountModelPolicy(ctx context.Context, accountID int64) (*ChannelMonitorAccountModelPolicy, error) {
	if s == nil || s.poolAccounts == nil {
		return nil, fmt.Errorf("scheduler probe account policies are unavailable")
	}
	account, err := s.poolAccounts.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	global, err := s.loadAutoModelPolicy(ctx)
	if err != nil {
		return nil, err
	}
	discovered := channelMonitorModelsForAccount(account)
	whitelist := channelMonitorAccountModelWhitelist(account)
	effective := filterAutoMonitorModels(discovered, global.Whitelist)
	effective = filterAutoMonitorModels(effective, whitelist)
	return &ChannelMonitorAccountModelPolicy{
		AccountID: account.ID, Whitelist: whitelist, DiscoveredModels: discovered, EffectiveModels: effective,
	}, nil
}

func (s *ChannelMonitorService) UpdateAccountModelPolicy(ctx context.Context, accountID int64, whitelist []string) (*ChannelMonitorAccountModelPolicy, error) {
	if s == nil || s.poolAccounts == nil {
		return nil, fmt.Errorf("scheduler probe account policies are unavailable")
	}
	normalized, err := normalizeAutoMonitorWhitelist(whitelist)
	if err != nil {
		return nil, err
	}
	if _, err := s.poolAccounts.GetByID(ctx, accountID); err != nil {
		return nil, err
	}
	if err := s.poolAccounts.UpdateExtra(ctx, accountID, map[string]any{
		SchedulerProbeAccountModelWhitelistExtraKey: normalized,
	}); err != nil {
		return nil, err
	}
	return s.GetAccountModelPolicy(ctx, accountID)
}

func channelMonitorAccountModelWhitelist(account *Account) []string {
	if account == nil || account.Extra == nil {
		return []string{}
	}
	raw, ok := account.Extra[SchedulerProbeAccountModelWhitelistExtraKey]
	if !ok || raw == nil {
		return []string{}
	}
	values := make([]string, 0)
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok {
				values = append(values, value)
			}
		}
	}
	normalized, err := normalizeAutoMonitorWhitelist(values)
	if err != nil {
		return []string{}
	}
	return normalized
}

func (s *ChannelMonitorService) loadAutoModelPolicy(ctx context.Context) (ChannelMonitorAutoModelPolicy, error) {
	return loadAutoModelPolicyFromStore(ctx, s.settings)
}

func loadAutoModelPolicyFromStore(ctx context.Context, settings channelMonitorAutoModelSettingStore) (ChannelMonitorAutoModelPolicy, error) {
	policy := defaultChannelMonitorAutoModelPolicy()
	if settings == nil {
		return policy, nil
	}
	values, err := settings.GetMultiple(ctx, []string{
		SettingKeySchedulerProbesEnabled,
		SettingKeySchedulerProbesWhitelist,
	})
	if err != nil {
		return policy, fmt.Errorf("load scheduler probe policy: %w", err)
	}
	policy.Enabled = !isFalseSettingValue(values[SettingKeySchedulerProbesEnabled])
	if raw := strings.TrimSpace(values[SettingKeySchedulerProbesWhitelist]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &policy.Whitelist); err != nil {
			return policy, fmt.Errorf("decode scheduler probe model whitelist: %w", err)
		}
	}
	policy.Whitelist, err = normalizeAutoMonitorWhitelist(policy.Whitelist)
	if err != nil {
		return policy, err
	}
	return policy, nil
}

func (s *ChannelMonitorService) resolveMonitorModels(_ context.Context, monitor *ChannelMonitor) []string {
	// Manual/public channel monitors probe exactly their configured models.
	// Account-pool discovery belongs exclusively to scheduler probes, preventing
	// duplicate upstream requests when both features are enabled.
	return normalizeModels(append([]string{monitor.PrimaryModel}, monitor.ExtraModels...))
}

func (s *ChannelMonitorService) discoverPoolModels(ctx context.Context, provider string) ([]string, error) {
	if s == nil || s.accounts == nil {
		return []string{}, nil
	}
	accounts, err := s.accounts.ListModelAvailabilityCandidates(ctx, nil, []string{provider}, true)
	if err != nil {
		return nil, fmt.Errorf("list schedulable %s accounts: %w", provider, err)
	}
	models := make([]string, 0)
	for i := range accounts {
		mapping := accounts[i].GetModelMapping()
		if len(mapping) == 0 {
			models = append(models, defaultModelsListCandidateIDs(provider)...)
			continue
		}
		// Only monitor the client-facing model aliases (mapping keys). Mapping
		// values are upstream implementation details and may not be valid input
		// for this monitor endpoint.
		for requested := range mapping {
			if requested = strings.TrimSpace(requested); requested != "" && !strings.Contains(requested, "*") {
				models = append(models, requested)
			}
		}
	}
	models = normalizeModels(models)
	sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i]) < strings.ToLower(models[j]) })
	return models, nil
}

func normalizeAutoMonitorWhitelist(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Count(value, "*") > 1 || (strings.Contains(value, "*") && !strings.HasSuffix(value, "*")) {
			return nil, fmt.Errorf("%w: %q", ErrChannelMonitorInvalidAutoModelWhitelist, value)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

func filterAutoMonitorModels(models, whitelist []string) []string {
	if len(whitelist) == 0 {
		return append([]string(nil), models...)
	}
	out := make([]string, 0, len(models))
	for _, model := range models {
		lowerModel := strings.ToLower(model)
		for _, pattern := range whitelist {
			lowerPattern := strings.ToLower(pattern)
			if lowerModel == lowerPattern || matchWildcard(lowerPattern, lowerModel) {
				out = append(out, model)
				break
			}
		}
	}
	return out
}
