//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestPaymentVerificationOutcomeRequiresConclusiveEvidence(t *testing.T) {
	order := &dbent.PaymentOrder{PayAmount: 80, OutTradeNo: "our-order", PaymentTradeNo: "real-trade"}
	for _, tt := range []struct {
		name     string
		response *payment.QueryOrderResponse
		want     string
	}{
		{"nil", nil, PaymentVerificationQueryError},
		{"unknown pending", &payment.QueryOrderResponse{Status: payment.ProviderStatusPending}, PaymentVerificationQueryError},
		{"explicit unpaid", &payment.QueryOrderResponse{Status: payment.ProviderStatusPending, Metadata: map[string]string{"payment_status_known": "true"}}, PaymentVerificationUnpaid},
		{"refunded outside system", &payment.QueryOrderResponse{Status: payment.ProviderStatusRefunded, Metadata: map[string]string{"payment_status_known": "true"}}, PaymentVerificationQueryError},
		{"paid without amount", &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid}, PaymentVerificationQueryError},
		{"nonfinite", &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: math.NaN()}, PaymentVerificationQueryError},
		{"amount mismatch", &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 1}, PaymentVerificationAmountMismatch},
		{"trade mismatch", &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 80, TradeNo: "other-trade"}, PaymentVerificationTradeMismatch},
		{"verified", &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 80, TradeNo: "real-trade"}, PaymentVerificationVerified},
		{"legacy missing trade number", &payment.QueryOrderResponse{Status: payment.ProviderStatusPaid, Amount: 80, TradeNo: "our-order"}, PaymentVerificationVerified},
	} {
		t.Run(tt.name, func(t *testing.T) {
			outcome, reason := paymentVerificationOutcome(order, tt.response)
			require.Equal(t, tt.want, outcome)
			require.NotEmpty(t, reason)
		})
	}
}

type paymentVerificationCoreRepoStub struct {
	PaymentVerificationRepository
	record       *PaymentVerificationRecord
	ids          []int64
	resolveCalls int
	listLimit    int
	saveErr      error
	resolveErr   error
}

func (r *paymentVerificationCoreRepoStub) GetByID(context.Context, int64) (*PaymentVerificationRecord, error) {
	if r.record == nil {
		return nil, ErrPaymentVerificationNotFound
	}
	record := *r.record
	return &record, nil
}
func (r *paymentVerificationCoreRepoStub) GetByToken(_ context.Context, token string) (*PaymentVerificationRecord, error) {
	if r.record == nil || r.record.ActionToken != token {
		return nil, ErrPaymentVerificationNotFound
	}
	copy := *r.record
	return &copy, nil
}
func (r *paymentVerificationCoreRepoStub) ListCandidates(_ context.Context, _ time.Time, _ time.Time, limit int) ([]int64, error) {
	r.listLimit = limit
	return r.ids, nil
}
func (r *paymentVerificationCoreRepoStub) SaveCheck(_ context.Context, record *PaymentVerificationRecord, _ time.Time) (*PaymentVerificationRecord, error) {
	if r.saveErr != nil {
		return nil, r.saveErr
	}
	copy := *record
	copy.ID = 1
	if r.record != nil {
		if r.record.State == PaymentVerificationIgnored || r.record.State == PaymentVerificationBanned {
			return r.record, nil
		}
		copy.ActionToken = r.record.ActionToken
	}
	r.record = &copy
	return &copy, nil
}
func (r *paymentVerificationCoreRepoStub) Resolve(_ context.Context, id int64, checkedAt time.Time, action, actor string, now time.Time) (*PaymentVerificationRecord, error) {
	r.resolveCalls++
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	if r.record.ID != id || r.record.State != PaymentVerificationPending || !checkedAt.Equal(r.record.CheckedAt) {
		return nil, ErrPaymentVerificationChanged
	}
	if action == "ban" {
		r.record.State = PaymentVerificationBanned
	} else {
		r.record.State = PaymentVerificationIgnored
	}
	r.record.HandledAt = &now
	r.record.HandledBy = actor
	copy := *r.record
	return &copy, nil
}

