package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	opsTelegramDefaultBaseURL      = "https://api.telegram.org"
	opsTelegramRequestTimeout      = 10 * time.Second
	opsTelegramMaxResponseBodySize = 8 * 1024
	opsTelegramMaxMessageBytes     = 4000
)

var opsTelegramHTTPClient = func() *http.Client {
	client := newSSRFSafeHTTPClient(opsTelegramRequestTimeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}()

type opsTelegramStoredConfig struct {
	Enabled             bool   `json:"enabled"`
	BotTokenEncrypted   string `json:"bot_token_encrypted,omitempty"`
	ChatID              string `json:"chat_id"`
	TopicID             *int64 `json:"topic_id"`
	BaseURL             string `json:"base_url"`
	DisableNotification bool   `json:"disable_notification"`
	ProtectContent      bool   `json:"protect_content"`
}

type opsTelegramDeliveryConfig struct {
	BotToken            string
	ChatID              string
	TopicID             *int64
	BaseURL             string
	DisableNotification bool
	ProtectContent      bool
}

type opsTelegramSendMessageRequest struct {
	ChatID              string `json:"chat_id"`
	Text                string `json:"text"`
	MessageThreadID     *int64 `json:"message_thread_id,omitempty"`
	DisableNotification bool   `json:"disable_notification,omitempty"`
	ProtectContent      bool   `json:"protect_content,omitempty"`
}

type opsTelegramAPIResponse struct {
	OK bool `json:"ok"`
}

func defaultOpsTelegramStoredConfig() *opsTelegramStoredConfig {
	return &opsTelegramStoredConfig{BaseURL: opsTelegramDefaultBaseURL}
}

func normalizeOpsTelegramStoredConfig(cfg *opsTelegramStoredConfig) {
	if cfg == nil {
		return
	}
	cfg.BotTokenEncrypted = strings.TrimSpace(cfg.BotTokenEncrypted)
	cfg.ChatID = strings.TrimSpace(cfg.ChatID)
	cfg.BaseURL = normalizeOpsTelegramBaseURL(cfg.BaseURL)
	if cfg.BaseURL == "" {
		cfg.BaseURL = opsTelegramDefaultBaseURL
	}
}

func (s *OpsService) loadOpsTelegramStoredConfig(ctx context.Context) (*opsTelegramStoredConfig, error) {
	defaultCfg := defaultOpsTelegramStoredConfig()
	if s == nil || s.settingRepo == nil {
		return defaultCfg, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOpsTelegramNotificationConfig)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaultCfg, nil
		}
		return nil, infraerrors.InternalServer("OPS_TELEGRAM_CONFIG_LOAD_FAILED", "failed to load Telegram notification config")
	}

	cfg := &opsTelegramStoredConfig{}
	if err := json.Unmarshal([]byte(raw), cfg); err != nil {
		return nil, infraerrors.InternalServer("OPS_TELEGRAM_CONFIG_CORRUPT", "Telegram notification config is corrupted")
	}
	normalizeOpsTelegramStoredConfig(cfg)
	return cfg, nil
}

func telegramPublicConfig(cfg *opsTelegramStoredConfig) *OpsTelegramNotificationConfig {
	if cfg == nil {
		cfg = defaultOpsTelegramStoredConfig()
	}
	return &OpsTelegramNotificationConfig{
		Enabled:             cfg.Enabled,
		BotTokenConfigured:  strings.TrimSpace(cfg.BotTokenEncrypted) != "",
		ChatID:              cfg.ChatID,
		TopicID:             cfg.TopicID,
		BaseURL:             cfg.BaseURL,
		DisableNotification: cfg.DisableNotification,
		ProtectContent:      cfg.ProtectContent,
	}
}

// GetTelegramNotificationConfig returns a redacted config. The bot token is
// represented only by BotTokenConfigured and never leaves the service.
func (s *OpsService) GetTelegramNotificationConfig(ctx context.Context) (*OpsTelegramNotificationConfig, error) {
	cfg, err := s.loadOpsTelegramStoredConfig(ctx)
	if err != nil {
		return nil, err
	}
	return telegramPublicConfig(cfg), nil
}

