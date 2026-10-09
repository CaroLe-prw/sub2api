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

type modelSchedulingRepo struct {
	AccountRepository
	account *Account
	cleared []string
}

func (r *modelSchedulingRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *modelSchedulingRepo) ClearError(context.Context, int64) error {
	r.account.Status = StatusActive
	return nil
}
func (r *modelSchedulingRepo) SetModelRateLimit(_ context.Context, _ int64, model string, until time.Time, reason ...string) error {
	message := ""
	if len(reason) > 0 {
		message = reason[0]
	}
	setAccountModelRateLimitSnapshot(r.account, model, until, message, time.Now())
	return nil
}
func (r *modelSchedulingRepo) SetModelError(_ context.Context, _ int64, model, reason string) error {
	setAccountModelErrorSnapshot(r.account, model, reason)
	return nil
}
func (r *modelSchedulingRepo) ClearModelRateLimit(_ context.Context, _ int64, model string) error {
	r.cleared = append(r.cleared, model)
	limits, _ := r.account.Extra[modelRateLimitsKey].(map[string]any)
	delete(limits, model)
	return nil
}

type modelTimeoutCounter struct{ counts map[string]int64 }

func (c *modelTimeoutCounter) IncrementTimeoutCount(_ context.Context, _ int64, model string, _ int) (int64, error) {
	c.counts[model]++
	return c.counts[model], nil
}
func (c *modelTimeoutCounter) GetTimeoutCount(_ context.Context, _ int64, model string) (int64, error) {
	return c.counts[model], nil
}
func (c *modelTimeoutCounter) ResetTimeoutCount(_ context.Context, _ int64, model string) error {
	delete(c.counts, model)
	return nil
}
func (c *modelTimeoutCounter) GetTimeoutCountTTL(context.Context, int64, string) (time.Duration, error) {
	return time.Minute, nil
}

func TestStreamTimeoutActionsAndRecoveryAreModelScoped(t *testing.T) {
	for _, action := range []string{StreamTimeoutActionTempUnsched, StreamTimeoutActionError} {
		t.Run(action, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
			repo := &modelSchedulingRepo{account: account}
			counter := &modelTimeoutCounter{counts: map[string]int64{}}
			encoded, err := json.Marshal(StreamTimeoutSettings{Enabled: true, Action: action, TempUnschedMinutes: 5, ThresholdCount: 2, ThresholdWindowMinutes: 1})
			require.NoError(t, err)
			svc := &RateLimitService{accountRepo: repo, timeoutCounterCache: counter, settingService: NewSettingService(&openAIAPIKeyHealthSettingRepo{value: string(encoded)}, &config.Config{})}
			ctx := context.Background()
			require.False(t, svc.HandleStreamTimeout(ctx, account, "gpt-6-astra"))
			require.False(t, svc.HandleStreamTimeout(ctx, account, "gpt-5.6-sol"))
			require.True(t, svc.HandleStreamTimeout(ctx, account, "gpt-6-astra"))
			require.True(t, account.IsSchedulable())
			require.False(t, account.IsSchedulableForModel("gpt-6-astra"))
			require.True(t, account.IsSchedulableForModel("gpt-5.6-sol"))
			require.EqualValues(t, 1, counter.counts["gpt-5.6-sol"])
			_, err = svc.RecoverAccountModelAfterSuccessfulTest(ctx, 42, "gpt-5.6-sol")
			require.NoError(t, err)
			require.Empty(t, repo.cleared)
			require.False(t, account.IsSchedulableForModel("gpt-6-astra"))
			require.True(t, svc.HandleStreamTimeout(ctx, account, "gpt-5.6-sol"))
			_, err = svc.RecoverAccountModelAfterSuccessfulTest(ctx, 42, "gpt-6-astra")
			require.NoError(t, err)
			require.Equal(t, []string{"gpt-6-astra"}, repo.cleared)
			require.True(t, account.IsSchedulableForModel("gpt-6-astra"))
			require.False(t, account.IsSchedulableForModel("gpt-5.6-sol"))
		})
	}
}

func TestModelSchedulingBlocksUseCompactUpstreamModel(t *testing.T) {
	for _, block := range []string{"persisted_cooldown", "persistent_error", "transient"} {
		t.Run(block, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"model_mapping": map[string]any{"public": "normal-model"}, "compact_model_mapping": map[string]any{"public": "compact-model"}}}
			svc := &OpenAIGatewayService{}
			blockModel := func(model string) {
				switch block {
				case "persisted_cooldown":
					setAccountModelRateLimitSnapshot(account, model, time.Now().Add(time.Minute), "test", time.Now())
				case "persistent_error":
					setAccountModelErrorSnapshot(account, model, "test")
				case "transient":
					svc.recordOpenAIAccountModelTransientFailure(account, model, time.Now())
					svc.recordOpenAIAccountModelTransientFailure(account, model, time.Now())
				}
			}
			blockModel("normal-model")
			scheduler := &defaultOpenAIAccountScheduler{service: svc}
			require.False(t, scheduler.isAccountRequestCompatible(context.Background(), account, OpenAIAccountScheduleRequest{RequestedModel: "public"}))
			require.True(t, scheduler.isAccountRequestCompatible(context.Background(), account, OpenAIAccountScheduleRequest{RequestedModel: "public", RequireCompact: true}))
			// Capability rechecks may defer compact admission; model health must
			// still use the compact mapping throughout the full selection path.
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{*account}}
			selection, _, err := scheduler.Select(context.Background(), OpenAIAccountScheduleRequest{RequestedModel: "public", RequireCompact: true})
			require.NoError(t, err)
			require.NotNil(t, selection)
			selection.ReleaseFunc()
			blockModel("compact-model")
			require.False(t, scheduler.isAccountRequestCompatible(context.Background(), account, OpenAIAccountScheduleRequest{RequestedModel: "public", RequireCompact: true}))
		})
	}
}