func paymentVerificationTestService(t *testing.T, body string) (*PaymentVerificationService, *dbent.PaymentOrder, *paymentVerificationCoreRepoStub, *mockAuthCacheInvalidator, *int) {
	t.Helper()
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusCompleted, time.Now())
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, order.OutTradeNo, r.URL.Query().Get("out_trade_no"))
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	inst, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).SetName("original merchant").
		SetSupportedTypes("alipay").SetEnabled(true).SetConfig(encryptWebhookProviderConfig(t, map[string]string{
		"pid": "merchant-test", "pkey": "test-only-secret", "apiBase": server.URL,
		"notifyUrl": "https://example.com/notify", "returnUrl": "https://example.com/return",
	})).Save(ctx)
	require.NoError(t, err)
	order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).
		ClearPlanID().ClearSubscriptionDays().ClearSubscriptionGroupID().SetCompletedAt(time.Now()).
		SetProviderKey(payment.TypeEasyPay).SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).Save(ctx)
	require.NoError(t, err)
	payments := &PaymentService{entClient: client, loadBalancer: newWebhookProviderTestLoadBalancer(client)}
	settings := &settingRepoStub{values: map[string]string{}}
	ops := newOpsTelegramTestService(settings)
	raw, err := json.Marshal(opsTelegramStoredConfig{Version: 2, Templates: []opsTelegramStoredTemplate{{ID: "verification", Name: "test", Enabled: true,
		BotTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("12345:" + strings.Repeat("a", 35))), ChatID: "12345", BaseURL: "https://api.telegram.org"}}})
	require.NoError(t, err)
	settings.values[SettingKeyOpsTelegramNotificationConfig] = string(raw)
	settings.values[paymentVerificationSettingKey] = `{"enabled":true,"lookback_days":7,"interval_minutes":5,"template_id":"verification"}`
	repo := &paymentVerificationCoreRepoStub{ids: []int64{order.ID}, record: &PaymentVerificationRecord{ID: 1, OrderID: order.ID, UserID: order.UserID,
		Result: PaymentVerificationUnpaid, State: PaymentVerificationPending, CheckedAt: time.Now().Add(-time.Minute), ActionToken: strings.Repeat("a", 32), ExpiresAt: time.Now().Add(time.Hour)}}
	cache := &mockAuthCacheInvalidator{}
	svc := NewPaymentVerificationService(repo, payments, ops, cache)
	t.Cleanup(svc.Stop)
	return svc, order, repo, cache, &queries
}

func TestPaymentVerificationScanHasNoFulfillmentSideEffects(t *testing.T) {
	svc, order, repo, _, queries := paymentVerificationTestService(t, `{"code":1,"status":"1","money":"80","trade_no":"trade-fulfillment"}`)
	before, err := svc.payments.entClient.User.Get(context.Background(), order.UserID)
	require.NoError(t, err)
	result, err := svc.Scan(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, result.Checked)
	require.Equal(t, 1, result.Verified)
	require.Equal(t, 10, repo.listLimit)
	require.Equal(t, 1, *queries)
	require.Equal(t, PaymentVerificationResolved, repo.record.State)
	latest, err := svc.payments.entClient.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, order.Status, latest.Status)
	require.Equal(t, order.PaidAt, latest.PaidAt)
	require.Equal(t, order.CompletedAt, latest.CompletedAt)
	after, err := svc.payments.entClient.User.Get(context.Background(), order.UserID)
	require.NoError(t, err)
	require.Equal(t, before.Balance, after.Balance)
}

