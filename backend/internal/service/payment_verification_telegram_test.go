package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type paymentVerificationTelegramRepoStub struct {
	PaymentVerificationRepository
	record       PaymentVerificationRecord
	resolveCalls int
	lookupCalls  int
	retryCalls   int
	markCalls    int
}

func (r *paymentVerificationTelegramRepoStub) GetByToken(_ context.Context, token string) (*PaymentVerificationRecord, error) {
	r.lookupCalls++
	if token != r.record.ActionToken {
		return nil, ErrPaymentVerificationNotFound
	}
	copy := r.record
	return &copy, nil
}

func (r *paymentVerificationTelegramRepoStub) GetByID(_ context.Context, id int64) (*PaymentVerificationRecord, error) {
	if id != r.record.ID {
		return nil, ErrPaymentVerificationNotFound
	}
	copy := r.record
	return &copy, nil
}

func (r *paymentVerificationTelegramRepoStub) Resolve(_ context.Context, id int64, checkedAt time.Time, action, actor string, now time.Time) (*PaymentVerificationRecord, error) {
	if id != r.record.ID || !checkedAt.Equal(r.record.CheckedAt) || r.record.State != PaymentVerificationPending {
		return nil, ErrPaymentVerificationChanged
	}
	r.resolveCalls++
	r.record.State = PaymentVerificationIgnored
	if action == "ban" {
		r.record.State = PaymentVerificationBanned
	}
	r.record.HandledBy, r.record.HandledAt = actor, &now
	copy := r.record
	return &copy, nil
}

func (r *paymentVerificationTelegramRepoStub) ClaimNotifications(_ context.Context, _ string, _ time.Time, _ int, _ time.Duration) ([]PaymentVerificationRecord, error) {
	if r.record.NotifiedAt != nil {
		return nil, nil
	}
	return []PaymentVerificationRecord{r.record}, nil
}

func (r *paymentVerificationTelegramRepoStub) PrepareNotification(_ context.Context, _ int64, _ string, binding PaymentVerificationNotificationBinding) error {
	r.record.TemplateID = binding.TemplateID
	r.record.ConfigGeneration = binding.ConfigGeneration
	r.record.BotFingerprint = binding.BotFingerprint
	r.record.TelegramChatID = binding.TelegramChatID
	r.record.TelegramTopicID = binding.TelegramTopicID
	return nil
}

func (r *paymentVerificationTelegramRepoStub) MarkNotified(_ context.Context, _ int64, _ string, messageID int64, now time.Time) error {
	r.markCalls++
	r.record.TelegramMessageID, r.record.NotifiedAt = messageID, &now
	return nil
}

func (r *paymentVerificationTelegramRepoStub) RetryNotification(_ context.Context, _ int64, _ string, _ time.Time) error {
	r.retryCalls++
	return nil
}

func paymentVerificationTestResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func newPaymentVerificationTelegramTestService(t *testing.T) (*PaymentVerificationService, *paymentVerificationTelegramRepoStub) {
	t.Helper()
	ops := newOpsTelegramTestService(newRuntimeSettingRepoStub())
	topic := int64(42)
	if _, err := ops.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates: []OpsTelegramNotificationTemplateUpdate{{
			ID: "payments", Name: "Payment alerts", Enabled: true, BotToken: "123456:verification-secret",
			ChatID: "-100123456", TopicID: &topic, BaseURL: opsTelegramDefaultBaseURL,
		}},
	}); err != nil {
		t.Fatalf("save template: %v", err)
	}
	now := time.Now().UTC()
	repo := &paymentVerificationTelegramRepoStub{record: PaymentVerificationRecord{
		ID: 11, OrderID: 99, UserID: 77, Email: "person+[special](x)@example.com", ProviderName: "Test gateway",
		OutTradeNo: "sub2-test", Amount: 10, PayAmount: 70, Currency: "CNY",
		Result: PaymentVerificationAmountMismatch, State: PaymentVerificationPending,
		ActionToken: strings.Repeat("a", 32), ExpiresAt: now.Add(time.Hour), CheckedAt: now,
	}}
	svc := NewPaymentVerificationService(repo, nil, ops, nil)
	return svc, repo
}

