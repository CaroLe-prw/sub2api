package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetRequestCredentialOpenAIFailureRequestsNextAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	expiredAt := time.Now().Add(-time.Minute).Format(time.RFC3339)
	account := &Account{
		ID:       319,
		Name:     "sticky-openai",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "expired-token",
			"expires_at":   expiredAt,
		},
	}
	svc := &OpenAIGatewayService{
		openAITokenProvider: NewOpenAITokenProvider(nil, nil, nil),
	}

	token, kind, err := svc.getRequestCredential(context.Background(), c, account)
	require.Empty(t, token)
	require.Empty(t, kind)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, GatewayFailureStageAccountAuth, failoverErr.Stage)
	require.Equal(t, GatewayFailureScopeAccount, failoverErr.Scope)
	require.Equal(t, OpenAICredentialReasonUnavailable, failoverErr.Reason)
	require.True(t, failoverErr.ShouldRetryNextAccount())
	require.Equal(t, http.StatusServiceUnavailable, failoverErr.ClientStatusCode)
	require.Equal(t, OpenAICredentialUnavailableClientMessage, failoverErr.ClientMessage)

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "credential_failover", events[0].Kind)
	require.Equal(t, int64(319), events[0].AccountID)
}

func TestGetRequestCredentialOpenAICanceledRequestDoesNotFailover(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	account := &Account{ID: 319, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	svc := &OpenAIGatewayService{}

	_, _, err := svc.getRequestCredential(ctx, nil, account)
	require.ErrorIs(t, err, context.Canceled)

	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
}
