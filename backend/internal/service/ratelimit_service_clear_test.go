//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type rateLimitClearRepoStub struct {
	mockAccountRepoForGemini
	getByIDAccount            *Account
	getByIDErr                error
	getByIDCalls              int
	clearErrorCalls           int
	clearRateLimitCalls       int
	clearAntigravityCalls     int
	clearModelRateLimitCalls  int
	clearTempUnschedCalls     int
	clearErrorErr             error
	clearRateLimitErr         error
	clearAntigravityErr       error
	clearModelRateLimitErr    error
	clearTempUnschedulableErr error
	setTempUnschedCalls       int
	setTempUnschedUntil       time.Time
	setTempUnschedReason      string
}

func (r *rateLimitClearRepoStub) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.getByIDCalls++
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	return r.getByIDAccount, nil
}

func (r *rateLimitClearRepoStub) ClearError(ctx context.Context, id int64) error {
	r.clearErrorCalls++
	return r.clearErrorErr
}

func (r *rateLimitClearRepoStub) ClearRateLimit(ctx context.Context, id int64) error {
	r.clearRateLimitCalls++
	return r.clearRateLimitErr
}

func (r *rateLimitClearRepoStub) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	r.clearAntigravityCalls++
	return r.clearAntigravityErr
}

func (r *rateLimitClearRepoStub) ClearModelRateLimits(ctx context.Context, id int64) error {
	r.clearModelRateLimitCalls++
	return r.clearModelRateLimitErr
}

func (r *rateLimitClearRepoStub) ClearTempUnschedulable(ctx context.Context, id int64) error {
	r.clearTempUnschedCalls++
	return r.clearTempUnschedulableErr
}

func (r *rateLimitClearRepoStub) SetTempUnschedulable(_ context.Context, _ int64, until time.Time, reason string) error {
	r.setTempUnschedCalls++
	r.setTempUnschedUntil = until
	r.setTempUnschedReason = reason
	if r.getByIDAccount != nil {
		r.getByIDAccount.TempUnschedulableUntil = &until
		r.getByIDAccount.TempUnschedulableReason = reason
	}
	return nil
}

type tempUnschedCacheRecorder struct {
	deletedIDs []int64
	deleteErr  error
	states     map[int64]*TempUnschedState
}

type recoverTokenInvalidatorStub struct {
	accounts []*Account
	err      error
}

func (c *tempUnschedCacheRecorder) SetTempUnsched(ctx context.Context, accountID int64, state *TempUnschedState) error {
	if c.states == nil {
		c.states = make(map[int64]*TempUnschedState)
	}
	c.states[accountID] = state
	return nil
}

func (c *tempUnschedCacheRecorder) GetTempUnsched(ctx context.Context, accountID int64) (*TempUnschedState, error) {
	return nil, nil
}

func (c *tempUnschedCacheRecorder) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	c.deletedIDs = append(c.deletedIDs, accountID)
	return c.deleteErr
}

func (s *recoverTokenInvalidatorStub) InvalidateToken(ctx context.Context, account *Account) error {
	s.accounts = append(s.accounts, account)
	return s.err
}

func TestRateLimitService_ClearRateLimit_AlsoClearsTempUnschedulable(t *testing.T) {
	repo := &rateLimitClearRepoStub{}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 42)
	require.NoError(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Equal(t, []int64{42}, cache.deletedIDs)
}

func TestRateLimitService_PauseAccountScheduling(t *testing.T) {
	account := &Account{ID: 42, Status: StatusActive, Schedulable: true}
	repo := &rateLimitClearRepoStub{getByIDAccount: account}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	updated, err := svc.PauseAccountScheduling(context.Background(), account.ID, 15*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1, repo.setTempUnschedCalls)
	require.WithinDuration(t, time.Now().Add(15*time.Minute), repo.setTempUnschedUntil, time.Second)
	require.Contains(t, repo.setTempUnschedReason, "Paused manually by administrator")
	require.NotNil(t, cache.states[account.ID])
	require.Equal(t, repo.setTempUnschedUntil.Unix(), cache.states[account.ID].UntilUnix)
	require.Equal(t, repo.setTempUnschedUntil, *updated.TempUnschedulableUntil)
}