func paymentVerificationTestConfig() *PaymentVerificationConfig {
	return &PaymentVerificationConfig{Enabled: true, TemplateID: "payments", LookbackDays: 7, IntervalMinutes: 5,
		AdminTelegramIDs: []int64{123}, WebhookURL: "https://app.example.com/api/v1/payment/verification/telegram"}
}

func configurePaymentVerificationTelegram(t *testing.T, svc *PaymentVerificationService) string {
	t.Helper()
	if _, err := svc.UpdateConfig(context.Background(), paymentVerificationTestConfig()); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/getWebhookInfo") {
			return paymentVerificationTestResponse(`{"ok":true,"result":{"url":""}}`), nil
		}
		if !strings.HasSuffix(req.URL.Path, "/setWebhook") {
			t.Fatalf("unexpected connect method: %s", req.URL.Path)
		}
		return paymentVerificationTestResponse(`{"ok":true,"result":true}`), nil
	})}
	if err := svc.ConnectTelegram(context.Background()); err != nil {
		t.Fatalf("ConnectTelegram: %v", err)
	}
	stored, err := svc.loadPaymentVerificationConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	secret, err := svc.ops.encryptor.Decrypt(stored.WebhookSecretEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func bindPaymentVerificationTelegramRecord(t *testing.T, svc *PaymentVerificationService, repo *paymentVerificationTelegramRepoStub) {
	t.Helper()
	stored, err := svc.loadPaymentVerificationConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	delivery, fingerprint, err := svc.paymentVerificationDelivery(context.Background(), stored.TemplateID)
	if err != nil {
		t.Fatal(err)
	}
	repo.record.TemplateID, repo.record.ConfigGeneration, repo.record.BotFingerprint = stored.TemplateID, stored.Generation, fingerprint
	repo.record.TelegramChatID, repo.record.TelegramMessageID, repo.record.TelegramTopicID = -100123456, 555, paymentVerificationTopicID(delivery.TopicID)
}

func paymentVerificationCallbackBody(t *testing.T, action string, mutate func(map[string]any)) []byte {
	t.Helper()
	callback := map[string]any{
		"id": "callback-123", "from": map[string]any{"id": int64(123), "is_bot": false},
		"data":    "pv:" + action + ":" + strings.Repeat("a", 32),
		"message": map[string]any{"message_id": int64(555), "message_thread_id": int64(42), "chat": map[string]any{"id": int64(-100123456)}},
	}
	if mutate != nil {
		mutate(callback)
	}
	body, err := json.Marshal(map[string]any{"callback_query": callback})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func paymentVerificationTestObject(t *testing.T, fields map[string]any, key string) map[string]any {
	t.Helper()
	object, ok := fields[key].(map[string]any)
	if !ok {
		t.Fatalf("callback field %q is not an object", key)
	}
	return object
}

func TestPaymentVerificationConfigDefaultsAndSavingIsLocal(t *testing.T) {
	svc, _ := newPaymentVerificationTelegramTestService(t)
	cfg, err := svc.GetConfig(context.Background())
	if err != nil || cfg.Enabled || cfg.LookbackDays != 7 || cfg.IntervalMinutes != 5 || cfg.WebhookConfigured {
		t.Fatalf("unexpected defaults: %+v err=%v", cfg, err)
	}
	requests := 0
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("must not send on save")
	})}
	input := paymentVerificationTestConfig()
	input.WebhookConfigured = true
	got, err := svc.UpdateConfig(context.Background(), input)
	if err != nil || requests != 0 || got.WebhookConfigured {
		t.Fatalf("save must be local and cannot trust configured flag: %+v requests=%d err=%v", got, requests, err)
	}
	stored, err := svc.loadPaymentVerificationConfig(context.Background())
	if err != nil || stored.Generation == "" || stored.WebhookSecretEncrypted == "" {
		t.Fatalf("missing encrypted webhook configuration: err=%v", err)
	}
	secret, err := svc.ops.encryptor.Decrypt(stored.WebhookSecretEncrypted)
	if err != nil || secret == stored.WebhookSecretEncrypted {
		t.Fatal("secret must be encrypted at rest")
	}
	publicJSON, _ := json.Marshal(got)
	if strings.Contains(string(publicJSON), secret) || strings.Contains(string(publicJSON), "secret") || strings.Contains(string(publicJSON), "generation") {
		t.Fatal("public configuration must not reveal callback credentials")
	}
}

func TestPaymentVerificationConfigValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*PaymentVerificationConfig)
	}{
		{"missing template", func(c *PaymentVerificationConfig) { c.TemplateID = "missing" }},
		{"missing admin", func(c *PaymentVerificationConfig) { c.AdminTelegramIDs = nil }},
		{"invalid admin", func(c *PaymentVerificationConfig) { c.AdminTelegramIDs = []int64{-1} }},
		{"lookback range", func(c *PaymentVerificationConfig) { c.LookbackDays = 366 }},
		{"interval range", func(c *PaymentVerificationConfig) { c.IntervalMinutes = 61 }},
		{"http callback", func(c *PaymentVerificationConfig) {
			c.WebhookURL = "http://app.example.com/api/v1/payment/verification/telegram"
		}},
		{"private callback", func(c *PaymentVerificationConfig) {
			c.WebhookURL = "https://127.0.0.1/api/v1/payment/verification/telegram"
		}},
		{"callback query", func(c *PaymentVerificationConfig) { c.WebhookURL += "?secret=value" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newPaymentVerificationTelegramTestService(t)
			cfg := paymentVerificationTestConfig()
			tc.mutate(cfg)
			if _, err := svc.UpdateConfig(context.Background(), cfg); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestPaymentVerificationConnectRequiresExplicitRegistrationAndDoesNotOverwrite(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(map[bool]string{true: "foreign webhook", false: "unused bot"}[occupied], func(t *testing.T) {
			svc, _ := newPaymentVerificationTelegramTestService(t)
			if _, err := svc.UpdateConfig(context.Background(), paymentVerificationTestConfig()); err != nil {
				t.Fatal(err)
			}
			setCalls := 0
			svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/getWebhookInfo") {
					if occupied {
						return paymentVerificationTestResponse(`{"ok":true,"result":{"url":"https://other.example/webhook"}}`), nil
					}
					return paymentVerificationTestResponse(`{"ok":true,"result":{"url":""}}`), nil
				}
				setCalls++
				var payload struct {
					URL            string   `json:"url"`
					Secret         string   `json:"secret_token"`
					AllowedUpdates []string `json:"allowed_updates"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || payload.URL != paymentVerificationTestConfig().WebhookURL || len(payload.Secret) < 32 || len(payload.AllowedUpdates) != 1 || payload.AllowedUpdates[0] != "callback_query" {
					t.Fatal("setWebhook must set dedicated secret and callback-only delivery")
				}
				return paymentVerificationTestResponse(`{"ok":true,"result":true}`), nil
			})}
			err := svc.ConnectTelegram(context.Background())
			cfg, getErr := svc.GetConfig(context.Background())
			if occupied {
				if err == nil || !strings.Contains(err.Error(), "专用机器人") || setCalls != 0 || cfg.WebhookConfigured {
					t.Fatalf("existing webhook must be preserved, requests=%d err=%v", setCalls, err)
				}
			} else if err != nil || getErr != nil || setCalls != 1 || !cfg.WebhookConfigured {
				t.Fatalf("connect failed: setCalls=%d config=%+v err=%v", setCalls, cfg, err)
			}
		})
	}
}

func TestPaymentVerificationConfigChangesRevokeConnections(t *testing.T) {
	for _, changed := range []string{"settings", "token", "chat", "topic", "base", "disabled"} {
		t.Run(changed, func(t *testing.T) {
			svc, repo := newPaymentVerificationTelegramTestService(t)
			secret := configurePaymentVerificationTelegram(t, svc)
			bindPaymentVerificationTelegramRecord(t, svc, repo)
			if changed == "settings" || changed == "disabled" {
				cfg := paymentVerificationTestConfig()
				cfg.LookbackDays = 8
				if changed == "disabled" {
					cfg.Enabled = false
				}
				if _, err := svc.UpdateConfig(context.Background(), cfg); err != nil {
					t.Fatal(err)
				}
			} else {
				cfg, err := svc.ops.loadOpsTelegramStoredConfig(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				switch changed {
				case "token":
					cfg.Templates[0].BotTokenEncrypted, _ = svc.ops.encryptor.Encrypt("222222:replacement")
				case "chat":
					cfg.Templates[0].ChatID = "-100999"
				case "topic":
					v := int64(43)
					cfg.Templates[0].TopicID = &v
				case "base":
					cfg.Templates[0].BaseURL = "https://other.example"
				}
				raw, _ := json.Marshal(cfg)
				if err := svc.ops.settingRepo.Set(context.Background(), SettingKeyOpsTelegramNotificationConfig, string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := svc.GetConfig(context.Background())
			if err != nil || cfg.WebhookConfigured {
				t.Fatalf("changed configuration must require reconnect: %+v err=%v", cfg, err)
			}
			if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", nil)); err == nil || repo.resolveCalls != 0 {
				t.Fatal("old buttons must no longer authorize actions")
			}
		})
	}
}

func TestPaymentVerificationTelegramCallbackRejectsUnauthorizedBindings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"sender", func(c map[string]any) { paymentVerificationTestObject(t, c, "from")["id"] = int64(999) }},
		{"bot sender", func(c map[string]any) { paymentVerificationTestObject(t, c, "from")["is_bot"] = true }},
		{"chat", func(c map[string]any) {
			paymentVerificationTestObject(t, paymentVerificationTestObject(t, c, "message"), "chat")["id"] = int64(-555)
		}},
		{"message", func(c map[string]any) { paymentVerificationTestObject(t, c, "message")["message_id"] = int64(556) }},
		{"topic", func(c map[string]any) {
			paymentVerificationTestObject(t, c, "message")["message_thread_id"] = int64(43)
		}},
		{"inline message", func(c map[string]any) { delete(c, "message") }},
		{"arbitrary action", func(c map[string]any) { c["data"] = "pv:delete:" + strings.Repeat("a", 32) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newPaymentVerificationTelegramTestService(t)
			secret := configurePaymentVerificationTelegram(t, svc)
			bindPaymentVerificationTelegramRecord(t, svc, repo)
			if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", tc.mutate)); err == nil || repo.resolveCalls != 0 {
				t.Fatal("invalid callback authorization accepted")
			}
		})
	}
	for _, secret := range []string{"", "invalid"} {
		svc, repo := newPaymentVerificationTelegramTestService(t)
		configurePaymentVerificationTelegram(t, svc)
		if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", nil)); err == nil || repo.lookupCalls != 0 {
			t.Fatal("webhook secret must be checked before looking up events")
		}
	}
}

func TestPaymentVerificationTelegramIgnoreIsBoundAndIdempotent(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	secret := configurePaymentVerificationTelegram(t, svc)
	bindPaymentVerificationTelegramRecord(t, svc, repo)
	answers, edits := 0, 0
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/answerCallbackQuery"):
			answers++
		case strings.HasSuffix(req.URL.Path, "/editMessageText"):
			edits++
			var payload struct {
				Text        string                              `json:"text"`
				ReplyMarkup paymentVerificationTelegramKeyboard `json:"reply_markup"`
			}
			if err := json.NewDecoder(req.Body).Decode(&payload); err != nil || len(payload.ReplyMarkup.InlineKeyboard) != 0 || !strings.Contains(payload.Text, "已忽略") || !strings.Contains(payload.Text, "telegram:123") {
				t.Fatal("resolved message must show actor and remove buttons")
			}
		default:
			t.Fatal("unexpected Telegram method")
		}
		return paymentVerificationTestResponse(`{"ok":true,"result":true}`), nil
	})}
	for i := 0; i < 2; i++ {
		if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", nil)); err != nil {
			t.Fatalf("HandleTelegramCallback: %v", err)
		}
	}
	if repo.resolveCalls != 1 || repo.record.State != PaymentVerificationIgnored || repo.record.UserID != 77 || answers != 2 || edits != 2 {
		t.Fatalf("callback must resolve once and preserve target: calls=%d state=%s answers=%d edits=%d", repo.resolveCalls, repo.record.State, answers, edits)
	}
}

func TestPaymentVerificationTelegramPendingQueryNeverOffersOrExecutesBan(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	secret := configurePaymentVerificationTelegram(t, svc)
	repo.record.Result = PaymentVerificationQueryError
	zero := float64(0)
	repo.record.UpstreamAmount = &zero // Do not render an unknown amount as a real zero payment.
	var sentText string
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/answerCallbackQuery") {
			return paymentVerificationTestResponse(`{"ok":true,"result":true}`), nil
		}
		var payload struct {
			Text        string                              `json:"text"`
			ReplyMarkup paymentVerificationTelegramKeyboard `json:"reply_markup"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		sentText = payload.Text
		if len(payload.ReplyMarkup.InlineKeyboard) != 1 || len(payload.ReplyMarkup.InlineKeyboard[0]) != 1 || payload.ReplyMarkup.InlineKeyboard[0][0].Text != "忽略本次" {
			t.Fatal("unknown payment result must only offer ignore")
		}
		return paymentVerificationTestResponse(`{"ok":true,"result":{"message_id":555,"message_thread_id":42,"chat":{"id":-100123456}}}`), nil
	})}
	if err := svc.deliverPendingAlerts(context.Background()); err != nil {
		t.Fatalf("deliverPendingAlerts: %v", err)
	}
	if !strings.Contains(sentText, "待核实") || strings.Contains(sentText, "平台查询金额") || strings.Contains(sentText, "未支付") || strings.Contains(sentText, "不存在") {
		t.Fatalf("unknown result must not make a fraud claim: %s", sentText)
	}
	if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "b", nil)); err != nil || repo.resolveCalls != 0 {
		t.Fatalf("forged ban button on unknown result must be ignored: calls=%d err=%v", repo.resolveCalls, err)
	}
}

