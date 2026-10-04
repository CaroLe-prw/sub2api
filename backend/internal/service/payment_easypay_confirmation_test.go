//go:build unit

package service

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/stretchr/testify/require"
)

func TestEasyPayCallbackRequiresUpstreamConfirmation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		body       string
		httpStatus int
		wantPaid   bool
	}{
		{"paid string status", `{"code":1,"status":"1","money":"80","trade_no":"gateway-trade"}`, 200, true},
		{"paid nested string status", `{"code":1,"data":{"status":"1","money":"80","trade_no":"gateway-trade"}}`, 200, true},
		{"unpaid string status", `{"code":1,"status":"0","money":"80"}`, 200, false},
		{"string paid status wrong amount", `{"code":1,"status":"1","money":"1"}`, 200, false},
		{"invalid string status", `{"code":1,"status":"paid","money":"80"}`, 200, false},
		{"unpaid despite valid callback signature", `{"code":1,"status":0,"money":"80"}`, 200, false},
		{"order not found", `{"code":0,"msg":"not found"}`, 200, false},
		{"failed business response with paid fields", `{"code":0,"status":1,"money":"80"}`, 200, false},
		{"HTTP failure with paid fields", `{"code":1,"status":1,"money":"80"}`, 503, false},
		{"malformed response", `not json`, 200, false},
		{"connection failure", "", 0, false},
		{"different order", `{"code":1,"status":1,"money":"80","out_trade_no":"other-order"}`, 200, false},
		{"different nested order", `{"code":1,"data":{"status":1,"money":"80","out_trade_no":"other-order"}}`, 200, false},
		{"wrong amount", `{"code":1,"status":1,"money":"1"}`, 200, false},
		{"zero amount", `{"code":1,"status":1,"money":"0"}`, 200, false},
		{"invalid amount", `{"code":1,"status":1,"money":"NaN"}`, 200, false},
		{"different transaction", `{"code":1,"status":1,"money":"80","trade_no":"other-trade"}`, 200, false},
		{"paid", `{"code":1,"status":1,"money":"80","trade_no":"gateway-trade"}`, 200, true},
		{"paid legacy response without trade number", `{"code":1,"status":1,"money":"80"}`, 200, true},
		{"active query", `{"code":1,"status":1,"money":"80","trade_no":"gateway-trade"}`, 200, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createPaymentFulfillmentSubscriptionOrder(t, ctx, client, OrderStatusPending, time.Now())
			var queries atomic.Int32
			var recovered atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				for key, want := range map[string]string{"act": "order", "pid": "merchant-test", "key": "test-only-secret", "out_trade_no": order.OutTradeNo} {
					if got := r.URL.Query().Get(key); got != want {
						t.Errorf("wrong query field %s", key)
					}
				}
				if r.Method != http.MethodGet || r.URL.Path != "/api.php" {
					t.Error("unexpected query endpoint")
				}
				if recovered.Load() {
					_, _ = w.Write([]byte(`{"code":1,"status":1,"money":"80","trade_no":"gateway-trade"}`))
					return
				}
				if tt.httpStatus == 0 {
					panic(http.ErrAbortHandler)
				}
				w.WriteHeader(tt.httpStatus)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)
			inst, err := client.PaymentProviderInstance.Create().SetProviderKey(payment.TypeEasyPay).
				SetName("original merchant").SetSupportedTypes("alipay").SetEnabled(true).
				SetConfig(encryptWebhookProviderConfig(t, map[string]string{
					"pid": "merchant-test", "pkey": "test-only-secret", "apiBase": server.URL,
					"notifyUrl": "https://example.com/notify", "returnUrl": "https://example.com/return",
				})).Save(ctx)
			require.NoError(t, err)
			order, err = client.PaymentOrder.UpdateOneID(order.ID).SetOrderType(payment.OrderTypeBalance).
				ClearPlanID().ClearSubscriptionGroupID().ClearSubscriptionDays().ClearPaidAt().
				SetProviderKey(payment.TypeEasyPay).SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).Save(ctx)
			require.NoError(t, err)
			redeemRepo := &paymentFulfillmentRedeemRepo{}
			credited := 0.0
			userRepo := &mockUserRepo{getByIDUser: &User{ID: order.UserID}}
			userRepo.updateBalanceFn = func(_ context.Context, id int64, amount float64) error {
				require.Equal(t, order.UserID, id)
				credited += amount
				return nil
			}
			svc := &PaymentService{entClient: client, loadBalancer: newWebhookProviderTestLoadBalancer(client), userRepo: userRepo,
				redeemService: NewRedeemService(redeemRepo, userRepo, nil, &paymentFulfillmentRedeemCacheStub{}, nil, client, nil, nil)}
			prov, err := svc.GetWebhookProvider(ctx, payment.TypeEasyPay, order.OutTradeNo)
			require.NoError(t, err)
			// Model a leaked merchant key: the forged callback passes the real verifier.
			values := url.Values{"pid": {"merchant-test"}, "out_trade_no": {order.OutTradeNo}, "trade_no": {"gateway-trade"}, "money": {"80"}, "trade_status": {"TRADE_SUCCESS"}}
			keys := make([]string, 0, len(values))
			for k := range values {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, k+"="+values.Get(k))
			}
			values.Set("sign", fmt.Sprintf("%x", md5.Sum([]byte(strings.Join(parts, "&")+"test-only-secret"))))
			n, err := prov.VerifyNotification(ctx, values.Encode(), nil)
			require.NoError(t, err)
			if tt.name == "active query" {
				_, err = svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
			} else {
				err = svc.HandlePaymentNotification(ctx, n, payment.TypeEasyPay)
			}
			if tt.wantPaid {
				require.NoError(t, err)
				require.Equal(t, order.Amount, credited)
				require.NoError(t, svc.HandlePaymentNotification(ctx, n, payment.TypeEasyPay))
				require.Equal(t, order.Amount, credited, "duplicate callback must not credit again")
			} else {
				require.Error(t, err)
				require.Zero(t, credited)
				require.Zero(t, redeemRepo.createCalls)
			}
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			if tt.wantPaid {
				require.Equal(t, OrderStatusCompleted, reloaded.Status)
			} else {
				require.Equal(t, OrderStatusPending, reloaded.Status)
				require.Nil(t, reloaded.PaidAt)
			}
			require.EqualValues(t, 1, queries.Load())
			if !tt.wantPaid {
				// A failed confirmation must remain recoverable by a later callback.
				recovered.Store(true)
				require.NoError(t, svc.HandlePaymentNotification(ctx, n, payment.TypeEasyPay))
				require.Equal(t, order.Amount, credited)
				require.EqualValues(t, 2, queries.Load())
				require.NoError(t, svc.HandlePaymentNotification(ctx, n, payment.TypeEasyPay))
				require.Equal(t, order.Amount, credited)
			}
		})
	}
}
