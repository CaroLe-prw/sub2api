package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// Balance exposes the top-level fields expected by CC Switch's generic usage
// template. It uses the same key-scoped amounts as Usage without fetching
// historical usage, daily totals, or model statistics.
func (h *GatewayHandler) Balance(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	ctx := c.Request.Context()
	var usage gin.H
	if apiKey.Quota > 0 || apiKey.HasRateLimits() {
		usage = h.buildQuotaLimitedUsageResponse(ctx, apiKey, nil, nil, nil)
	} else {
		var err error
		usage, err = h.buildUnrestrictedUsageResponse(c, ctx, apiKey, subject, nil, nil, nil)
		if err != nil {
			h.errorResponse(c, http.StatusInternalServerError, "api_error", "Failed to get user info")
			return
		}
	}

	balance, known := usage["remaining"].(float64)
	if !known {
		// A rate-limited key without a total quota has no top-level remaining
		// amount in /v1/usage. Show the tightest configured window instead of
		// exposing the owner's unrelated wallet balance.
		if windows, ok := usage["rate_limits"].([]gin.H); ok {
			for _, window := range windows {
				if remaining, ok := window["remaining"].(float64); ok && (!known || remaining < balance) {
					balance, known = remaining, true
				}
			}
		}
	}
	if !known && apiKey.Quota <= 0 && !apiKey.HasRateLimits() && apiKey.Group != nil && apiKey.Group.IsSubscriptionType() {
		// Balance-route authentication rejects failed/unavailable subscription
		// lookups. Missing context here therefore means no active subscription.
		balance, known = 0, true
	}
	if !known {
		// Do not turn a failed rate-limit lookup into a fabricated zero balance.
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "Balance is temporarily unavailable")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"is_active": usage["isValid"] == true,
		"balance":   balance,
		"unit":      "USD",
	})
}