func TestPaymentVerificationTelegramDeliveryFailureRetriesWithoutLeakingToken(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	configurePaymentVerificationTelegram(t, svc)
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New(req.URL.String())
	})}
	err := svc.deliverPendingAlerts(context.Background())
	if err == nil || strings.Contains(err.Error(), "verification-secret") || repo.retryCalls != 1 || repo.markCalls != 0 {
		t.Fatalf("failed sends must be queued again with safe errors: retries=%d marked=%d err=%v", repo.retryCalls, repo.markCalls, err)
	}
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return paymentVerificationTestResponse(`{"ok":true,"result":{"message_id":555,"message_thread_id":42,"chat":{"id":-100123456}}}`), nil
	})}
	if err := svc.deliverPendingAlerts(context.Background()); err != nil || repo.markCalls != 1 || repo.record.NotifiedAt == nil {
		t.Fatalf("successful retry must bind message: err=%v", err)
	}
}

func TestPaymentVerificationTelegramExpiredCallbackCannotResolve(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	secret := configurePaymentVerificationTelegram(t, svc)
	bindPaymentVerificationTelegramRecord(t, svc, repo)
	repo.record.ExpiresAt = time.Now().Add(-time.Minute)
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/answerCallbackQuery") {
			t.Fatal("expired callback must not mutate the message or user")
		}
		return paymentVerificationTestResponse(`{"ok":true,"result":true}`), nil
	})}
	if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", nil)); err != nil || repo.resolveCalls != 0 {
		t.Fatalf("expired action was executed: err=%v", err)
	}
}