// UpdateTelegramNotificationConfig encrypts new tokens before writing the
// settings row. An empty token preserves the stored ciphertext.
func (s *OpsService) UpdateTelegramNotificationConfig(ctx context.Context, req *OpsTelegramNotificationConfigUpdateRequest) (*OpsTelegramNotificationConfig, error) {
	if s == nil || s.settingRepo == nil {
		return nil, infraerrors.InternalServer("OPS_TELEGRAM_CONFIG_UNAVAILABLE", "Telegram notification config is unavailable")
	}
	if req == nil {
		return nil, opsTelegramConfigError("invalid request")
	}

	stored, err := s.loadOpsTelegramStoredConfig(ctx)
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(req.BotToken)
	if req.ClearBotToken && token != "" {
		return nil, opsTelegramConfigError("bot_token and clear_bot_token cannot be used together")
	}
	if token != "" {
		if err := validateOpsTelegramBotToken(token); err != nil {
			return nil, err
		}
	}

	candidate := &opsTelegramStoredConfig{
		Enabled:             req.Enabled,
		BotTokenEncrypted:   stored.BotTokenEncrypted,
		ChatID:              strings.TrimSpace(req.ChatID),
		TopicID:             req.TopicID,
		BaseURL:             normalizeOpsTelegramBaseURL(req.BaseURL),
		DisableNotification: req.DisableNotification,
		ProtectContent:      req.ProtectContent,
	}
	if candidate.BaseURL == "" {
		candidate.BaseURL = opsTelegramDefaultBaseURL
	}
	if req.ClearBotToken {
		candidate.BotTokenEncrypted = ""
	}
	if token == "" && !req.ClearBotToken && stored.BotTokenEncrypted != "" && !sameOpsTelegramBaseURL(candidate.BaseURL, stored.BaseURL) {
		return nil, opsTelegramConfigError("base_url cannot be changed while reusing the saved Telegram bot token; enter the token again")
	}
	tokenConfigured := candidate.BotTokenEncrypted != "" || token != ""
	if err := validateOpsTelegramConfig(candidate, tokenConfigured, candidate.Enabled); err != nil {
		return nil, err
	}

	if token != "" {
		if s.encryptor == nil || s.cfg == nil || !s.cfg.Totp.EncryptionKeyConfigured {
			return nil, infraerrors.BadRequest(
				"OPS_TELEGRAM_ENCRYPTION_KEY_NOT_CONFIGURED",
				"cannot store the Telegram bot token until a fixed TOTP_ENCRYPTION_KEY is configured",
			)
		}
		encrypted, err := s.encryptor.Encrypt(token)
		if err != nil {
			return nil, infraerrors.InternalServer("OPS_TELEGRAM_TOKEN_ENCRYPT_FAILED", "failed to encrypt Telegram bot token")
		}
		candidate.BotTokenEncrypted = encrypted
	}

	raw, err := json.Marshal(candidate)
	if err != nil {
		return nil, infraerrors.InternalServer("OPS_TELEGRAM_CONFIG_ENCODE_FAILED", "failed to encode Telegram notification config")
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpsTelegramNotificationConfig, string(raw)); err != nil {
		return nil, infraerrors.InternalServer("OPS_TELEGRAM_CONFIG_SAVE_FAILED", "failed to save Telegram notification config")
	}
	return telegramPublicConfig(candidate), nil
}

// TestTelegramNotification sends the current form values without saving them.
// A blank token reuses and decrypts the saved token.
func (s *OpsService) TestTelegramNotification(ctx context.Context, req *OpsTelegramNotificationTestRequest) error {
	if req == nil {
		return opsTelegramConfigError("invalid request")
	}
	baseURL := normalizeOpsTelegramBaseURL(req.BaseURL)
	if baseURL == "" {
		baseURL = opsTelegramDefaultBaseURL
	}
	token := strings.TrimSpace(req.BotToken)
	if token == "" {
		stored, err := s.loadOpsTelegramStoredConfig(ctx)
		if err != nil {
			return err
		}
		if stored.BotTokenEncrypted != "" && !sameOpsTelegramBaseURL(baseURL, stored.BaseURL) {
			return opsTelegramConfigError("base_url cannot be changed while reusing the saved Telegram bot token; enter the token again")
		}
		token, err = s.decryptOpsTelegramBotToken(stored.BotTokenEncrypted)
		if err != nil {
			return err
		}
	}

	delivery := opsTelegramDeliveryConfig{
		BotToken:            token,
		ChatID:              strings.TrimSpace(req.ChatID),
		TopicID:             req.TopicID,
		BaseURL:             baseURL,
		DisableNotification: req.DisableNotification,
		ProtectContent:      req.ProtectContent,
	}
	if err := validateOpsTelegramDeliveryConfig(delivery); err != nil {
		return err
	}

	message := "✅ Sub2API Telegram notification test\nTime: " + time.Now().UTC().Format(time.RFC3339)
	return sendOpsTelegramMessage(ctx, s.opsTelegramClient(), delivery, message)
}

