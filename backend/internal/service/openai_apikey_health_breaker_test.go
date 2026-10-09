package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIAPIKeyHealthSettingRepo struct {
	SettingRepository
	value    string
	getCalls int
}

func (r *openAIAPIKeyHealthSettingRepo) GetValue(context.Context, string) (string, error) {
	r.getCalls++
	return r.value, nil
}

type openAIAPIKeyHealthAccountRepo struct {
	AccountRepository
	setCalls   int
	reason     string
	modelCalls []string
}

func (r *openAIAPIKeyHealthAccountRepo) SetModelRateLimit(_ context.Context, _ int64, model string, _ time.Time, reason ...string) error {
	r.modelCalls = append(r.modelCalls, model)
	if len(reason) > 0 {
		r.reason = reason[0]
	}
	return nil
}

func TestOpenAIAPIKeyHealthBreakerDoesNotDisableOtherModels(t *testing.T) {
	encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{Enabled: true, WindowMinutes: 1, FailureThreshold: 3, CooldownMinutes: 5})
	require.NoError(t, err)
	repo := &openAIAPIKeyHealthAccountRepo{}
	cache := &openAIAPIKeyHealthCacheStub{tripped: true}
	limits := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
	limits.SetSettingService(NewSettingService(&openAIAPIKeyHealthSettingRepo{value: string(encoded)}, &config.Config{}))
	limits.SetOpenAIAPIKeyHealthCache(cache)
	account := openAIHealthPoolAccount()
	account.Status, account.Schedulable = StatusActive, true
	gateway := &OpenAIGatewayService{rateLimitService: limits}
	require.True(t, gateway.ReportOpenAIAccountScheduleResult(account, "gpt-6-astra", false, nil, &UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
	require.Zero(t, repo.setCalls)
	require.Equal(t, []string{"gpt-6-astra"}, repo.modelCalls)
	require.True(t, account.IsSchedulableForModel("gpt-5.6-sol"))
	require.False(t, account.IsSchedulableForModel("gpt-6-astra"))
}

func (r *openAIAPIKeyHealthAccountRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, reason string) error {
	r.setCalls++
	r.reason = reason
	return nil
}

type openAIAPIKeyHealthCacheStub struct {
	TempUnschedCache
	recordCalls int
	setCalls    int
	tripped     bool
}

func (c *openAIAPIKeyHealthCacheStub) RecordOpenAIAPIKeyHealthFailure(context.Context, int64, string, int, int) (int64, bool, error) {
	c.recordCalls++
	return 3, c.tripped, nil
}

func (c *openAIAPIKeyHealthCacheStub) SetTempUnsched(context.Context, int64, *TempUnschedState) error {
	c.setCalls++
	return nil
}

type openAIAPIKeyHealthRuntimeBlocker struct{ calls int }

func (b *openAIAPIKeyHealthRuntimeBlocker) BlockAccountScheduling(*Account, time.Time, string) {
	b.calls++
}
func (*openAIAPIKeyHealthRuntimeBlocker) ClearAccountSchedulingBlock(int64) {}

func openAIHealthPoolAccount() *Account {
	return &Account{
		ID:       42,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"pool_mode": true,
		},
	}
}

func TestClassifyOpenAIAPIKeyHealthFailureExclusions(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		eligible bool
	}{
		{name: "account attributed 502", err: &UpstreamFailoverError{StatusCode: http.StatusBadGateway}, eligible: true},
		{name: "request scoped capacity", err: &UpstreamFailoverError{StatusCode: 529, RequestScopedTransient: true}},
		{name: "provider scoped overload", err: &UpstreamFailoverError{StatusCode: 529, Scope: GatewayFailureScopeProvider}},
		{name: "dedicated same account retry", err: &UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, RetryableOnSameAccount: true}},
		{name: "credential disable path", err: &UpstreamFailoverError{StatusCode: http.StatusUnauthorized, Stage: GatewayFailureStageAccountAuth, Scope: GatewayFailureScopeAccount}},
		{name: "client request", err: &UpstreamFailoverError{StatusCode: http.StatusBadRequest}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, eligible := classifyOpenAIAPIKeyHealthFailure(tt.err)
			require.Equal(t, tt.eligible, eligible)
		})
	}
}

func TestOpenAIAPIKeyHealthBreakerDefaultDisabled(t *testing.T) {
	settings := NewSettingService(&openAIAPIKeyHealthSettingRepo{}, &config.Config{})
	cache := &openAIAPIKeyHealthCacheStub{tripped: true}
	svc := NewRateLimitService(&openAIAPIKeyHealthAccountRepo{}, nil, &config.Config{}, nil, cache)
	svc.SetSettingService(settings)
	svc.SetOpenAIAPIKeyHealthCache(cache)

	require.False(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), openAIHealthPoolAccount(), "gpt-6-astra", &UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
	require.Zero(t, cache.recordCalls)
}

func TestOpenAIAPIKeyHealthBreakerTripsOnlyModelState(t *testing.T) {
	encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{Enabled: true, WindowMinutes: 1, FailureThreshold: 3, CooldownMinutes: 5})
	require.NoError(t, err)
	settings := NewSettingService(&openAIAPIKeyHealthSettingRepo{value: string(encoded)}, &config.Config{})
	cache := &openAIAPIKeyHealthCacheStub{tripped: true}
	repo := &openAIAPIKeyHealthAccountRepo{}
	blocker := &openAIAPIKeyHealthRuntimeBlocker{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
	svc.SetSettingService(settings)
	svc.SetOpenAIAPIKeyHealthCache(cache)
	svc.SetAccountRuntimeBlocker(blocker)
	account := openAIHealthPoolAccount()

	require.True(t, svc.ObserveOpenAIAPIKeyHealthFailure(context.Background(), account, "gpt-6-astra", &UpstreamFailoverError{StatusCode: http.StatusBadGateway, ResponseBody: []byte(`{"error":"upstream"}`)}))
	require.Equal(t, 1, cache.recordCalls)
	require.Zero(t, cache.setCalls)
	require.Zero(t, repo.setCalls)
	require.Equal(t, []string{"gpt-6-astra"}, repo.modelCalls)
	require.Zero(t, blocker.calls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.True(t, account.isRateLimitActiveForKey("gpt-6-astra"))
	require.Contains(t, repo.reason, openAIAPIKeyHealthBreakerReason)
}

func TestOpenAIAPIKeyHealthSuccessDoesNotTouchSettingsOrCache(t *testing.T) {
	encoded, err := json.Marshal(OpenAIAPIKeyHealthBreakerSettings{Enabled: true, WindowMinutes: 1, FailureThreshold: 3, CooldownMinutes: 5})
	require.NoError(t, err)
	settingRepo := &openAIAPIKeyHealthSettingRepo{value: string(encoded)}
	settings := NewSettingService(settingRepo, &config.Config{})
	cache := &openAIAPIKeyHealthCacheStub{}
	svc := NewRateLimitService(&openAIAPIKeyHealthAccountRepo{}, nil, &config.Config{}, nil, cache)
	svc.SetSettingService(settings)
	svc.SetOpenAIAPIKeyHealthCache(cache)

	svc.ObserveOpenAIAPIKeyHealthSuccess(context.Background(), openAIHealthPoolAccount())
	svc.ObserveOpenAIAPIKeyHealthSuccess(context.Background(), &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeAPIKey})
	require.Zero(t, settingRepo.getCalls)
	require.Zero(t, cache.recordCalls)
}