func TestPaymentVerificationTelegramCommittedActionAcknowledgesFeedbackFailure(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	secret := configurePaymentVerificationTelegram(t, svc)
	bindPaymentVerificationTelegramRecord(t, svc, repo)
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("temporary Telegram failure")
	})}
	for i := 0; i < 2; i++ {
		if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", nil)); err != nil {
			t.Fatalf("committed action must not become HTTP 500 because Telegram feedback failed: %v", err)
		}
	}
	if repo.resolveCalls != 1 || repo.record.State != PaymentVerificationIgnored {
		t.Fatalf("feedback failure repeated the action: calls=%d state=%s", repo.resolveCalls, repo.record.State)
	}
}

func TestPaymentVerificationTelegramFailedRegistrationStaysDisconnected(t *testing.T) {
	for _, body := range []string{`{"ok":false,"description":"123456:verification-secret"}`, `{"ok":true,"result":false}`} {
		svc, _ := newPaymentVerificationTelegramTestService(t)
		if _, err := svc.UpdateConfig(context.Background(), paymentVerificationTestConfig()); err != nil {
			t.Fatal(err)
		}
		svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.HasSuffix(req.URL.Path, "/getWebhookInfo") {
				return paymentVerificationTestResponse(`{"ok":true,"result":{"url":""}}`), nil
			}
			return paymentVerificationTestResponse(body), nil
		})}
		err := svc.ConnectTelegram(context.Background())
		if err == nil || strings.Contains(err.Error(), "verification-secret") {
			t.Fatal("registration failure must be reported without Telegram's raw error")
		}
		cfg, err := svc.GetConfig(context.Background())
		if err != nil || cfg.WebhookConfigured {
			t.Fatalf("unsuccessful registration cannot be advertised as connected: %+v err=%v", cfg, err)
		}
	}
}

