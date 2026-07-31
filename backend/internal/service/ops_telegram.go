package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	opsTelegramDefaultBaseURL      = "https://api.telegram.org"
	opsTelegramRequestTimeout      = 10 * time.Second
	opsTelegramMaxResponseBodySize = 8 * 1024
	opsTelegramMaxMessageBytes     = 4000
	opsTelegramMaxTemplates        = 50
	opsTelegramMaxTemplateName     = 100
	opsTelegramMaxTemplateID       = 128
)

var opsTelegramHTTPClient = func() *http.Client {
	client := newSSRFSafeHTTPClient(opsTelegramRequestTimeout)
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client
}()

type opsTelegramStoredConfig struct {
	Version                      int                         `json:"version"`
	Templates                    []opsTelegramStoredTemplate `json:"templates"`
	OpsAlertTemplateID           string                      `json:"ops_alert_template_id,omitempty"`
	UpstreamRateChangeEnabled    bool                        `json:"upstream_rate_change_enabled"`
	UpstreamRateChangeTemplateID string                      `json:"upstream_rate_change_template_id,omitempty"`
	UpstreamBalanceLowEnabled    bool                        `json:"upstream_balance_low_enabled"`
	UpstreamBalanceLowTemplateID string                      `json:"upstream_balance_low_template_id,omitempty"`
}

type opsTelegramStoredTemplate struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Enabled             bool   `json:"enabled"`
	BotTokenEncrypted   string `json:"bot_token_encrypted,omitempty"`
	ChatID              string `json:"chat_id"`
	TopicID             *int64 `json:"topic_id"`
	BaseURL             string `json:"base_url"`
	DisableNotification bool   `json:"disable_notification"`
	ProtectContent      bool   `json:"protect_content"`
}

type opsTelegramLegacyStoredConfig struct {
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
	return &opsTelegramStoredConfig{Version: 2, Templates: []opsTelegramStoredTemplate{}}
}

