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
		Enabled:             true,
		BotToken:            token,
		ChatID:              "-1001234567890",
		TopicID:             &topicID,
		BaseURL:             "https://api.telegram.org/",
		DisableNotification: true,
		ProtectContent:      true,
	})
	if err != nil {
		t.Fatalf("UpdateTelegramNotificationConfig() error = %v", err)
	}
	if !got.BotTokenConfigured || got.BaseURL != opsTelegramDefaultBaseURL {
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
	firstCiphertext := stored.BotTokenEncrypted
	if firstCiphertext == "" {
		t.Fatal("expected encrypted token")
	}

	got, err = svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Enabled:             true,
		ChatID:              "@sub2api_alerts",
		TopicID:             &topicID,
		BaseURL:             opsTelegramDefaultBaseURL,
		DisableNotification: true,
		ProtectContent:      true,
	})
	if err != nil {
		t.Fatalf("blank-token update error = %v", err)
	}
	if !got.BotTokenConfigured {
		t.Fatal("blank token should preserve saved token")
	}
	if err := json.Unmarshal([]byte(repo.values[SettingKeyOpsTelegramNotificationConfig]), &stored); err != nil {
		t.Fatalf("decode updated config: %v", err)
	}
	if stored.BotTokenEncrypted != firstCiphertext {
		t.Fatal("blank token unexpectedly replaced ciphertext")
	}
}

func TestOpsTelegramSavedTokenCannotBeReboundToDifferentBaseURL(t *testing.T) {
	repo := newRuntimeSettingRepoStub()
	svc := newOpsTelegramTestService(repo)
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Enabled:  true,
		BotToken: "123456:saved-secret",
		ChatID:   "-1001234567890",
		BaseURL:  opsTelegramDefaultBaseURL,
	}); err != nil {
		t.Fatalf("save Telegram config: %v", err)
	}

	attackerBaseURL := "https://attacker.example"
	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Enabled: true,
		ChatID:  "-1001234567890",
		BaseURL: attackerBaseURL,
	}); err == nil {
		t.Fatal("expected base URL change with saved token to fail")
	}

	sendCount := 0
	svc.telegramClient = &http.Client{Transport: opsTelegramRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		sendCount++
		return nil, errors.New("unexpected request")
	})}
	if err := svc.TestTelegramNotification(context.Background(), &OpsTelegramNotificationTestRequest{
		ChatID:  "-1001234567890",
		BaseURL: attackerBaseURL,
	}); err == nil {
		t.Fatal("expected test with saved token and changed base URL to fail")
	}
	if sendCount != 0 {
		t.Fatalf("saved token was sent to a changed base URL: sends=%d", sendCount)
	}

	if _, err := svc.UpdateTelegramNotificationConfig(context.Background(), &OpsTelegramNotificationConfigUpdateRequest{
		Enabled:  true,
		BotToken: "654321:replacement-secret",
		ChatID:   "-1001234567890",
		BaseURL:  attackerBaseURL,
	}); err != nil {
		t.Fatalf("base URL change with a replacement token should succeed: %v", err)
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
		if payload.ChatID != cfg.ChatID || payload.Text != "test message" || payload.MessageThreadID == nil || *payload.MessageThreadID != topicID {
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
		Enabled:  true,
		BotToken: "123456:secret-value",
		ChatID:   "-1001234567890",
		BaseURL:  opsTelegramDefaultBaseURL,
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