func TestPaymentVerificationTelegramUnconnectedDoesNotDeliver(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	if _, err := svc.UpdateConfig(context.Background(), paymentVerificationTestConfig()); err != nil {
		t.Fatal(err)
	}
	requests := 0
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("must not send unusable buttons")
	})}
	if err := svc.deliverPendingAlerts(context.Background()); err != nil || requests != 0 || repo.markCalls != 0 {
		t.Fatalf("delivery before explicit webhook connection: requests=%d err=%v", requests, err)
	}
}

func TestPaymentVerificationTelegramSendResponseCannotRebindTarget(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	configurePaymentVerificationTelegram(t, svc)
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return paymentVerificationTestResponse(`{"ok":true,"result":{"message_id":555,"message_thread_id":42,"chat":{"id":-100666}}}`), nil
	})}
	if err := svc.deliverPendingAlerts(context.Background()); err == nil || repo.markCalls != 0 || repo.retryCalls != 1 || repo.record.TelegramChatID != -100123456 {
		t.Fatal("send response cannot change the configured authorized chat")
	}
}

func TestPaymentVerificationTelegramOldGenerationIsAcknowledgedWithoutAction(t *testing.T) {
	svc, repo := newPaymentVerificationTelegramTestService(t)
	secret := configurePaymentVerificationTelegram(t, svc)
	bindPaymentVerificationTelegramRecord(t, svc, repo)
	repo.record.ConfigGeneration = "previous-configuration"
	answers := 0
	svc.ops.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/answerCallbackQuery") {
			t.Fatal("revoked button must only receive an explanation")
		}
		answers++
		return paymentVerificationTestResponse(`{"ok":true,"result":true}`), nil
	})}
	if err := svc.HandleTelegramCallback(context.Background(), secret, paymentVerificationCallbackBody(t, "i", nil)); err != nil || repo.resolveCalls != 0 || answers != 1 {
		t.Fatalf("old generation should be acknowledged without execution: answers=%d err=%v", answers, err)
	}
}