func normalizeOpsTelegramStoredConfig(cfg *opsTelegramStoredConfig) {
	if cfg == nil {
		return
	}
	cfg.Version = 2
	cfg.OpsAlertTemplateID = strings.TrimSpace(cfg.OpsAlertTemplateID)
	cfg.UpstreamRateChangeTemplateID = strings.TrimSpace(cfg.UpstreamRateChangeTemplateID)
	cfg.UpstreamBalanceLowTemplateID = strings.TrimSpace(cfg.UpstreamBalanceLowTemplateID)
	if cfg.Templates == nil {
		cfg.Templates = []opsTelegramStoredTemplate{}
	}
	for i := range cfg.Templates {
		template := &cfg.Templates[i]
		template.ID = strings.TrimSpace(template.ID)
		template.Name = strings.TrimSpace(template.Name)
		template.BotTokenEncrypted = strings.TrimSpace(template.BotTokenEncrypted)
		template.ChatID = strings.TrimSpace(template.ChatID)
		template.BaseURL = normalizeOpsTelegramBaseURL(template.BaseURL)
		if template.BaseURL == "" {
			template.BaseURL = opsTelegramDefaultBaseURL
		}
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
	if cfg.Version == 0 && cfg.Templates == nil {
		legacy := &opsTelegramLegacyStoredConfig{}
		if err := json.Unmarshal([]byte(raw), legacy); err != nil {
			return nil, infraerrors.InternalServer("OPS_TELEGRAM_CONFIG_CORRUPT", "Telegram notification config is corrupted")
		}
		legacyID := "legacy-ops-alert"
		cfg = defaultOpsTelegramStoredConfig()
		if strings.TrimSpace(legacy.BotTokenEncrypted) != "" || strings.TrimSpace(legacy.ChatID) != "" {
			cfg.Templates = append(cfg.Templates, opsTelegramStoredTemplate{
				ID: legacyID, Name: "Ops alerts", Enabled: legacy.Enabled,
				BotTokenEncrypted: legacy.BotTokenEncrypted, ChatID: legacy.ChatID,
				TopicID: legacy.TopicID, BaseURL: legacy.BaseURL,
				DisableNotification: legacy.DisableNotification, ProtectContent: legacy.ProtectContent,
			})
			if legacy.Enabled {
				cfg.OpsAlertTemplateID = legacyID
			}
		}
	}
	normalizeOpsTelegramStoredConfig(cfg)
	return cfg, nil
}

func telegramPublicConfig(cfg *opsTelegramStoredConfig) *OpsTelegramNotificationConfig {
	if cfg == nil {
		cfg = defaultOpsTelegramStoredConfig()
	}
	result := &OpsTelegramNotificationConfig{
		Templates:                    make([]OpsTelegramNotificationTemplate, 0, len(cfg.Templates)),
		OpsAlertTemplateID:           cfg.OpsAlertTemplateID,
		UpstreamRateChangeEnabled:    cfg.UpstreamRateChangeEnabled,
		UpstreamRateChangeTemplateID: cfg.UpstreamRateChangeTemplateID,
		UpstreamBalanceLowEnabled:    cfg.UpstreamBalanceLowEnabled,
		UpstreamBalanceLowTemplateID: cfg.UpstreamBalanceLowTemplateID,
	}
	for _, template := range cfg.Templates {
		result.Templates = append(result.Templates, OpsTelegramNotificationTemplate{
			ID: template.ID, Name: template.Name, Enabled: template.Enabled,
			BotTokenConfigured: strings.TrimSpace(template.BotTokenEncrypted) != "",
			ChatID:             template.ChatID, TopicID: template.TopicID, BaseURL: template.BaseURL,
			DisableNotification: template.DisableNotification, ProtectContent: template.ProtectContent,
		})
	}
	return result
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
	storedByID := make(map[string]opsTelegramStoredTemplate, len(stored.Templates))
	for _, template := range stored.Templates {
		storedByID[template.ID] = template
	}
	candidate := defaultOpsTelegramStoredConfig()
	if len(req.Templates) > opsTelegramMaxTemplates {
		return nil, opsTelegramConfigError("too many Telegram templates")
	}
	seen := make(map[string]struct{}, len(req.Templates))
	for _, input := range req.Templates {
		id := strings.TrimSpace(input.ID)
		if id == "" {
			id = uuid.NewString()
		}
		if len(id) > opsTelegramMaxTemplateID || strings.IndexFunc(id, unicode.IsControl) >= 0 {
			return nil, opsTelegramConfigError("template ID is invalid")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, opsTelegramConfigError("template IDs must be unique")
		}
		seen[id] = struct{}{}
		previous := storedByID[id]
		token := strings.TrimSpace(input.BotToken)
		if input.ClearBotToken && token != "" {
			return nil, opsTelegramConfigError("bot_token and clear_bot_token cannot be used together")
		}
		template := opsTelegramStoredTemplate{
			ID: id, Name: strings.TrimSpace(input.Name), Enabled: input.Enabled,
			BotTokenEncrypted: previous.BotTokenEncrypted,
			ChatID:            strings.TrimSpace(input.ChatID), TopicID: input.TopicID,
			BaseURL:             normalizeOpsTelegramBaseURL(input.BaseURL),
			DisableNotification: input.DisableNotification, ProtectContent: input.ProtectContent,
		}
		if template.Name == "" {
			return nil, opsTelegramConfigError("template name is required")
		}
		if len(template.Name) > opsTelegramMaxTemplateName || strings.IndexFunc(template.Name, unicode.IsControl) >= 0 {
			return nil, opsTelegramConfigError("template name is invalid")
		}
		if template.BaseURL == "" {
			template.BaseURL = opsTelegramDefaultBaseURL
		}
		if input.ClearBotToken {
			template.BotTokenEncrypted = ""
		}
		if token == "" && !input.ClearBotToken && previous.BotTokenEncrypted != "" && !sameOpsTelegramBaseURL(template.BaseURL, previous.BaseURL) {
			return nil, opsTelegramConfigError("base_url cannot be changed while reusing a saved Telegram bot token; enter the token again")
		}
		if token != "" {
			if err := validateOpsTelegramBotToken(token); err != nil {
				return nil, err
			}
			if s.encryptor == nil || s.cfg == nil || !s.cfg.Totp.EncryptionKeyConfigured {
				return nil, infraerrors.BadRequest("OPS_TELEGRAM_ENCRYPTION_KEY_NOT_CONFIGURED", "cannot store the Telegram bot token until a fixed TOTP_ENCRYPTION_KEY is configured")
			}
			encrypted, err := s.encryptor.Encrypt(token)
			if err != nil {
				return nil, infraerrors.InternalServer("OPS_TELEGRAM_TOKEN_ENCRYPT_FAILED", "failed to encrypt Telegram bot token")
			}
			template.BotTokenEncrypted = encrypted
		}
		if err := validateOpsTelegramTemplate(&template, template.Enabled); err != nil {
			return nil, err
		}
		candidate.Templates = append(candidate.Templates, template)
	}
	candidate.OpsAlertTemplateID = strings.TrimSpace(req.OpsAlertTemplateID)
	candidate.UpstreamRateChangeEnabled = req.UpstreamRateChangeEnabled
	candidate.UpstreamRateChangeTemplateID = strings.TrimSpace(req.UpstreamRateChangeTemplateID)
	candidate.UpstreamBalanceLowEnabled = req.UpstreamBalanceLowEnabled
	candidate.UpstreamBalanceLowTemplateID = strings.TrimSpace(req.UpstreamBalanceLowTemplateID)
	if err := validateOpsTelegramAssignments(candidate); err != nil {
		return nil, err
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
		template, ok := findOpsTelegramTemplate(stored, strings.TrimSpace(req.TemplateID))
		if !ok {
			return opsTelegramConfigError("saved Telegram template not found")
		}
		if template.BotTokenEncrypted != "" && !sameOpsTelegramBaseURL(baseURL, template.BaseURL) {
			return opsTelegramConfigError("base_url cannot be changed while reusing the saved Telegram bot token; enter the token again")
		}
		token, err = s.decryptOpsTelegramBotToken(template.BotTokenEncrypted)
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
	template, ok := findOpsTelegramTemplate(stored, stored.OpsAlertTemplateID)
	if !ok || !template.Enabled {
		return false, nil
	}
	token, err := s.decryptOpsTelegramBotToken(template.BotTokenEncrypted)
	if err != nil {
		return false, err
	}
	delivery := opsTelegramDeliveryConfig{
		BotToken: token,
		ChatID:   template.ChatID, TopicID: template.TopicID, BaseURL: template.BaseURL,
		DisableNotification: template.DisableNotification, ProtectContent: template.ProtectContent,
	}
	if err := validateOpsTelegramDeliveryConfig(delivery); err != nil {
		return false, err
	}
	if err := sendOpsTelegramMessage(ctx, s.opsTelegramClient(), delivery, buildOpsTelegramAlertText(rule, event)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *OpsService) sendUpstreamRateChangeTelegram(ctx context.Context, account *Account, oldRate, newRate float64, source string) (bool, error) {
	stored, err := s.loadOpsTelegramStoredConfig(ctx)
	if err != nil {
		return false, err
	}
	if !stored.UpstreamRateChangeEnabled {
		return false, nil
	}
	template, ok := findOpsTelegramTemplate(stored, stored.UpstreamRateChangeTemplateID)
	if !ok || !template.Enabled {
		return false, nil
	}
	token, err := s.decryptOpsTelegramBotToken(template.BotTokenEncrypted)
	if err != nil {
		return false, err
	}
	delivery := opsTelegramDeliveryConfig{
		BotToken: token, ChatID: template.ChatID, TopicID: template.TopicID, BaseURL: template.BaseURL,
		DisableNotification: template.DisableNotification, ProtectContent: template.ProtectContent,
	}
	if err := sendOpsTelegramMessage(ctx, s.opsTelegramClient(), delivery, buildUpstreamRateChangeTelegramText(account, oldRate, newRate, source)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *OpsService) notifyUpstreamRateChange(account *Account, oldRate, newRate float64, source string) {
	if s == nil || account == nil || equalBillingMultiplier(oldRate, newRate) {
		return
	}
	accountCopy := *account
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opsTelegramRequestTimeout)
		defer cancel()
		if _, err := s.sendUpstreamRateChangeTelegram(ctx, &accountCopy, oldRate, newRate, source); err != nil {
			slog.Warn("upstream_rate_change_telegram_failed", "account_id", accountCopy.ID, "source", source, "error", err)
		}
	}()
}

func (s *OpsService) sendUpstreamBalanceLowTelegram(ctx context.Context, account *Account, balance, threshold float64) (bool, error) {
	stored, err := s.loadOpsTelegramStoredConfig(ctx)
	if err != nil {
		return false, err
	}
	if !stored.UpstreamBalanceLowEnabled {
		return false, nil
	}
	template, ok := findOpsTelegramTemplate(stored, stored.UpstreamBalanceLowTemplateID)
	if !ok || !template.Enabled {
		return false, nil
	}
	token, err := s.decryptOpsTelegramBotToken(template.BotTokenEncrypted)
	if err != nil {
		return false, err
	}
	delivery := opsTelegramDeliveryConfig{
		BotToken: token, ChatID: template.ChatID, TopicID: template.TopicID, BaseURL: template.BaseURL,
		DisableNotification: template.DisableNotification, ProtectContent: template.ProtectContent,
	}
	if err := sendOpsTelegramMessage(ctx, s.opsTelegramClient(), delivery, buildUpstreamBalanceLowTelegramText(account, balance, threshold)); err != nil {
		return false, err
	}
	return true, nil
}

func (s *OpsService) notifyUpstreamBalanceLow(account *Account, balance, threshold float64) {
	if s == nil || account == nil {
		return
	}
	accountCopy := *account
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), opsTelegramRequestTimeout)
		defer cancel()
		if _, err := s.sendUpstreamBalanceLowTelegram(ctx, &accountCopy, balance, threshold); err != nil {
			slog.Warn("upstream_balance_low_telegram_failed", "account_id", accountCopy.ID, "error", err)
		}
	}()
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

