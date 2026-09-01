package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	OpenAIContextCompactionModeAuto     = "auto"
	OpenAIContextCompactionModeForceOn  = "force_on"
	OpenAIContextCompactionModeForceOff = "force_off"

	openAIContextCompactionModeExtraKey       = "openai_context_compaction_mode"
	openAIContextCompactionSupportedExtraKey  = "openai_context_compaction_supported"
	openAIContextCompactionThresholdExtraKey  = "openai_context_compaction_threshold"
	openAIContextCompactionCheckedAtExtraKey  = "openai_context_compaction_checked_at"
	openAIContextCompactionLastStatusExtraKey = "openai_context_compaction_last_status"
	openAIContextCompactionLastErrorExtraKey  = "openai_context_compaction_last_error"

	// The official compaction guide uses 200k as its reference threshold. It is
	// deliberately conservative for long Codex sessions and can be overridden
	// per account through accounts.extra.
	defaultOpenAIContextCompactionThreshold       int64 = 200_000
	openAIAutoContextCompactionInjectedKey              = "openai_auto_context_compaction_injected"
	openAIAutoContextCompactionFailoverAllowedKey       = "openai_auto_context_compaction_failover_allowed"
	openAIContextCompactionObservationTimeout           = 2 * time.Second
)

func normalizeOpenAIContextCompactionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case OpenAIContextCompactionModeForceOn:
		return OpenAIContextCompactionModeForceOn
	case OpenAIContextCompactionModeForceOff:
		return OpenAIContextCompactionModeForceOff
	default:
		return OpenAIContextCompactionModeAuto
	}
}

func (a *Account) GetOpenAIContextCompactionMode() string {
	if a == nil || !a.IsOpenAI() || a.Extra == nil {
		return OpenAIContextCompactionModeAuto
	}
	mode, _ := a.Extra[openAIContextCompactionModeExtraKey].(string)
	return normalizeOpenAIContextCompactionMode(mode)
}

func (a *Account) OpenAIContextCompactionSupportKnown() (supported bool, known bool) {
	if a == nil || !a.IsOpenAI() || a.Type != AccountTypeAPIKey {
		return false, false
	}
	switch a.GetOpenAIContextCompactionMode() {
	case OpenAIContextCompactionModeForceOn:
		return true, true
	case OpenAIContextCompactionModeForceOff:
		return false, true
	}
	if a.Extra == nil {
		return false, false
	}
	supported, ok := a.Extra[openAIContextCompactionSupportedExtraKey].(bool)
	return supported, ok
}

func (a *Account) GetOpenAIContextCompactionThreshold() int64 {
	if a == nil || a.Extra == nil {
		return defaultOpenAIContextCompactionThreshold
	}
	raw, exists := a.Extra[openAIContextCompactionThresholdExtraKey]
	if !exists {
		return defaultOpenAIContextCompactionThreshold
	}
	var threshold int64
	switch value := raw.(type) {
	case int:
		threshold = int64(value)
	case int64:
		threshold = value
	case float64:
		threshold = int64(value)
	case string:
		threshold, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	}
	if threshold <= 0 {
		return defaultOpenAIContextCompactionThreshold
	}
	return threshold
}

