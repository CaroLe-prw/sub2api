package service

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type accountSchedulingModelContextKey struct{}
type compactSchedulingContextKey struct{}
type accountSchedulingModel struct {
	accountID int64
	model     string
}

func withCompactScheduling(ctx context.Context, compact bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, compactSchedulingContextKey{}, compact)
}

func compactScheduling(ctx context.Context, fallback bool) bool {
	if ctx != nil {
		if compact, ok := ctx.Value(compactSchedulingContextKey{}).(bool); ok {
			return compact
		}
	}
	return fallback
}

func accountSchedulingUpstreamModel(ctx context.Context, account *Account, requestedModel string) string {
	if account == nil {
		return strings.TrimSpace(requestedModel)
	}
	if account.IsBedrock() {
		if model, ok := ResolveBedrockModelID(account, requestedModel); ok {
			return model
		}
	}
	if account.Platform == PlatformAntigravity {
		return resolveFinalAntigravityModelKey(ctx, account, requestedModel)
	}
	return canonicalOpenAIAccountSchedulingModel(account, requestedModel)
}

func withAccountSchedulingModel(ctx context.Context, account *Account, model string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if account == nil {
		return ctx
	}
	return context.WithValue(ctx, accountSchedulingModelContextKey{}, accountSchedulingModel{account.ID, model})
}

// Prefer the model actually sent on this attempt. Mapping the public name a
// second time can associate a timeout with an unrelated model after a retry.
func accountFailureModel(c *gin.Context, account *Account, fallback string) string {
	if c != nil {
		if value, ok := c.Get(OpsUpstreamModelKey); ok {
			if model, ok := value.(string); ok && strings.TrimSpace(model) != "" {
				return strings.TrimSpace(model)
			}
		}
	}
	if account != nil && account.IsOpenAI() {
		return ResolveOpenAIAccountUpstreamModelForRequest(account, fallback, isOpenAIResponsesCompactPath(c))
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	return accountSchedulingUpstreamModel(ctx, account, fallback)
}

func (s *RateLimitService) persistModelCooldown(ctx context.Context, account *Account, model string, until time.Time, reason string) bool {
	model = strings.TrimSpace(model)
	if s == nil || s.accountRepo == nil || account == nil || model == "" {
		return false
	}
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, model, until, reason); err != nil {
		slog.Warn("model_cooldown_persist_failed", "account_id", account.ID, "model", model, "error", err)
		return false
	}
	setAccountModelRateLimitSnapshot(account, model, until, reason, time.Now())
	return true
}

// A successful probe can recover account credential errors, but model failures
// are only cleared for the model that was actually tested. Manual account
// recovery continues to use RecoverAccountState and can clear all model state.
func (s *RateLimitService) RecoverAccountModelAfterSuccessfulTest(ctx context.Context, accountID int64, model string) (*SuccessfulTestRecoveryResult, error) {
	result := &SuccessfulTestRecoveryResult{}
	model = strings.TrimSpace(model)
	if s == nil || s.accountRepo == nil || model == "" {
		return result, nil
	}
	repo, ok := s.accountRepo.(AccountModelStateRepository)
	if !ok {
		return result, nil
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return result, nil
	}
	if account.Status == StatusError {
		if err := s.accountRepo.ClearError(ctx, accountID); err != nil {
			return nil, err
		}
		result.ClearedError = true
	}
	if !account.isRateLimitActiveForKey(model) {
		return result, nil
	}
	if err := repo.ClearModelRateLimit(ctx, accountID, model); err != nil {
		return nil, err
	}
	result.ClearedRateLimit = true
	return result, nil
}