func validateOpsTelegramTemplate(template *opsTelegramStoredTemplate, requireDelivery bool) error {
	if template == nil {
		return opsTelegramConfigError("invalid config")
	}
	if err := validateOpsTelegramBaseURL(template.BaseURL); err != nil {
		return err
	}
	if template.ChatID != "" {
		if err := validateOpsTelegramChatID(template.ChatID); err != nil {
			return err
		}
	}
	if template.TopicID != nil && *template.TopicID <= 0 {
		return opsTelegramConfigError("topic_id must be a positive integer")
	}
	if requireDelivery {
		if strings.TrimSpace(template.BotTokenEncrypted) == "" {
			return opsTelegramConfigError("Telegram bot token is required when a template is enabled")
		}
		if template.ChatID == "" {
			return opsTelegramConfigError("chat_id is required when a Telegram template is enabled")
		}
	}
	return nil
}

func validateOpsTelegramAssignments(cfg *opsTelegramStoredConfig) error {
	if cfg == nil {
		return opsTelegramConfigError("invalid config")
	}
	if cfg.OpsAlertTemplateID != "" {
		if template, ok := findOpsTelegramTemplate(cfg, cfg.OpsAlertTemplateID); !ok || !template.Enabled {
			return opsTelegramConfigError("ops alert template must reference an enabled template")
		}
	}
	if cfg.UpstreamRateChangeEnabled {
		if template, ok := findOpsTelegramTemplate(cfg, cfg.UpstreamRateChangeTemplateID); !ok || !template.Enabled {
			return opsTelegramConfigError("upstream rate change notification must reference an enabled template")
		}
	}
	if cfg.UpstreamBalanceLowEnabled {
		if template, ok := findOpsTelegramTemplate(cfg, cfg.UpstreamBalanceLowTemplateID); !ok || !template.Enabled {
			return opsTelegramConfigError("upstream low balance notification must reference an enabled template")
		}
	}
	return nil
}

