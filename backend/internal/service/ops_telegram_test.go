package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type opsTelegramTestEncryptor struct{}

func (opsTelegramTestEncryptor) Encrypt(plaintext string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (opsTelegramTestEncryptor) Decrypt(ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	return string(raw), err
}

type opsTelegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (f opsTelegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newOpsTelegramTestService(repo SettingRepository) *OpsService {
	return &OpsService{
		settingRepo: repo,
		cfg: &config.Config{Totp: config.TotpConfig{
			EncryptionKeyConfigured: true,
		}},
		encryptor: opsTelegramTestEncryptor{},
	}
}

func TestOpsTelegramConfigEncryptsAndPreservesToken(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := newOpsTelegramTestService(repo)
	topicID := int64(42)
	token := "123456:super-secret-token"

	got, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates: []OpsTelegramNotificationTemplateUpdate{{
			ID: "alerts", Name: "Alerts", Enabled: true, BotToken: token,
			ChatID: "-1001234567890", TopicID: &topicID, BaseURL: "https://api.telegram.org/",
			DisableNotification: true, ProtectContent: true,
		}},
		OpsAlertTemplateID: "alerts",
	})
	if err != nil {
		t.Fatalf("UpdateTelegramNotificationConfig() error = %v", err)
	}
	if len(got.Templates) != 1 || !got.Templates[0].BotTokenConfigured || got.Templates[0].BaseURL != opsTelegramDefaultBaseURL {
		t.Fatalf("unexpected public config: %+v", got)
	}
	raw := repo.values[SettingKeyOpsTelegramNotificationConfig]
	if strings.Contains(raw, token) {
		t.Fatalf("stored config leaked plaintext bot token: %s", raw)
	}
	var stored opsTelegramStoredConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("decode stored config: %v", err)
	}
	firstCiphertext := stored.Templates[0].BotTokenEncrypted
	if firstCiphertext == "" {
		t.Fatal("expected encrypted token")
	}

	got, err = svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates: []OpsTelegramNotificationTemplateUpdate{{
			ID: "alerts", Name: "Alerts", Enabled: true, ChatID: "@sub2api_alerts",
			TopicID: &topicID, BaseURL: opsTelegramDefaultBaseURL,
			DisableNotification: true, ProtectContent: true,
		}},
		OpsAlertTemplateID: "alerts",
	})
	if err != nil {
		t.Fatalf("blank-token update error = %v", err)
	}
	if !got.Templates[0].BotTokenConfigured {
		t.Fatal("blank token should preserve saved token")
	}
	if err := json.Unmarshal([]byte(repo.values[SettingKeyOpsTelegramNotificationConfig]), &stored); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	if stored.Templates[0].BotTokenEncrypted != firstCiphertext {
		t.Fatal("blank token unexpectedly replaced ciphertext")
	}
}

func TestOpsTelegramSavedTokenCannotBeReboundToDifferentBaseURL(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := newOpsTelegramTestService(repo)
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates:          []OpsTelegramNotificationTemplateUpdate{{ID: "alerts", Name: "Alerts", Enabled: true, BotToken: "123456:saved-secret", ChatID: "-1001234567890", BaseURL: opsTelegramDefaultBaseURL}},
		OpsAlertTemplateID: "alerts",
	}); err != nil {
		t.Fatalf("save Telegram config: %v", err)
	}

	attackerBaseURL := "https://attacker.example"
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates:          []OpsTelegramNotificationTemplateUpdate{{ID: "alerts", Name: "Alerts", Enabled: true, ChatID: "-1001234567890", BaseURL: attackerBaseURL}},
		OpsAlertTemplateID: "alerts",
	}); err == nil {
		t.Fatal("expected base URL change with saved token to fail")
	}

	sendCount := 0
	svc.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		sendCount++
		return nil, errors.New("unexpected request")
	})}
	if err := svc.TestTelegramNotification(context.Background(), &OpsTelegramNotificationTestRequest{
		TemplateID: "alerts", ChatID: "-1001234567890", BaseURL: attackerBaseURL,
	}); err == nil {
		t.Fatal("expected test with saved token and changed base URL to fail")
	}
	if sendCount != 0 {
		t.Fatalf("saved token was sent to a changed base URL: sends=%d", sendCount)
	}

	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates:          []OpsTelegramNotificationTemplateUpdate{{ID: "alerts", Name: "Alerts", Enabled: true, BotToken: "654321:replacement-secret", ChatID: "-1001234567890", BaseURL: attackerBaseURL}},
		OpsAlertTemplateID: "alerts",
	}); err != nil {
		t.Fatalf("base URL change with a replacement token should succeed: %v", err)
	}
}

