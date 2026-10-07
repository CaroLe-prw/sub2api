package provider

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/payment"
)

func newEasyPayNotificationProvider(t *testing.T) *EasyPay {
	t.Helper()
	e, err := NewEasyPay("notify-test", map[string]string{
		"pid":         "1001",
		"pkey":        "notification-test-secret",
		"apiBase":     "https://pay.example.com",
		"notifyUrl":   "https://app.example.com/api/v1/payment/webhook/easypay",
		"returnUrl":   "https://app.example.com/payment/result",
		"paymentMode": paymentModePopup,
	})
	if err != nil {
		t.Fatalf("NewEasyPay: %v", err)
	}
	return e
}

func signedEasyPayNotification(e *EasyPay, param *string) url.Values {
	params := map[string]string{
		"pid":          "1001",
		"trade_no":     "gateway-123",
		"out_trade_no": "sub2-notify-test",
		"type":         "alipay",
		"name":         "Balance recharge",
		"money":        "12.34",
		"trade_status": tradeStatusSuccess,
	}
	if param != nil {
		params["param"] = *param
	}
	params["sign"] = easyPaySign(params, e.config["pkey"])
	params["sign_type"] = signTypeMD5
	values := url.Values{}
	for key, value := range params {
		values.Set(key, value)
	}
	return values
}

func TestEasyPayVerifyNotificationAcceptsStandardCallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		param *string
	}{
		{name: "without optional param"},
		{name: "with optional param", param: stringPointer("customer=123&source=checkout")},
		{name: "with empty optional param", param: stringPointer("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEasyPayNotificationProvider(t)
			notification, err := e.VerifyNotification(context.Background(), signedEasyPayNotification(e, tc.param).Encode(), nil)
			if err != nil {
				t.Fatalf("VerifyNotification: %v", err)
			}
			if notification.OrderID != "sub2-notify-test" || notification.TradeNo != "gateway-123" || notification.Amount != 12.34 || notification.Status != payment.ProviderStatusSuccess {
				t.Fatalf("unexpected notification: %+v", notification)
			}
			if notification.Metadata["pid"] != "1001" {
				t.Fatalf("pid = %q, want 1001", notification.Metadata["pid"])
			}
		})
	}
}

func TestEasyPayVerifyNotificationRejectsUnknownParameters(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"notify_url", "return_url", "cid", "device", "clientip", "unknown", ""} {
		for _, value := range []string{"", "unexpected-value"} {
			t.Run(key+"/"+value, func(t *testing.T) {
				t.Parallel()
				e := newEasyPayNotificationProvider(t)
				values := signedEasyPayNotification(e, nil)
				values.Set(key, value)
				notification, err := e.VerifyNotification(context.Background(), values.Encode(), nil)
				if err == nil || !strings.Contains(err.Error(), "unexpected notify parameter") {
					t.Fatalf("unknown field must be rejected before signature verification, notification=%+v err=%v", notification, err)
				}
				if notification != nil {
					t.Fatalf("rejected callback returned a notification: %+v", notification)
				}
			})
		}
	}
}

func TestEasyPayVerifyNotificationRejectsDuplicateParameters(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"pid", "trade_no", "out_trade_no", "type", "name", "money", "trade_status", "param", "sign", "sign_type"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			e := newEasyPayNotificationProvider(t)
			values := signedEasyPayNotification(e, stringPointer("custom-value"))
			// Even identical duplicates must not rely on first/last-value parsing.
			values.Add(key, values.Get(key))
			notification, err := e.VerifyNotification(context.Background(), values.Encode(), nil)
			if err == nil || !strings.Contains(err.Error(), "duplicate notify parameter") {
				t.Fatalf("duplicate field must be rejected before signature verification, notification=%+v err=%v", notification, err)
			}
		})
	}
}

func TestEasyPayVerifyNotificationRejectsPopupPaymentURLReplay(t *testing.T) {
	t.Parallel()
	e := newEasyPayNotificationProvider(t)
	response, err := e.createRedirectPayment(payment.CreatePaymentRequest{
		OrderID: "sub2-replay", Amount: "12.34", PaymentType: payment.TypeAlipay, Subject: "Balance recharge",
	})
	if err != nil {
		t.Fatalf("createRedirectPayment: %v", err)
	}
	payURL, err := url.Parse(response.PayURL)
	if err != nil {
		t.Fatalf("parse PayURL: %v", err)
	}
	// Replay the real checkout signature without access to the signing function.
	if notification, err := e.VerifyNotification(context.Background(), payURL.RawQuery, nil); err == nil || !strings.Contains(err.Error(), "unexpected notify parameter") {
		t.Fatalf("checkout parameters must not verify as a notification, notification=%+v err=%v", notification, err)
	}
}

func TestEasyPayVerifyNotificationRejectsIssue7881SignatureReuse(t *testing.T) {
	t.Parallel()
	e := newEasyPayNotificationProvider(t)
	const returnURL = "https://app.example.com/payment/result?source=checkout"
	response, err := e.createRedirectPayment(payment.CreatePaymentRequest{
		OrderID: "sub2-forged", Amount: "12.34", PaymentType: payment.TypeAlipay, Subject: "Balance recharge",
		ReturnURL: returnURL + "&trade_no=forged-trade&trade_status=TRADE_SUCCESS",
	})
	if err != nil {
		t.Fatalf("createRedirectPayment: %v", err)
	}
	payURL, err := url.Parse(response.PayURL)
	if err != nil {
		t.Fatalf("parse PayURL: %v", err)
	}
	values := payURL.Query()
	// This moves fields out of the signed return_url value. The canonical MD5
	// input is unchanged, so the checkout signature works without knowing pkey.
	// Keeping checkout-only fields must cause rejection before signature checks.
	values.Set("return_url", returnURL)
	values.Set("trade_no", "forged-trade")
	values.Set("trade_status", tradeStatusSuccess)
	if notification, err := e.VerifyNotification(context.Background(), values.Encode(), nil); err == nil || !strings.Contains(err.Error(), "unexpected notify parameter") {
		t.Fatalf("reused checkout signature must not produce a paid callback, notification=%+v err=%v", notification, err)
	}
}

func stringPointer(value string) *string { return &value }
