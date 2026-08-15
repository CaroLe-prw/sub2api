package handler

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAISchedulerCacheEligibleTokensDoesNotDoubleCountCache(t *testing.T) {
	usage := service.OpenAIUsage{
		InputTokens:              100,
		CacheCreationInputTokens: 5,
		CacheReadInputTokens:     90,
	}

	require.Equal(t, int64(100), openAISchedulerCacheEligibleTokens(usage))
}

func TestOpenAIClientDisconnectOutcomeMarksStartedResponseAsSuccess(t *testing.T) {
	firstTokenMs := 120
	outcome := openAIClientDisconnectOutcome(&service.OpenAIForwardResult{
		FirstTokenMs: &firstTokenMs,
		Usage:        service.OpenAIUsage{InputTokens: 10, OutputTokens: 2},
	}, 500)

	require.True(t, outcome.Success)
	require.True(t, outcome.Canceled)
	require.Equal(t, "client_disconnected", outcome.Reason)
	require.Equal(t, &firstTokenMs, outcome.FirstTokenMs)
}

func TestOpenAIClientDisconnectOutcomeKeepsUnstartedResponseCanceled(t *testing.T) {
	outcome := openAIClientDisconnectOutcome(&service.OpenAIForwardResult{
		Usage: service.OpenAIUsage{InputTokens: 10},
	}, 500)

	require.False(t, outcome.Success)
	require.True(t, outcome.Canceled)
}

func TestShouldRecordOpenAISchedulerWebSocketTurnOutcome(t *testing.T) {
	require.False(t, shouldRecordOpenAISchedulerWebSocketTurnOutcome(1, errors.New("first turn failed")), "outer failover records the real HTTP status")
	require.True(t, shouldRecordOpenAISchedulerWebSocketTurnOutcome(1, nil))
	require.True(t, shouldRecordOpenAISchedulerWebSocketTurnOutcome(2, errors.New("later turn failed")))
}

func TestOpenAIForwardClientCanceled(t *testing.T) {
	require.True(t, openAIForwardClientCanceled(nil, &service.OpenAIForwardResult{ClientDisconnect: true}, errors.New("drain failed")))
	require.True(t, openAIForwardClientCanceled(nil, nil, fmt.Errorf("wrapped: %w", context.Canceled)))
	require.False(t, openAIForwardClientCanceled(nil, nil, errors.New("upstream failed")))
}