func TestOpsTelegramLoadsLegacyConfigAsTemplate(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	legacy := opsTelegramLegacyStoredConfig{
		Enabled: true, BotTokenEncrypted: "encrypted-token", ChatID: "-1001234567890",
		BaseURL: opsTelegramDefaultBaseURL,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	repo.values[SettingKeyOpsTelegramNotificationConfig] = string(raw)

	config, err := newOpsTelegramTestService(repo).GetTelegramNotificationConfig(context.Background())
	if err != nil {
		t.Fatalf("GetTelegramNotificationConfig() error = %v", err)
	}
	if len(config.Templates) != 1 || config.Templates[0].ID != "legacy-ops-alert" || !config.Templates[0].BotTokenConfigured {
		t.Fatalf("unexpected migrated config: %+v", config)
	}
	if config.OpsAlertTemplateID != "legacy-ops-alert" {
		t.Fatalf("legacy alert template ID = %q", config.OpsAlertTemplateID)
	}
}

func TestUpstreamRateChangeTelegramUsesSelectedTemplate(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := newOpsTelegramTestService(repo)
	topicID := int64(86)
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates: []OpsTelegramNotificationTemplateUpdate{
			{ID: "ops", Name: "Ops", Enabled: true, BotToken: "111:ops-token", ChatID: "-1001", BaseURL: opsTelegramDefaultBaseURL},
			{ID: "rates", Name: "Rates", Enabled: true, BotToken: "222:rate-token", ChatID: "-1002", TopicID: &topicID, BaseURL: opsTelegramDefaultBaseURL},
		},
		OpsAlertTemplateID: "ops", UpstreamRateChangeEnabled: true, UpstreamRateChangeTemplateID: "rates",
	}); err != nil {
		t.Fatalf("save Telegram templates: %v", err)
	}

	svc.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "222:rate-token") {
			t.Fatalf("rate notification used wrong bot: %s", req.URL.Path)
		}
		var payload opsTelegramSendMessageRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ChatID != "-1002" || payload.MessageThreadID == nil || *payload.MessageThreadID != topicID {
			t.Fatalf("rate notification used wrong target: %+v", payload)
		}
		if payload.ParseMode != opsTelegramParseModeMarkdownV2 ||
			!strings.Contains(payload.Text, "> *原倍率:* 0\\.5x") ||
			!strings.Contains(payload.Text, "> *新倍率:* 0\\.8x") ||
			!strings.Contains(payload.Text, "> *来源:* NewAPI 倍率同步") {
			t.Fatalf("rate notification missing change details: %q", payload.Text)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}

	sent, err := svc.sendUpstreamRateChangeTelegram(context.Background(), &Account{ID: 9, Name: "Upstream A"}, 0.5, 0.8, "NewAPI ratio sync")
	if err != nil || !sent {
		t.Fatalf("sendUpstreamRateChangeTelegram() sent=%v err=%v", sent, err)
	}
}