func (s *OpsService) sendOpsTelegramAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent) (bool, error) {
	stored, err := s.loadOpsTelegramStoredConfig(ctx)
	if err != nil {
		return false, err
	}
	if !stored.Enabled {
		return false, nil
	}
	token, err := s.decryptOpsTelegramBotToken(stored.BotTokenEncrypted)
	if err != nil {
		return false, err
	}
	delivery := opsTelegramDeliveryConfig{
		BotToken:            token,
		ChatID:              stored.ChatID,
		TopicID:             stored.TopicID,
		BaseURL:             stored.BaseURL,
		DisableNotification: stored.DisableNotification,
		ProtectContent:      stored.ProtectContent,
	}
	if err := validateOpsTelegramDeliveryConfig(delivery); err != nil {
		return false, err
	}
	if err := sendOpsTelegramMessage(ctx, s.opsTelegramClient(), delivery, buildOpsTelegramAlertText(rule, event)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *OpsService) decryptOpsTelegramBotToken(ciphertext string) (string, error) {
	if strings.TrimSpace(ciphertext) == "" {
		return "", opsTelegramConfigError("Telegram bot token is required")
	}
	if s == nil || s.encryptor == nil {
		return "", infraerrors.InternalServer("OPS_TELEGRAM_TOKEN_DECRYPT_FAILED", "failed to decrypt Telegram bot token")
	}
	plaintext, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", infraerrors.InternalServer("OPS_TELEGRAM_TOKEN_DECRYPT_FAILED", "failed to decrypt Telegram bot token")
	}
	plaintext = strings.TrimSpace(plaintext)
	if err := validateOpsTelegramBotToken(plaintext); err != nil {
		return "", infraerrors.InternalServer("OPS_TELEGRAM_TOKEN_INVALID", "stored Telegram bot token is invalid")
	}
	return plaintext, nil
}

func (s *OpsService) opsTelegramClient() *http.Client {
	if s != nil && s.telegramClient != nil {
		return s.telegramClient
	}
	return opsTelegramHTTPClient
}

func validateOpsTelegramConfig(cfg *opsTelegramStoredConfig, tokenConfigured, requireDelivery bool) error {
	if cfg == nil {
		return opsTelegramConfigError("invalid config")
	}
	if err := validateOpsTelegramBaseURL(cfg.BaseURL); err != nil {
		return err
	}
	if cfg.ChatID != "" {
		if err := validateOpsTelegramChatID(cfg.ChatID); err != nil {
			return err
		}
	}
	if cfg.TopicID != nil && *cfg.TopicID <= 0 {
		return opsTelegramConfigError("topic_id must be a positive integer")
	}
	if requireDelivery {
		if !tokenConfigured {
			return opsTelegramConfigError("Telegram bot token is required when Telegram notifications are enabled")
		}
		if cfg.ChatID == "" {
			return opsTelegramConfigError("chat_id is required when Telegram notifications are enabled")
		}
	}
	return nil
}

func validateOpsTelegramDeliveryConfig(cfg opsTelegramDeliveryConfig) error {
	if err := validateOpsTelegramBotToken(cfg.BotToken); err != nil {
		return err
	}
	if err := validateOpsTelegramChatID(cfg.ChatID); err != nil {
		return err
	}
	if cfg.TopicID != nil && *cfg.TopicID <= 0 {
		return opsTelegramConfigError("topic_id must be a positive integer")
	}
	return validateOpsTelegramBaseURL(cfg.BaseURL)
}