func TestRateLimitService_ClearRateLimit_ClearTempUnschedulableFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearTempUnschedulableErr: errors.New("clear temp unsched failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 7)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearRateLimitFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearRateLimitErr: errors.New("clear rate limit failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 11)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 0, repo.clearAntigravityCalls)
	require.Equal(t, 0, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearAntigravityFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearAntigravityErr: errors.New("clear antigravity failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 12)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 0, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_ClearModelRateLimitsFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		clearModelRateLimitErr: errors.New("clear model rate limits failed"),
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 13)
	require.Error(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_CacheDeleteFailedShouldNotFail(t *testing.T) {
	repo := &rateLimitClearRepoStub{}
	cache := &tempUnschedCacheRecorder{
		deleteErr: errors.New("cache delete failed"),
	}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	err := svc.ClearRateLimit(context.Background(), 14)
	require.NoError(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Equal(t, []int64{14}, cache.deletedIDs)
}

func TestRateLimitService_ClearRateLimit_WithoutTempUnschedCache(t *testing.T) {
	repo := &rateLimitClearRepoStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

	err := svc.ClearRateLimit(context.Background(), 15)
	require.NoError(t, err)

	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
}

func TestRateLimitService_RecoverAccountAfterSuccessfulTest_ClearsErrorAndRateLimitRelatedState(t *testing.T) {
	now := time.Now()
	repo := &rateLimitClearRepoStub{
		getByIDAccount: &Account{
			ID:                     42,
			Status:                 StatusError,
			RateLimitedAt:          &now,
			TempUnschedulableUntil: &now,
			Extra: map[string]any{
				"model_rate_limits": map[string]any{
					"claude-sonnet-4-5": map[string]any{
						"rate_limit_reset_at": now.Format(time.RFC3339),
					},
				},
				"antigravity_quota_scopes": map[string]any{"gemini": true},
			},
		},
	}
	cache := &tempUnschedCacheRecorder{}
	blocker := &runtimeBlockRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)
	svc.SetAccountRuntimeBlocker(blocker)

	result, err := svc.RecoverAccountAfterSuccessfulTest(context.Background(), 42)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClearedError)
	require.True(t, result.ClearedRateLimit)

	require.Equal(t, 1, repo.getByIDCalls)
	require.Equal(t, 1, repo.clearErrorCalls)
	require.Equal(t, 1, repo.clearRateLimitCalls)
	require.Equal(t, 1, repo.clearAntigravityCalls)
	require.Equal(t, 1, repo.clearModelRateLimitCalls)
	require.Equal(t, 1, repo.clearTempUnschedCalls)
	require.Equal(t, []int64{42}, cache.deletedIDs)
	require.Equal(t, []int64{42}, blocker.clearedIDs)
}

func TestRateLimitService_RecoverAccountAfterSuccessfulTest_NoRecoverableStateIsNoop(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		getByIDAccount: &Account{
			ID:          7,
			Status:      StatusActive,
			Schedulable: true,
			Extra:       map[string]any{},
		},
	}
	cache := &tempUnschedCacheRecorder{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, cache)

	result, err := svc.RecoverAccountAfterSuccessfulTest(context.Background(), 7)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.ClearedError)
	require.False(t, result.ClearedRateLimit)

	require.Equal(t, 1, repo.getByIDCalls)
	require.Equal(t, 0, repo.clearErrorCalls)
	require.Equal(t, 0, repo.clearRateLimitCalls)
	require.Equal(t, 0, repo.clearAntigravityCalls)
	require.Equal(t, 0, repo.clearModelRateLimitCalls)
	require.Equal(t, 0, repo.clearTempUnschedCalls)
	require.Empty(t, cache.deletedIDs)
}

func TestRateLimitService_RecoverAccountAfterSuccessfulTest_ClearErrorFailed(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		getByIDAccount: &Account{
			ID:     9,
			Status: StatusError,
		},
		clearErrorErr: errors.New("clear error failed"),
	}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)

	result, err := svc.RecoverAccountAfterSuccessfulTest(context.Background(), 9)
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 1, repo.getByIDCalls)
	require.Equal(t, 1, repo.clearErrorCalls)
	require.Equal(t, 0, repo.clearRateLimitCalls)
}

func TestRateLimitService_RecoverAccountState_InvalidatesOAuthTokenOnErrorRecovery(t *testing.T) {
	repo := &rateLimitClearRepoStub{
		getByIDAccount: &Account{
			ID:     21,
			Type:   AccountTypeOAuth,
			Status: StatusError,
		},
	}
	invalidator := &recoverTokenInvalidatorStub{}
	svc := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	svc.SetTokenCacheInvalidator(invalidator)

	result, err := svc.RecoverAccountState(context.Background(), 21, AccountRecoveryOptions{
		InvalidateToken: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClearedError)
	require.False(t, result.ClearedRateLimit)
	require.Equal(t, 1, repo.clearErrorCalls)
	require.Len(t, invalidator.accounts, 1)
	require.Equal(t, int64(21), invalidator.accounts[0].ID)
}