func TestUpstreamBalanceLowTelegramUsesSelectedTemplate(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := newOpsTelegramTestService(repo)
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates: []OpsTelegramNotificationTemplateUpdate{
			{ID: "balance", Name: "Balance", Enabled: true, BotToken: "333:balance-token", ChatID: "-1003", BaseURL: opsTelegramDefaultBaseURL},
		},
		UpstreamBalanceLowEnabled: true, UpstreamBalanceLowTemplateID: "balance",
	}); err != nil {
		t.Fatalf("save Telegram template: %v", err)
	}

	svc.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "333:balance-token") {
			t.Fatalf("balance notification used wrong bot: %s", req.URL.Path)
		}
		var payload opsTelegramSendMessageRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.ChatID != "-1003" ||
			payload.ParseMode != opsTelegramParseModeMarkdownV2 ||
			!strings.Contains(payload.Text, "> *当前余额:* $12\\.5") ||
			!strings.Contains(payload.Text, "> *提醒阈值:* $20") {
			t.Fatalf("balance notification missing details: %+v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})}

	sent, err := svc.sendUpstreamBalanceLowTelegram(context.Background(), &Account{ID: 9, Name: "Upstream A"}, 12.5, 20)
	if err != nil || !sent {
		t.Fatalf("sendUpstreamBalanceLowTelegram() sent=%v err=%v", sent, err)
	}
}

func TestSendOpsTelegramMessagePayloadAndTokenSafeErrors(t *testing.T) {
	topicID := int64(77)
	cfg := opsTelegramDeliveryConfig{
		BotToken:            "123456:secret-value",
		ChatID:              "-1001234567890",
		TopicID:             &topicID,
		BaseURL:             "https://telegram.example",
		DisableNotification: true,
		ProtectContent:      true,
	}

	client := &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/bot123456:secret-value/sendMessage" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.Path)
		}
		var payload opsTelegramSendMessageRequest
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload.ChatID != cfg.ChatID || payload.Text != "test message" ||
			payload.ParseMode != opsTelegramParseModeMarkdownV2 ||
			payload.MessageThreadID == nil || *payload.MessageThreadID != topicID {
			t.Fatalf("unexpected payload: %+v", payload)
		}
		if !payload.DisableNotification || !payload.ProtectContent {
			t.Fatalf("missing Telegram delivery flags: %+v", payload)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     make(http.Header),
		}, nil
	})}
	if err := sendOpsTelegramMessage(context.Background(), client, cfg, "test message"); err != nil {
		t.Fatalf("sendOpsTelegramMessage() error = %v", err)
	}

	leakingClient := &http.Client{Transport: opsTelegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("failed request to " + req.URL.String())
	})}
	err := sendOpsTelegramMessage(context.Background(), leakingClient, cfg, "test message")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), cfg.BotToken) {
		t.Fatalf("transport error leaked bot token: %v", err)
	}

	rejectedClient := &http.Client{Transport: opsTelegramRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"123456:secret-value"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	err = sendOpsTelegramMessage(context.Background(), rejectedClient, cfg, "test message")
	if err == nil || strings.Contains(err.Error(), cfg.BotToken) {
		t.Fatalf("Telegram API error was not token-safe: %v", err)
	}
}

func TestOpsTelegramMarkdownV2FormattingEscapesDynamicValues(t *testing.T) {
	account := &Account{ID: 9, Name: "Prod_[US] (primary) #1!"}
	rateText := buildUpstreamRateChangeTelegramText(account, 0.5, 0.8, "NewAPI (ratio_sync)")
	for _, expected := range []string{
		"🟠 *Sub2API 上游倍率升高*",
		"> *账号:* Prod\\_\\[US\\] \\(primary\\) \\#1\\!（\\#9）",
		"> *来源:* NewAPI \\(ratio\\_sync\\)",
		"> *原倍率:* 0\\.5x",
		"> *新倍率:* 0\\.8x",
	} {
		if !strings.Contains(rateText, expected) {
			t.Fatalf("rate text missing %q:\n%s", expected, rateText)
		}
	}

	balanceText := buildUpstreamBalanceLowTelegramText(account, 4.95, 5)
	for _, expected := range []string{
		"🔴 *Sub2API 上游余额不足*",
		"> *当前余额:* $4\\.95",
		"🟠 *需要处理*",
	} {
		if !strings.Contains(balanceText, expected) {
			t.Fatalf("balance text missing %q:\n%s", expected, balanceText)
		}
	}

	testText := buildOpsTelegramTestText(time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC))
	if !strings.Contains(testText, "✅ *Sub2API Telegram 通知测试*") ||
		!strings.Contains(testText, "> *状态:* 通知发送成功") ||
		!strings.Contains(testText, "> *时间:* 2026\\-07\\-31 09:02:03 北京时间") {
		t.Fatalf("unexpected Chinese test notification:\n%s", testText)
	}
}

