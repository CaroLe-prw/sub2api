package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"go.uber.org/zap"
)

// handleOpenAIOAuthCapacitySelectionExhausted puts the last OAuth account back
// into the candidate set only after account selection has no other candidate.
// This keeps normal OAuth failover intact while allowing a single-account group
// to recover from the upstream model-capacity response.
func handleOpenAIOAuthCapacitySelectionExhausted(
	ctx context.Context,
	failedAccountIDs map[int64]struct{},
	lastFailoverErr *service.UpstreamFailoverError,
	lastFailoverAccountID int64,
	sameAccountRetryCount map[int64]int,
	reqLog *zap.Logger,
) FailoverAction {
	if ctx == nil || ctx.Err() != nil {
		return FailoverCanceled
	}
	if lastFailoverErr == nil || !lastFailoverErr.RetryableOnSameAccountIfNoOtherAccount {
		return FailoverExhausted
	}
	if sameAccountRetryCount[lastFailoverAccountID] >= maxSameAccountRetries {
		return FailoverExhausted
	}

	sameAccountRetryCount[lastFailoverAccountID]++
	delete(failedAccountIDs, lastFailoverAccountID)
	if reqLog != nil {
		reqLog.Warn("openai.oauth_capacity_same_account_retry",
			zap.Int64("account_id", lastFailoverAccountID),
			zap.Int("upstream_status", lastFailoverErr.StatusCode),
			zap.Int("retry_count", sameAccountRetryCount[lastFailoverAccountID]),
			zap.Int("retry_max", maxSameAccountRetries),
		)
	}
	if !sleepWithContext(ctx, sameAccountRetryDelay) {
		return FailoverCanceled
	}
	return FailoverContinue
}