func findOpsTelegramTemplate(cfg *opsTelegramStoredConfig, id string) (*opsTelegramStoredTemplate, bool) {
	if cfg == nil || strings.TrimSpace(id) == "" {
		return nil, false
	}
	for i := range cfg.Templates {
		if cfg.Templates[i].ID == id {
			return &cfg.Templates[i], true
		}
	}
	return nil, false
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

func buildUpstreamRateChangeTelegramText(account *Account, oldRate, newRate float64, source string) string {
	accountName := ""
	accountID := int64(0)
	if account != nil {
		accountName = strings.TrimSpace(account.Name)
		accountID = account.ID
	}
	if accountName == "" {
		accountName = fmt.Sprintf("Account #%d", accountID)
	}
	return truncateString(fmt.Sprintf(
		"Sub2API upstream rate changed\nAccount: %s (#%d)\nSource: %s\nPrevious rate: %gx\nNew rate: %gx\nChanged at: %s",
		accountName, accountID, source, oldRate, newRate, time.Now().UTC().Format(time.RFC3339),
	), opsTelegramMaxMessageBytes)
}

func buildUpstreamBalanceLowTelegramText(account *Account, balance, threshold float64) string {
	accountName := ""
	accountID := int64(0)
	if account != nil {
		accountName = strings.TrimSpace(account.Name)
		accountID = account.ID
	}
	if accountName == "" {
		accountName = fmt.Sprintf("Account #%d", accountID)
	}
	return truncateString(fmt.Sprintf(
		"Sub2API upstream balance is low\nAccount: %s (#%d)\nCurrent balance: $%g\nAlert threshold: $%g\nDetected at: %s",
		accountName, accountID, balance, threshold, time.Now().UTC().Format(time.RFC3339),
	), opsTelegramMaxMessageBytes)
}