func TestPaymentVerificationResolveRechecksBeforeBan(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		wantErr    error
		wantBan    bool
	}{
		{"unpaid", `{"code":1,"status":"0","money":"80"}`, nil, true},
		{"amount mismatch", `{"code":1,"status":"1","money":"1"}`, nil, true},
		{"recovered", `{"code":1,"status":"1","money":"80","trade_no":"trade-fulfillment"}`, ErrPaymentVerificationRecovered, false},
		{"business error is not proof", `{"code":-5,"msg":"test-only-secret"}`, ErrPaymentVerificationUnsafeBan, false},
		{"missing status", `{"code":1,"money":"80"}`, ErrPaymentVerificationUnsafeBan, false},
		{"invalid payload", `not-json test-only-secret`, ErrPaymentVerificationUnsafeBan, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, order, repo, cache, queries := paymentVerificationTestService(t, tt.body)
			got, err := svc.ResolveAlert(context.Background(), 1, "ban", "telegram:100")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, *queries)
			require.NotContains(t, repo.record.Reason, "test-only-secret")
			if tt.wantBan {
				require.Equal(t, PaymentVerificationBanned, got.State)
				require.Equal(t, 1, repo.resolveCalls)
				require.Equal(t, []int64{order.UserID}, cache.invalidatedUserIDs)
			} else {
				require.Zero(t, repo.resolveCalls)
				require.Empty(t, cache.invalidatedUserIDs)
			}
		})
	}
}

func TestPaymentVerificationNoBanAfterRefundOrWithMissingBinding(t *testing.T) {
	for _, kind := range []string{"refund", "changed status", "missing binding"} {
		t.Run(kind, func(t *testing.T) {
			svc, order, repo, cache, queries := paymentVerificationTestService(t, `{"code":1,"status":0,"money":"80"}`)
			update := svc.payments.entClient.PaymentOrder.UpdateOneID(order.ID)
			switch kind {
			case "refund":
				update.SetRefundAmount(1)
			case "changed status":
				update.SetStatus(OrderStatusRefundPending)
			case "missing binding":
				update.ClearProviderInstanceID().ClearProviderSnapshot()
			}
			_, err := update.Save(context.Background())
			require.NoError(t, err)
			_, err = svc.ResolveAlert(context.Background(), 1, "ban", "admin:100")
			require.Error(t, err)
			require.Zero(t, repo.resolveCalls)
			require.Zero(t, *queries)
			require.Empty(t, cache.invalidatedUserIDs)
		})
	}
}

func TestPaymentVerificationIgnoreDoesNotQueryOrChangeBalance(t *testing.T) {
	svc, _, repo, cache, queries := paymentVerificationTestService(t, `invalid`)
	result, err := svc.ResolveAlert(context.Background(), 1, "ignore", "admin:100")
	require.NoError(t, err)
	require.Equal(t, PaymentVerificationIgnored, result.State)
	require.Zero(t, *queries)
	require.Empty(t, cache.invalidatedUserIDs)
	require.Equal(t, 1, repo.resolveCalls)
	_, err = svc.ResolveAlert(context.Background(), 1, "ban", "admin:100")
	require.ErrorIs(t, err, ErrPaymentVerificationChanged)
	require.Zero(t, *queries)
}

func TestPaymentVerificationCacheInvalidatesOnlyAfterCommit(t *testing.T) {
	svc, _, repo, cache, _ := paymentVerificationTestService(t, `{"code":1,"status":0}`)
	repo.resolveErr = errors.New("commit failed")
	_, err := svc.ResolveAlert(context.Background(), 1, "ban", "admin:100")
	require.ErrorContains(t, err, "commit failed")
	require.Empty(t, cache.invalidatedUserIDs)
}

func TestPaymentVerificationRecordHidesActionCredentials(t *testing.T) {
	token, err := paymentVerificationToken()
	require.NoError(t, err)
	require.Len(t, token, 32)
	record := PaymentVerificationRecord{ActionToken: token, TemplateID: "private-template", ConfigGeneration: "private-generation", BotFingerprint: "private-fingerprint", TelegramChatID: 999, TelegramMessageID: 888, TelegramTopicID: 777}
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	for _, value := range []string{token, "private-template", "private-generation", "private-fingerprint", "telegram_chat_id", "telegram_message_id", "telegram_topic_id"} {
		require.NotContains(t, string(raw), value)
	}
}
