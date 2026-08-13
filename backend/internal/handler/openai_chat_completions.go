package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

// ChatCompletions handles OpenAI Chat Completions API requests.
// POST /v1/chat/completions
func (h *OpenAIGatewayHandler) ChatCompletions(c *gin.Context) {
	streamStarted := false
	defer h.recoverResponsesPanic(c, &streamStarted)

	requestStart := time.Now()

	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}

	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(
		c,
		"handler.openai_gateway.chat_completions",
		zap.Int64("user_id", subject.UserID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.Any("group_id", apiKey.GroupID),
	)

	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}

	body, err := readLenientJSONRequestBodyWithPrealloc(c.Request, h.cfg)
	if err != nil {
		logRequestBodyReadFailure(reqLog, c.Request, err)
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	if len(body) == 0 {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Request body is empty")
		return
	}

	if !gjson.ValidBytes(body) {
		logRequestBodyParseFailure(reqLog, body, nil)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return
	}

	modelResult := gjson.GetBytes(body, "model")
	if !modelResult.Exists() || modelResult.Type != gjson.String || modelResult.String() == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	reqModel := modelResult.String()
	ensureCompositeTargetPlatform(c, apiKey, reqModel)
	if !openAICompatibleTextTargetAllowed(c, apiKey, reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Model is not supported by this OpenAI-compatible endpoint for composite groups")
		return
	}
	if cappedBody, changed := applyOpenAIReasoningEffortPolicyForRequest(c, apiKey, body); changed {
		body = cappedBody
	}
	reqStream, ok := parseOpenAICompatibleStream(body)
	if !ok {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", invalidStreamFieldTypeMessage)
		return
	}
	if service.IsGPTImageGenerationModel(reqModel) {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "This model is not supported on the Chat Completions endpoint")
		return
	}

	reqLog = reqLog.With(zap.String("model", reqModel), zap.Bool("stream", reqStream))

	setOpsRequestContext(c, reqModel, reqStream)
	setOpsEndpointContext(c, "", int16(service.RequestTypeFromLegacy(reqStream, false)))

	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIChat, reqModel, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	if h.rejectIfCyberSessionBlocked(c, apiKey, body, reqModel, cyberBlockFormatChat) {
		return
	}

	// 解析渠道级模型映射
	channelMapping, _ := h.gatewayService.ResolveChannelMappingAndRestrict(c.Request.Context(), apiKey.GroupID, reqModel)

	if h.errorPassthroughService != nil {
		service.BindErrorPassthroughService(c, h.errorPassthroughService)
	}

	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	requestPlatform := openAICompatibleRequestPlatform(c.Request.Context(), apiKey)

	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(requestStart).Milliseconds())
	routingStart := time.Now()

	userReleaseFunc, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, reqStream, &streamStarted, reqLog)
	if !acquired {
		return
	}
	if userReleaseFunc != nil {
		defer userReleaseFunc()
	}

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		reqLog.Info("openai_chat_completions.billing_eligibility_check_failed", zap.Error(err))
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.handleStreamingAwareError(c, status, code, message, streamStarted)
		return
	}

	sessionHash := h.gatewayService.GenerateSessionHash(c, body)
	promptCacheKey := h.gatewayService.ExtractSessionID(c, body)

	maxAccountSwitches := h.maxAccountSwitches
	failoverBudget := newOpenAIFailoverBudget(c.Request.Context(), h.cfg, body, routingStart, maxAccountSwitches)
	switchCount := 0
	profitVetoCount := 0
	failedAccountIDs := make(map[int64]struct{})
	sameAccountRetryCount := make(map[int64]int)
	var pinnedRetryAccountID int64
	var lastFailoverErr *service.UpstreamFailoverError
	var lastFailoverAccountID int64
	var oauth429FailoverState service.OpenAIOAuth429FailoverState
	var observabilityAccountID int64
	var observabilityAccountName string
	observabilityOutcomeRecorded := true
	recordObservabilityOutcome := func(ctx context.Context, outcome service.OpenAISchedulerObservabilityOutcome) {
		h.gatewayService.RecordOpenAISchedulerObservabilityOutcome(ctx, outcome)
		if outcome.AccountID == observabilityAccountID {
			observabilityOutcomeRecorded = true
		}
	}
	// Keep every selected Chat Completions trace terminal even when a local
	// admission/concurrency branch returns before forwarding. Explicit upstream
	// outcomes below take precedence; this defer is only the last-resort guard.
	defer func() {
		if observabilityAccountID == 0 || observabilityOutcomeRecorded {
			return
		}
		outcome := service.OpenAISchedulerObservabilityOutcome{
			AccountID: observabilityAccountID, AccountName: observabilityAccountName,
			Reason: "request_terminated", DurationMs: time.Since(routingStart).Milliseconds(),
		}
		if failoverClientGone(c) {
			outcome.Canceled = true
			outcome.Reason = "client_disconnected"
		} else if status := c.Writer.Status(); status >= http.StatusBadRequest {
			outcome.UpstreamStatus = status
		}
		h.gatewayService.RecordOpenAISchedulerObservabilityOutcome(c.Request.Context(), outcome)
	}()

	// 分组利润控制：chat completions 文本入口请求级装门并固定 pricingAt。
	ccPricingCtx, pricingAt := h.gatewayService.WithOpenAIRequestPricingContext(c.Request.Context(), apiKey.GroupID)
	c.Request = c.Request.WithContext(ccPricingCtx)

	for {
		if failoverClientGone(c) {
			return
		}
		reqLog.Debug("openai_chat_completions.account_selecting", zap.Int("excluded_account_count", len(failedAccountIDs)))
		selectAccount := h.gatewayService.SelectAccountWithSchedulerForCapability
		if pinnedRetryAccountID > 0 {
			pinnedID := pinnedRetryAccountID
			pinnedRetryAccountID = 0
			selectAccount = func(ctx context.Context, groupID *int64, previousResponseID, sessionHash, requestedModel string, excludedIDs map[int64]struct{}, requiredTransport service.OpenAIUpstreamTransport, requiredCapability service.OpenAIEndpointCapability, requireCompact, previousResponseCanMove, useUpstreamTokenCost bool, platformOverride ...string) (*service.AccountSelectionResult, service.OpenAIAccountScheduleDecision, error) {
				return h.gatewayService.SelectPinnedAccountWithSchedulerForCapability(ctx, pinnedID, groupID, previousResponseID, sessionHash, requestedModel, excludedIDs, requiredTransport, requiredCapability, requireCompact, previousResponseCanMove, useUpstreamTokenCost, platformOverride...)
			}
		}
		selection, scheduleDecision, err := selectAccount(
			c.Request.Context(),
			apiKey.GroupID,
			"",
			sessionHash,
			reqModel,
			failedAccountIDs,
			service.OpenAIUpstreamTransportAny,
			service.OpenAIEndpointCapabilityChatCompletions,
			false,
			false,
			true,
			requestPlatform,
		)
		if err != nil {
			if failoverClientGone(c) {
				reqLog.Info("openai_chat_completions.account_select_aborted_client_disconnected", zap.Error(err))
				return
			}
			reqLog.Warn("openai_chat_completions.account_select_failed",
				zap.Error(openAICompatibleSelectionErrorForLog(err, requestPlatform)),
				zap.Int("excluded_account_count", len(failedAccountIDs)),
			)
			if len(failedAccountIDs) == 0 {
				cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
				if !cls.ModelNotFound {
					markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
				}
				h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
				return
			}
			switch action := handleOpenAIOAuthCapacitySelectionExhausted(
				c.Request.Context(), failedAccountIDs, lastFailoverErr, lastFailoverAccountID, sameAccountRetryCount, reqLog,
			); action {
			case FailoverContinue:
				budgetDecision := failoverBudget.evaluateSameAccountRetry(c.Request.Context(), switchCount, -1)
				h.recordOpenAIFailoverBudgetDecision(c.Request.Context(), budgetDecision)
				if !budgetDecision.AllowNext {
					h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
					return
				}
				pinnedRetryAccountID = lastFailoverAccountID
				continue
			case FailoverCanceled:
				return
			}
			if lastFailoverErr != nil {
				h.handleFailoverExhausted(c, lastFailoverErr, streamStarted)
			} else {
				h.handleStreamingAwareError(c, http.StatusBadGateway, "api_error", "Upstream request failed", streamStarted)
			}
			return
		}
		if selection == nil || selection.Account == nil {
			cls := classifyOpenAICompatibleNoAccountErrorFromGin(c, h.gatewayService, apiKey, reqModel, reqModel)
			if !cls.ModelNotFound {
				markOpsRoutingCapacityLimited(c)
			}
			h.handleStreamingAwareError(c, cls.Status, cls.ErrType, cls.Message, streamStarted)
			return
		}
		account := selection.Account
		observabilityAccountID = account.ID
		observabilityAccountName = account.Name
		observabilityOutcomeRecorded = false
		sessionHash = ensureOpenAIPoolModeSessionHash(sessionHash, account)
		reqLog.Debug("openai_chat_completions.account_selected", zap.Int64("account_id", account.ID), zap.String("account_name", account.Name))
		setOpsSelectedAccount(c, account.ID, account.Platform)

		accountReleaseFunc, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, sessionHash, selection, reqStream, &streamStarted, reqLog)
		if slotResult == openAISlotAcquireProfitVetoed {
			// 利润终检否决：排除该账号重新选号；否决次数达上限则按无可用账号终止。
			if !recordOpenAIProfitVeto(failedAccountIDs, account.ID, &profitVetoCount) {
				h.handleOpenAIProfitVetoExhausted(c, streamStarted, reqLog, profitVetoCount)
				return
			}
			continue
		}
		if slotResult != openAISlotAcquireOK {
			return
		}

		service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
		forwardStart := time.Now()

		forwardBody := body
		if channelMapping.Mapped {
			forwardBody = h.gatewayService.ReplaceModelInBody(body, channelMapping.MappedModel)
		}
		writerSizeBeforeForward := c.Writer.Size()
		result, err := func() (*service.OpenAIForwardResult, error) {
			defer func() {
				if accountReleaseFunc != nil {
					accountReleaseFunc()
				}
			}()
			return h.gatewayService.ForwardAsChatCompletions(c.Request.Context(), c, account, forwardBody, promptCacheKey, "")
		}()
		cyberBlockKeyChat := ""
		if service.GetOpsCyberPolicy(c) != nil {
			cyberBlockKeyChat = service.CyberSessionBlockKey(apiKey.ID, c, body)
		}
		h.recordCyberPolicyIfMarked(c, apiKey, account, subscription, reqModel, err != nil, cyberBlockKeyChat, clientRequestedUsageFields(c, channelMapping, reqModel, ""), service.HashUsageRequestPayload(body))

		forwardDurationMs := time.Since(forwardStart).Milliseconds()
		upstreamLatencyMs, _ := getContextInt64(c, service.OpsUpstreamLatencyMsKey)
		responseLatencyMs := forwardDurationMs
		if upstreamLatencyMs > 0 && forwardDurationMs > upstreamLatencyMs {
			responseLatencyMs = forwardDurationMs - upstreamLatencyMs
		}
		service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, responseLatencyMs)
		if err == nil && result != nil && result.FirstTokenMs != nil {
			service.SetOpsLatencyMs(c, service.OpsTimeToFirstTokenMsKey, int64(*result.FirstTokenMs))
		}
		if err != nil {
			if result != nil && result.ImageCount > 0 {
				reqLog.Warn("openai_chat_completions.forward_partial_error_with_image_result",
					zap.Int64("account_id", account.ID),
					zap.Int("image_count", result.ImageCount),
					zap.Error(err),
				)
			} else {
				var failoverErr *service.UpstreamFailoverError
				if errors.As(err, &failoverErr) {
					if failoverClientGone(c) {
						recordObservabilityOutcome(c.Request.Context(), service.OpenAISchedulerObservabilityOutcome{
							AccountID: account.ID, AccountName: account.Name, Canceled: true, Reason: "client_disconnected",
							DurationMs: time.Since(requestStart).Milliseconds(),
						})
						reqLog.Info("openai_chat_completions.failover_aborted_client_disconnected",
							zap.Int64("account_id", account.ID),
							zap.Int("upstream_status", failoverErr.StatusCode),
						)
						return
					}
					observabilityReason := string(failoverErr.Reason)
					if observabilityReason == "" {
						if failoverErr.StatusCode == http.StatusTooManyRequests {
							observabilityReason = "rate_limit"
						} else {
							observabilityReason = "upstream_error"
						}
					}
					recordObservabilityOutcome(c.Request.Context(), service.OpenAISchedulerObservabilityOutcome{
						AccountID: account.ID, AccountName: account.Name, UpstreamStatus: failoverErr.StatusCode,
						Reason: observabilityReason, DurationMs: time.Since(routingStart).Milliseconds(),
					})
					if c.Writer.Size() != writerSizeBeforeForward {
						h.handleFailoverExhausted(c, failoverErr, true)
						return
					}
					if failoverErr.ShouldReportAccountScheduleFailure() {
						h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), false, nil)
					}
					if !failoverErr.ShouldRetryNextAccount() {
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					// Pool mode: retry on the same account
					if failoverErr.RetryableOnSameAccount {
						retryLimit := account.GetPoolModeRetryCount()
						if sameAccountRetryCount[account.ID] < retryLimit {
							budgetDecision := failoverBudget.evaluateSameAccountRetry(
								c.Request.Context(), switchCount,
								openAIFailoverRemainingCandidates(scheduleDecision.CandidateCount),
							)
							h.recordOpenAIFailoverBudgetDecision(c.Request.Context(), budgetDecision)
							if !budgetDecision.AllowNext {
								h.handleFailoverExhausted(c, failoverErr, streamStarted)
								return
							}
							sameAccountRetryCount[account.ID]++
							pinnedRetryAccountID = account.ID
							retryDelay := sameAccountRetryDelayFor(failoverErr, sameAccountRetryCount[account.ID])
							reqLog.Warn("openai_chat_completions.pool_mode_same_account_retry",
								zap.Int64("account_id", account.ID),
								zap.Int("upstream_status", failoverErr.StatusCode),
								zap.Int("retry_limit", retryLimit),
								zap.Int("retry_count", sameAccountRetryCount[account.ID]),
								zap.Duration("retry_delay", retryDelay),
							)
							select {
							case <-c.Request.Context().Done():
								return
							case <-time.After(retryDelay):
							}
							continue
						}
					}
					failedAccountIDs[account.ID] = struct{}{}
					lastFailoverErr = failoverErr
					lastFailoverAccountID = account.ID
					budgetDecision := failoverBudget.evaluateNextAccount(
						c.Request.Context(), failoverErr, switchCount,
						openAIFailoverRemainingCandidates(scheduleDecision.CandidateCount),
					)
					if !budgetDecision.AllowNext {
						h.recordOpenAIFailoverBudgetDecision(c.Request.Context(), budgetDecision)
						reqLog.Warn("openai_chat_completions.failover_budget_exhausted",
							zap.String("reason", budgetDecision.Reason),
							zap.Int64("elapsed_ms", budgetDecision.ElapsedMs),
							zap.Int64("budget_ms", budgetDecision.BudgetMs),
							zap.Int("switch_count", budgetDecision.SwitchCount),
							zap.Int("max_switches", budgetDecision.SwitchLimit),
						)
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					switchCount = budgetDecision.SwitchCount
					if h.gatewayService.ShouldStopOpenAIOAuth429Failover(account, failoverErr.StatusCode, switchCount, &oauth429FailoverState) {
						budgetDecision.AllowNext = false
						budgetDecision.Reason = "oauth_429_failover_limit"
						h.recordOpenAIFailoverBudgetDecision(c.Request.Context(), budgetDecision)
						h.handleFailoverExhausted(c, failoverErr, streamStarted)
						return
					}
					h.gatewayService.RecordOpenAIAccountSwitch()
					h.recordOpenAIFailoverBudgetDecision(c.Request.Context(), budgetDecision)
					reqLog.Warn("openai_chat_completions.upstream_failover_switching",
						zap.Int64("account_id", account.ID),
						zap.Int("upstream_status", failoverErr.StatusCode),
						zap.Int("switch_count", switchCount),
						zap.Int("max_switches", maxAccountSwitches),
					)
					continue
				}
				if openAIForwardClientCanceled(c, result, err) {
					recordObservabilityOutcome(c.Request.Context(), service.OpenAISchedulerObservabilityOutcome{
						AccountID: account.ID, AccountName: account.Name, Canceled: true, Reason: "client_disconnected",
						DurationMs: time.Since(requestStart).Milliseconds(),
					})
					reqLog.Info("openai_chat_completions.client_disconnected", zap.Int64("account_id", account.ID), zap.Error(err))
					return
				}
				h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), false, nil)
				recordObservabilityOutcome(c.Request.Context(), service.OpenAISchedulerObservabilityOutcome{
					AccountID: account.ID, AccountName: account.Name, Reason: "upstream_error",
					DurationMs: time.Since(routingStart).Milliseconds(),
				})
				upstreamErrorAlreadyCommunicated := openAIForwardErrorAlreadyCommunicated(c, writerSizeBeforeForward, err)
				wroteFallback := false
				if !upstreamErrorAlreadyCommunicated {
					wroteFallback = h.ensureOpenAIStreamReadErrorResponse(c, err, streamStarted)
					if !wroteFallback {
						wroteFallback = h.ensureForwardErrorResponse(c, streamStarted)
					}
				}
				reqLog.Warn("openai_chat_completions.forward_failed",
					zap.Int64("account_id", account.ID),
					zap.Bool("fallback_error_response_written", wroteFallback),
					zap.Bool("upstream_error_response_already_written", upstreamErrorAlreadyCommunicated),
					zap.Error(err),
				)
				return
			}
		}
		if result != nil {
			observabilityReason := openAIClientDisconnectReason(result)
			if observabilityReason == "" && h.gatewayService.RecordOpenAIFirstOutputSlow(account, result) {
				observabilityReason = "slow_first_output"
			}
			succeeded := openAIForwardSucceededForScheduling(result)
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), succeeded, result.FirstTokenMs)
			recordObservabilityOutcome(c.Request.Context(), service.OpenAISchedulerObservabilityOutcome{
				AccountID: account.ID, AccountName: account.Name, Success: succeeded, Canceled: result.ClientDisconnect,
				Reason: observabilityReason, FirstTokenMs: result.FirstTokenMs,
				DurationMs: time.Since(routingStart).Milliseconds(), CacheReadTokens: int64(result.Usage.CacheReadInputTokens),
				CacheEligibleTokens: openAISchedulerCacheEligibleTokens(result.Usage),
			})
		} else {
			h.gatewayService.ReportOpenAIAccountScheduleResult(account.ID, account.GetMappedModel(reqModel), true, nil)
			recordObservabilityOutcome(c.Request.Context(), service.OpenAISchedulerObservabilityOutcome{
				AccountID: account.ID, AccountName: account.Name, Success: true,
				DurationMs: time.Since(routingStart).Milliseconds(),
			})
		}

		userAgent := c.GetHeader("User-Agent")
		clientIP := ip.GetClientIP(c)
		inboundEndpoint := GetInboundEndpoint(c)
		upstreamEndpoint := resolveOpenAIUpstreamEndpoint(c, account, result)
		quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
		sessionID := service.ExtractClientSessionID(c)

		cyberBlocked := service.GetOpsCyberPolicy(c) != nil
		h.submitOpenAIUsageRecordTask(c.Request.Context(), result, func(ctx context.Context) {
			if err := h.gatewayService.RecordUsage(ctx, &service.OpenAIRecordUsageInput{
				Result:             result,
				APIKey:             apiKey,
				User:               apiKey.User,
				Account:            account,
				Subscription:       subscription,
				InboundEndpoint:    inboundEndpoint,
				UpstreamEndpoint:   upstreamEndpoint,
				UserAgent:          userAgent,
				IPAddress:          clientIP,
				APIKeyService:      h.apiKeyService,
				QuotaPlatform:      quotaPlatform,
				SessionID:          sessionID,
				ChannelUsageFields: clientRequestedUsageFields(c, channelMapping, reqModel, result.UpstreamModel),
				PricingAt:          pricingAt,
				CyberBlocked:       cyberBlocked,
			}); err != nil {
				logger.L().With(
					zap.String("component", "handler.openai_gateway.chat_completions"),
					zap.Int64("user_id", subject.UserID),
					zap.Int64("api_key_id", apiKey.ID),
					zap.Any("group_id", apiKey.GroupID),
					zap.String("model", reqModel),
					zap.Int64("account_id", account.ID),
				).Error("openai_chat_completions.record_usage_failed", zap.Error(err))
			}
		})
		reqLog.Debug("openai_chat_completions.request_completed",
			zap.Int64("account_id", account.ID),
			zap.Int("switch_count", switchCount),
		)
		return
	}
}

// resolveOpenAIUpstreamEndpoint returns the actual upstream endpoint for an
// OpenAI-compatible account. A forwarding result is authoritative because a
// single inbound route may choose raw Chat or a Responses bridge at runtime.
// The account-based derivation remains as a fallback for existing callers and
// forwarding paths that do not report their endpoint yet.
func resolveOpenAIUpstreamEndpoint(c *gin.Context, account *service.Account, result *service.OpenAIForwardResult) string {
	if result != nil {
		if endpoint := strings.TrimSpace(result.UpstreamEndpoint); endpoint != "" {
			return endpoint
		}
	}
	if endpoint := service.GetActualOpenAIUpstreamEndpoint(c); endpoint != "" {
		return endpoint
	}
	if account != nil && account.Type == service.AccountTypeAPIKey &&
		!openai_compat.ShouldUseResponsesAPI(account.Extra) {
		return EndpointChatCompletions
	}
	return GetUpstreamEndpoint(c, account.Platform)
}