func TestOpsTelegramAlertFormattingUsesSeverityAndRecoveryEmoji(t *testing.T) {
	rule := &OpsAlertRule{
		Name:        "Error_rate [gateway]",
		Description: "Failure ratio > 5%!",
		Severity:    "critical",
		MetricType:  "error_rate",
		Operator:    ">",
		Threshold:   5,
	}
	firing := &OpsAlertEvent{
		Status:      OpsAlertStatusFiring,
		Severity:    "critical",
		Description: rule.Description,
		FiredAt:     time.Date(2026, 7, 31, 1, 2, 3, 0, time.UTC),
	}
	firingText := buildOpsTelegramAlertText(rule, firing)
	if !strings.HasPrefix(firingText, "🔴 *Sub2API 运维告警*") ||
		!strings.Contains(firingText, "Error\\_rate \\[gateway\\]") ||
		!strings.Contains(firingText, "> *级别:* 严重") ||
		!strings.Contains(firingText, "> *状态:* 告警中") ||
		!strings.Contains(firingText, "> *指标:* 错误率 高于 5") ||
		!strings.Contains(firingText, "> *触发时间:* 2026\\-07\\-31 09:02:03 北京时间") ||
		!strings.Contains(firingText, "Failure ratio \\> 5%\\!") {
		t.Fatalf("unexpected firing alert MarkdownV2:\n%s", firingText)
	}

	resolved := *firing
	resolved.Status = OpsAlertStatusResolved
	if text := buildOpsTelegramAlertText(rule, &resolved); !strings.HasPrefix(text, "✅ *Sub2API 运维告警*") ||
		!strings.Contains(text, "> *状态:* 已恢复") {
		t.Fatalf("resolved alert should use success emoji:\n%s", text)
	}
}

func TestValidateOpsTelegramBaseURLRejectsUnsafeOrigins(t *testing.T) {
	unsafe := []string{
		"http://api.telegram.org",
		"https://localhost",
		"https://127.0.0.1",
		"https://169.254.169.254",
		"https://198.18.0.1",
		"https://metadata.google.internal",
		"https://user:pass@example.com",
		"https://example.com/proxy",
		"https://example.com?target=telegram",
	}
	for _, raw := range unsafe {
		t.Run(raw, func(t *testing.T) {
			if err := validateOpsTelegramBaseURL(raw); err == nil {
				t.Fatalf("validateOpsTelegramBaseURL(%q) unexpectedly succeeded", raw)
			}
		})
	}
	if err := validateOpsTelegramBaseURL(opsTelegramDefaultBaseURL); err != nil {
		t.Fatalf("default Telegram base URL rejected: %v", err)
	}
}

func TestOpsAlertTelegramUsesExistingNotificationGate(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := newOpsTelegramTestService(repo)
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Templates:          []OpsTelegramNotificationTemplateUpdate{{ID: "alerts", Name: "Alerts", Enabled: true, BotToken: "123456:secret-value", ChatID: "-1001234567890", BaseURL: opsTelegramDefaultBaseURL}},
		OpsAlertTemplateID: "alerts",
	}); err != nil {
		t.Fatalf("save Telegram config: %v", err)
	}

	sendCount := 0
	svc.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		sendCount++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Header:     make(http.Header),
		}, nil
	})}
	evaluator := NewOpsAlertEvaluatorService(svc, &opsRepoMock{}, nil, nil, nil, nil)
	event := &OpsAlertEvent{ID: 1, Status: OpsAlertStatusFiring, FiredAt: time.Now().UTC()}
	rule := &OpsAlertRule{ID: 1, Name: "Error rate", Severity: "critical", MetricType: "error_rate", Operator: ">", Threshold: 5}

	if evaluator.maybeSendAlertTelegram(context.Background(), nil, rule, event) {
		t.Fatal("notify_email=false should disable all alert notification channels")
	}
	if sendCount != 0 {
		t.Fatalf("unexpected Telegram sends with notification gate disabled: %d", sendCount)
	}

	rule.NotifyEmail = true
	if !evaluator.maybeSendAlertTelegram(context.Background(), nil, rule, event) {
		t.Fatal("expected Telegram notification")
	}
	if sendCount != 1 {
		t.Fatalf("Telegram send count = %d, want 1", sendCount)
	}
}