func TestOpenAI429RetryWindowsAndCooldownAreModelScoped(t *testing.T) {
	ctx := context.Background()
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	repo := &modelSchedulingRepo{account: account}
	limits := &RateLimitService{accountRepo: repo, cfg: &config.Config{}}
	svc := &OpenAIGatewayService{rateLimitService: limits}
	require.True(t, svc.openAIOAuth429RetryWindowActive(account, "gpt-6-astra"))
	require.True(t, svc.openAIOAuth429RetryWindowActive(account, "gpt-5.6-sol"))
	expired := time.Now().Add(-openAIOAuth429RetryWindow - time.Second)
	svc.openaiOAuth429RetryStartedAt.Store(openAI429RetryKey(account.ID, "gpt-6-astra"), expired)
	svc.markOpenAIOAuth429RateLimited(withTempUnschedulableModel(ctx, []string{"gpt-6-astra"}), account, nil, nil)
	require.False(t, account.IsSchedulableForModel("gpt-6-astra"))
	require.True(t, account.IsSchedulableForModel("gpt-5.6-sol"))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, http.StatusTooManyRequests, false, nil, nil, "gpt-6-astra"))
	require.True(t, svc.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, http.StatusTooManyRequests, false, nil, nil, "gpt-5.6-sol"))
	svc.openaiOAuth429RetryStartedAt.Store(openAI429RetryKey(account.ID, "gpt-6-astra"), expired)
	svc.ReportOpenAIAccountScheduleResult(account, "gpt-5.6-sol", true, nil)
	_, found := svc.openaiOAuth429RetryStartedAt.Load(openAI429RetryKey(account.ID, "gpt-6-astra"))
	require.True(t, found)
}

func TestPermanentModelErrorIsNotClearedByCooldown(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Extra: map[string]any{"allow_overages": true}}
	setAccountModelErrorSnapshot(account, "gemini-2.5-pro", "repeated timeout")
	setAccountModelRateLimitSnapshot(account, "gemini-2.5-pro", time.Now().Add(time.Minute), "transient", time.Now())
	require.True(t, account.isModelErrorForKey("gemini-2.5-pro"))
	require.False(t, account.IsSchedulableForModel("gemini-2.5-pro"))
}

func TestBedrockModelStateUsesActualUpstreamID(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformAnthropic, Type: AccountTypeBedrock, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"aws_region": "us-east-1", "model_mapping": map[string]any{"public": "anthropic.claude-sonnet-4-20250514-v1:0"}}}
	model, ok := ResolveBedrockModelID(account, "public")
	require.True(t, ok)
	require.NotEqual(t, "public", model)
	setAccountModelErrorSnapshot(account, model, "timeout")
	require.False(t, account.IsSchedulableForModel("public"))
	require.Equal(t, model, accountSchedulingUpstreamModel(context.Background(), account, "public"))
}

func TestSuccessfulModelProbeRecoversCredentialErrorWithoutClearingOtherModels(t *testing.T) {
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusError, Schedulable: true}
	setAccountModelErrorSnapshot(account, "gpt-6-astra", "timeout")
	repo := &modelSchedulingRepo{account: account}
	svc := &RateLimitService{accountRepo: repo}
	result, err := svc.RecoverAccountModelAfterSuccessfulTest(context.Background(), 42, "gpt-5.6-sol")
	require.NoError(t, err)
	require.True(t, result.ClearedError)
	require.True(t, account.IsSchedulableForModel("gpt-5.6-sol"))
	require.False(t, account.IsSchedulableForModel("gpt-6-astra"))
	require.Empty(t, repo.cleared)
}

func TestFallback429AndOverloadDoNotPauseOtherModels(t *testing.T) {
	for _, status := range []int{429, 529} {
		account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
		repo := &modelSchedulingRepo{account: account}
		svc := &RateLimitService{accountRepo: repo, cfg: &config.Config{}}
		ctx := withTempUnschedulableModel(context.Background(), []string{"gpt-6-astra"})
		if status == 429 {
			svc.apply429FallbackRateLimit(ctx, account, "no_reset")
		} else {
			svc.handle529(ctx, account)
		}
		require.True(t, account.IsSchedulable())
		require.False(t, account.IsSchedulableForModel("gpt-6-astra"))
		require.True(t, account.IsSchedulableForModel("gpt-5.6-sol"))
	}
}