func validateOpsTelegramBotToken(token string) error {
	token = strings.TrimSpace(token)
	if len(token) < 3 || len(token) > 256 || !strings.Contains(token, ":") {
		return opsTelegramConfigError("Telegram bot token is invalid")
	}
	if strings.ContainsAny(token, "/?#") || strings.IndexFunc(token, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return opsTelegramConfigError("Telegram bot token is invalid")
	}
	return nil
}

func validateOpsTelegramChatID(chatID string) error {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return opsTelegramConfigError("chat_id is required")
	}
	if len(chatID) > 256 || strings.IndexFunc(chatID, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return opsTelegramConfigError("chat_id is invalid")
	}
	return nil
}

func validateOpsTelegramBaseURL(raw string) error {
	raw = normalizeOpsTelegramBaseURL(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return opsTelegramConfigError("base_url must be a public HTTPS origin")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return opsTelegramConfigError("base_url must be a public HTTPS origin without a path, query, or fragment")
	}
	hostname := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if isBlockedHostname(hostname) {
		return opsTelegramConfigError("base_url must use a public host")
	}
	if ip := net.ParseIP(hostname); ip != nil && isPrivateIP(ip) {
		return opsTelegramConfigError("base_url must use a public host")
	}
	return nil
}

func normalizeOpsTelegramBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func sameOpsTelegramBaseURL(left, right string) bool {
	left = normalizeOpsTelegramBaseURL(left)
	right = normalizeOpsTelegramBaseURL(right)
	if left == "" {
		left = opsTelegramDefaultBaseURL
	}
	if right == "" {
		right = opsTelegramDefaultBaseURL
	}
	return strings.EqualFold(left, right)
}

func sendOpsTelegramMessage(ctx context.Context, client *http.Client, cfg opsTelegramDeliveryConfig, text string) error {
	if err := validateOpsTelegramDeliveryConfig(cfg); err != nil {
		return err
	}
	if client == nil {
		return opsTelegramDeliveryError()
	}
	payload, err := json.Marshal(opsTelegramSendMessageRequest{
		ChatID:              cfg.ChatID,
		Text:                truncateString(strings.TrimSpace(text), opsTelegramMaxMessageBytes),
		MessageThreadID:     cfg.TopicID,
		DisableNotification: cfg.DisableNotification,
		ProtectContent:      cfg.ProtectContent,
	})
	if err != nil {
		return opsTelegramDeliveryError()
	}

	endpoint := normalizeOpsTelegramBaseURL(cfg.BaseURL) + "/bot" + cfg.BotToken + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return opsTelegramDeliveryError()
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		// url.Error contains the full request URL, which embeds the bot token.
		// Never wrap or expose the transport error.
		return opsTelegramDeliveryError()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, opsTelegramMaxResponseBodySize+1))
	if err != nil || len(body) > opsTelegramMaxResponseBodySize {
		return opsTelegramDeliveryError()
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return opsTelegramDeliveryError()
	}
	var result opsTelegramAPIResponse
	if err := json.Unmarshal(body, &result); err != nil || !result.OK {
		return opsTelegramDeliveryError()
	}
	return nil
}

func opsTelegramConfigError(message string) error {
	return infraerrors.BadRequest("OPS_TELEGRAM_INVALID_CONFIG", message)
}

func opsTelegramDeliveryError() error {
	return infraerrors.New(http.StatusBadGateway, "OPS_TELEGRAM_DELIVERY_FAILED", "Telegram notification delivery failed")
}

func buildOpsTelegramAlertText(rule *OpsAlertRule, event *OpsAlertEvent) string {
	values := opsAlertEmailVariables(rule, event)
	text := fmt.Sprintf(
		"🚨 Sub2API Ops Alert\nRule: %s\nSeverity: %s\nStatus: %s\nMetric: %s %s %s\nCurrent value: %s\nFired at: %s\nDescription: %s",
		values["rule_name"],
		values["severity"],
		values["alert_status"],
		values["metric_type"],
		values["operator"],
		values["threshold_value"],
		values["metric_value"],
		values["triggered_at"],
		values["alert_description"],
	)
	return truncateString(text, opsTelegramMaxMessageBytes)
}
