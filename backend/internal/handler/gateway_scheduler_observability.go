package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// recordGatewaySchedulerSelection feeds generic gateway decisions into the
// same trace store used by OpenAI. This keeps CC, Gemini and Grok attempts in
// the existing unified scheduling timeline.
func (h *GatewayHandler) recordGatewaySchedulerSelection(
	ctx context.Context,
	groupID *int64,
	platform string,
	sessionHash string,
	model string,
	selection *service.AccountSelectionResult,
	selectionErr error,
) {
	if h == nil || h.openAIGatewayService == nil {
		return
	}
	decision := service.OpenAIAccountScheduleDecision{}
	if selection != nil && selection.SchedulerDecision != nil {
		decision = *selection.SchedulerDecision
	}
	req := service.OpenAIAccountScheduleRequest{
		GroupID:        groupID,
		Platform:       platform,
		SessionHash:    sessionHash,
		RequestedModel: model,
	}
	if decision.StickySessionHit && decision.SelectedAccountID > 0 {
		req.StickyAccountID = decision.SelectedAccountID
	}
	h.openAIGatewayService.RecordOpenAISchedulerObservabilitySelection(ctx, req, decision, selection, selectionErr)
}

func (h *GatewayHandler) recordGatewaySchedulerFailure(ctx context.Context, account *service.Account, err error) {
	if h == nil || h.openAIGatewayService == nil || account == nil || err == nil {
		return
	}
	outcome := service.OpenAISchedulerObservabilityOutcome{
		AccountID:   account.ID,
		AccountName: account.Name,
		Reason:      "upstream_error",
	}
	var failoverErr *service.UpstreamFailoverError
	if errors.As(err, &failoverErr) {
		outcome.UpstreamStatus = failoverErr.StatusCode
		if reason := strings.TrimSpace(string(failoverErr.Reason)); reason != "" {
			outcome.Reason = reason
		}
	}
	h.openAIGatewayService.RecordOpenAISchedulerObservabilityOutcome(ctx, outcome)
}

func (h *GatewayHandler) recordGatewaySchedulerSuccess(ctx context.Context, account *service.Account, result *service.ForwardResult) {
	if h == nil || h.openAIGatewayService == nil || account == nil {
		return
	}
	outcome := service.OpenAISchedulerObservabilityOutcome{
		AccountID:   account.ID,
		AccountName: account.Name,
		Success:     true,
	}
	if result != nil {
		outcome.FirstTokenMs = result.FirstTokenMs
	}
	h.openAIGatewayService.RecordOpenAISchedulerObservabilityOutcome(ctx, outcome)
}
