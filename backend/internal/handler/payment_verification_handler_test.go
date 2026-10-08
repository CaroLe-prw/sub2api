package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type paymentVerificationHandlerStub struct {
	callbackCalls int
	secret        string
	callbackErr   error
	resolveCalls  int
	actor, action string
	id            int64
}

func (*paymentVerificationHandlerStub) GetConfig(context.Context) (*service.PaymentVerificationConfig, error) {
	return &service.PaymentVerificationConfig{}, nil
}
func (*paymentVerificationHandlerStub) UpdateConfig(_ context.Context, cfg *service.PaymentVerificationConfig) (*service.PaymentVerificationConfig, error) {
	return cfg, nil
}
func (*paymentVerificationHandlerStub) ConnectTelegram(context.Context) error { return nil }
func (*paymentVerificationHandlerStub) Scan(context.Context) (*service.PaymentVerificationScanResult, error) {
	return &service.PaymentVerificationScanResult{}, nil
}
func (*paymentVerificationHandlerStub) ListAlerts(context.Context, int) ([]service.PaymentVerificationRecord, error) {
	return []service.PaymentVerificationRecord{}, nil
}
func (s *paymentVerificationHandlerStub) HandleTelegramCallback(_ context.Context, secret string, _ []byte) error {
	s.callbackCalls++
	s.secret = secret
	return s.callbackErr
}
func (s *paymentVerificationHandlerStub) ResolveAlert(_ context.Context, id int64, action, actor string) (*service.PaymentVerificationRecord, error) {
	s.resolveCalls++
	s.id = id
	s.actor = actor
	s.action = action
	return &service.PaymentVerificationRecord{ID: id}, nil
}

func TestPaymentVerificationWebhookBoundsAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body    string
		serviceErr    error
		status, calls int
	}{
		{"oversized", strings.Repeat("x", 64*1024+1), nil, http.StatusRequestEntityTooLarge, 0},
		{"unauthorized", "{}", infraerrors.Forbidden("INVALID_TELEGRAM_CALLBACK", "invalid callback"), http.StatusForbidden, 1},
		{"accepted", "{}", nil, http.StatusOK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &paymentVerificationHandlerStub{callbackErr: tc.serviceErr}
			h := &PaymentVerificationHandler{service: stub}
			r := gin.New()
			r.POST("/callback", h.TelegramCallback)
			req := httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(tc.body))
			req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "test-secret")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.calls, stub.callbackCalls)
			if tc.calls > 0 {
				require.Equal(t, "test-secret", stub.secret)
			}
		})
	}
}

func TestPaymentVerificationResolveUsesAuthenticatedActorAndRejectsInvalidActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body    string
		authenticated bool
		status, calls int
	}{
		{"no actor", `{"action":"ban"}`, false, http.StatusUnauthorized, 0},
		{"unknown action", `{"action":"activate"}`, true, http.StatusBadRequest, 0},
		{"ignore", `{"action":"ignore","user_id":999,"actor":"attacker"}`, true, http.StatusOK, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &paymentVerificationHandlerStub{}
			h := &PaymentVerificationHandler{service: stub}
			r := gin.New()
			if tc.authenticated {
				r.Use(func(c *gin.Context) {
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
					c.Next()
				})
			}
			r.POST("/alerts/:id/resolve", h.Resolve)
			req := httptest.NewRequest(http.MethodPost, "/alerts/42/resolve", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.calls, stub.resolveCalls)
			if tc.calls > 0 {
				require.Equal(t, int64(42), stub.id)
				require.Equal(t, "admin:7", stub.actor)
				require.Equal(t, "ignore", stub.action)
			}
		})
	}
}