// applyOpenAIAutoContextCompactionToBody injects server-side compaction into
// Responses-shaped API-key pool traffic, including Chat Completions requests
// after they have been converted to Responses. Client-provided
// context_management is authoritative and standalone /responses/compact has
// separate semantics.
func applyOpenAIAutoContextCompactionToBody(c *gin.Context, account *Account, body []byte) ([]byte, bool, error) {
	setOpenAIAutoContextCompactionState(c, false, false)
	responsesLite := isOpenAIResponsesLiteRequestedForPayload(c, account, body, "")
	if len(body) == 0 || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey || !account.IsPoolMode() || isOpenAIResponsesCompactPath(c) || responsesLite {
		return body, false, nil
	}
	// A pool may contain providers with different effective context windows.
	// Stateless requests can safely try another account on a pre-output context
	// error even when this account has already rejected context_management.
	failoverAllowed := !gjson.GetBytes(body, "previous_response_id").Exists()
	setOpenAIAutoContextCompactionState(c, false, failoverAllowed)
	if gjson.GetBytes(body, "context_management").Exists() {
		return body, false, nil
	}
	supported, known := account.OpenAIContextCompactionSupportKnown()
	// Do not probe an unknown provider with production traffic. Responses Lite
	// and many compatible gateways accept /responses but reject server-side
	// compaction, and their rejection shape is not standardized enough for a
	// reliable transparent retry.
	if !known || !supported {
		return body, false, nil
	}

	threshold := account.GetOpenAIContextCompactionThreshold()
	updated, err := sjson.SetBytes(body, "context_management", []map[string]any{{
		"type":              "compaction",
		"compact_threshold": threshold,
	}})
	if err != nil {
		return body, false, fmt.Errorf("inject OpenAI context_management compaction: %w", err)
	}
	setOpenAIAutoContextCompactionState(c, true, failoverAllowed)
	return updated, true, nil
}

func setOpenAIAutoContextCompactionState(c *gin.Context, injected, failoverAllowed bool) {
	if c == nil {
		return
	}
	c.Set(openAIAutoContextCompactionInjectedKey, injected)
	c.Set(openAIAutoContextCompactionFailoverAllowedKey, failoverAllowed)
}

func openAIAutoContextCompactionInjected(c *gin.Context) bool {
	if c == nil {
		return false
	}
	injected, _ := c.Get(openAIAutoContextCompactionInjectedKey)
	value, _ := injected.(bool)
	return value
}

func openAIAutoContextCompactionMayFailover(c *gin.Context) bool {
	if c == nil {
		return false
	}
	allowed, _ := c.Get(openAIAutoContextCompactionFailoverAllowedKey)
	value, _ := allowed.(bool)
	return value
}

func openAIContextCompactionRejectedRetryBody(statusCode int, body, responseBody []byte) ([]byte, bool, error) {
	retryBody, reason, changed, err := normalizeOpenAIResponsesRejectedFieldRetryBody(statusCode, body, responseBody)
	if err != nil || !changed || reason != "context_management parameter rejection" {
		return nil, false, err
	}
	return retryBody, true, nil
}

func (s *OpenAIGatewayService) recordOpenAIContextCompactionObservation(
	ctx context.Context,
	account *Account,
	supported bool,
	statusCode int,
	errorMessage string,
) {
	if s == nil || s.accountRepo == nil || account == nil || account.ID <= 0 {
		return
	}
	if current, known := account.OpenAIContextCompactionSupportKnown(); known && current == supported {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	updateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAIContextCompactionObservationTimeout)
	defer cancel()
	updates := map[string]any{
		openAIContextCompactionSupportedExtraKey:  supported,
		openAIContextCompactionCheckedAtExtraKey:  time.Now().Format(time.RFC3339),
		openAIContextCompactionLastStatusExtraKey: statusCode,
		openAIContextCompactionLastErrorExtraKey:  truncateString(sanitizeUpstreamErrorMessage(errorMessage), 2048),
	}
	if supported {
		updates[openAIContextCompactionLastErrorExtraKey] = ""
	}
	if err := s.accountRepo.UpdateExtra(updateCtx, account.ID, updates); err != nil {
		logger.LegacyPrintf("service.openai_gateway", "[OpenAI] persist context compaction capability failed: account=%d supported=%v status=%d err=%v", account.ID, supported, statusCode, err)
	}
}

func openAIContextCompactionHTTPShouldFailover(c *gin.Context, statusCode int, message string, body []byte) bool {
	return openAIAutoContextCompactionMayFailover(c) &&
		statusCode >= http.StatusBadRequest &&
		isOpenAIContextWindowError(message, body)
}
