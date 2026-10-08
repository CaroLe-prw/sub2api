package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const paymentVerificationSettingKey = "payment_verification_config"

type PaymentVerificationConfig struct {
	Enabled           bool    `json:"enabled"`
	TemplateID        string  `json:"template_id"`
	LookbackDays      int     `json:"lookback_days"`
	IntervalMinutes   int     `json:"interval_minutes"`
	AdminTelegramIDs  []int64 `json:"admin_telegram_ids"`
	WebhookURL        string  `json:"webhook_url"`
	WebhookConfigured bool    `json:"webhook_configured"`
}

type paymentVerificationStoredConfig struct {
	PaymentVerificationConfig
	Generation              string `json:"generation"`
	WebhookSecretEncrypted  string `json:"webhook_secret_encrypted,omitempty"`
	ConnectedBotFingerprint string `json:"connected_bot_fingerprint,omitempty"`
	ConnectedWebhookURL     string `json:"connected_webhook_url,omitempty"`
	ConnectedTemplateID     string `json:"connected_template_id,omitempty"`
	ConnectedChatID         string `json:"connected_chat_id,omitempty"`
	ConnectedTopicID        int64  `json:"connected_topic_id,omitempty"`
}

func defaultPaymentVerificationConfig() *paymentVerificationStoredConfig {
	return &paymentVerificationStoredConfig{PaymentVerificationConfig: PaymentVerificationConfig{
		LookbackDays: 7, IntervalMinutes: 5, AdminTelegramIDs: []int64{},
	}}
}

func (s *PaymentVerificationService) loadPaymentVerificationConfig(ctx context.Context) (*paymentVerificationStoredConfig, error) {
	if s == nil || s.ops == nil || s.ops.settingRepo == nil {
		return nil, paymentVerificationConfigUnavailable()
	}
	raw, err := s.ops.settingRepo.GetValue(ctx, paymentVerificationSettingKey)
	if errors.Is(err, ErrSettingNotFound) || err == nil && strings.TrimSpace(raw) == "" {
		return defaultPaymentVerificationConfig(), nil
	}
	if err != nil {
		return nil, paymentVerificationConfigUnavailable()
	}
	stored := defaultPaymentVerificationConfig()
	if json.Unmarshal([]byte(raw), stored) != nil {
		return nil, infraerrors.InternalServer("PAYMENT_VERIFICATION_CONFIG_CORRUPT", "支付查验设置损坏")
	}
	stored.WebhookConfigured = false
	if stored.AdminTelegramIDs == nil {
		stored.AdminTelegramIDs = []int64{}
	}
	return stored, nil
}

func (s *PaymentVerificationService) savePaymentVerificationConfig(ctx context.Context, stored *paymentVerificationStoredConfig) error {
	stored.WebhookConfigured = false // Derived from the active bot, never trust a caller's value.
	raw, err := json.Marshal(stored)
	if err != nil {
		return paymentVerificationConfigUnavailable()
	}
	if err := s.ops.settingRepo.Set(ctx, paymentVerificationSettingKey, string(raw)); err != nil {
		return paymentVerificationConfigUnavailable()
	}
	return nil
}

func (s *PaymentVerificationService) GetConfig(ctx context.Context) (*PaymentVerificationConfig, error) {
	stored, err := s.loadPaymentVerificationConfig(ctx)
	if err != nil {
		return nil, err
	}
	view := stored.PaymentVerificationConfig
	view.AdminTelegramIDs = append([]int64{}, view.AdminTelegramIDs...)
	if stored.Enabled && stored.WebhookSecretEncrypted != "" && stored.ConnectedWebhookURL == stored.WebhookURL {
		delivery, fingerprint, err := s.paymentVerificationDelivery(ctx, stored.TemplateID)
		view.WebhookConfigured = err == nil && paymentVerificationConnectionMatches(stored, delivery, fingerprint)
	}
	return &view, nil
}

func (s *PaymentVerificationService) UpdateConfig(ctx context.Context, input *PaymentVerificationConfig) (*PaymentVerificationConfig, error) {
	if s == nil {
		return nil, paymentVerificationConfigUnavailable()
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if input == nil {
		return nil, paymentVerificationConfigError("支付查验设置不能为空")
	}
	previous, err := s.loadPaymentVerificationConfig(ctx)
	if err != nil {
		return nil, err
	}
	candidate := *previous
	candidate.PaymentVerificationConfig = *input
	candidate.WebhookConfigured = false
	candidate.TemplateID = strings.TrimSpace(input.TemplateID)
	candidate.WebhookURL = strings.TrimSpace(input.WebhookURL)
	if candidate.LookbackDays == 0 {
		candidate.LookbackDays = 7
	}
	if candidate.IntervalMinutes == 0 {
		candidate.IntervalMinutes = 5
	}
	if candidate.LookbackDays < 1 || candidate.LookbackDays > 365 {
		return nil, paymentVerificationConfigError("查验范围必须为 1 至 365 天")
	}
	if candidate.IntervalMinutes < 1 || candidate.IntervalMinutes > 60 {
		return nil, paymentVerificationConfigError("查验间隔必须为 1 至 60 分钟")
	}
	candidate.AdminTelegramIDs = []int64{}
	if len(input.AdminTelegramIDs) > 100 {
		return nil, paymentVerificationConfigError("最多允许 100 个 Telegram 管理员")
	}
	for _, id := range input.AdminTelegramIDs {
		if id <= 0 {
			return nil, paymentVerificationConfigError("Telegram 管理员 ID 必须为正整数")
		}
		if !slices.Contains(candidate.AdminTelegramIDs, id) {
			candidate.AdminTelegramIDs = append(candidate.AdminTelegramIDs, id)
		}
	}
	slices.Sort(candidate.AdminTelegramIDs)
	if candidate.WebhookURL != "" {
		if err := validatePaymentVerificationWebhookURL(candidate.WebhookURL); err != nil {
			return nil, err
		}
	}
	if candidate.Enabled {
		if len(candidate.AdminTelegramIDs) == 0 {
			return nil, paymentVerificationConfigError("启用支付查验前请填写 Telegram 管理员数字 ID")
		}
		if candidate.WebhookURL == "" {
			return nil, paymentVerificationConfigError("启用支付查验前请填写 HTTPS 按钮回调地址")
		}
		if _, _, err := s.paymentVerificationDelivery(ctx, candidate.TemplateID); err != nil {
			return nil, err
		}
	}
	// A saved settings revision revokes action tokens from earlier messages.
	candidate.Generation, err = newPaymentVerificationSecret()
	if err != nil {
		return nil, err
	}
	if candidate.Enabled {
		if err := s.rotatePaymentVerificationWebhookSecret(&candidate); err != nil {
			return nil, err
		}
	}
	candidate.ConnectedBotFingerprint = ""
	candidate.ConnectedWebhookURL = ""
	candidate.ConnectedTemplateID = ""
	candidate.ConnectedChatID = ""
	candidate.ConnectedTopicID = 0
	if err := s.savePaymentVerificationConfig(ctx, &candidate); err != nil {
		return nil, err
	}
	// Saving never registers a webhook and never sends a Telegram message.
	return s.GetConfig(ctx)
}

func (s *PaymentVerificationService) rotatePaymentVerificationWebhookSecret(stored *paymentVerificationStoredConfig) error {
	if s.ops == nil || s.ops.encryptor == nil || s.ops.cfg == nil || !s.ops.cfg.Totp.EncryptionKeyConfigured {
		return paymentVerificationConfigError("请先配置固定 TOTP_ENCRYPTION_KEY，以加密保存按钮回调密钥")
	}
	secret, err := newPaymentVerificationSecret()
	if err != nil {
		return err
	}
	stored.WebhookSecretEncrypted, err = s.ops.encryptor.Encrypt(secret)
	if err != nil {
		return paymentVerificationConfigUnavailable()
	}
	stored.ConnectedBotFingerprint = ""
	stored.ConnectedWebhookURL = ""
	return nil
}

func newPaymentVerificationSecret() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", paymentVerificationConfigUnavailable()
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func paymentVerificationConnectionMatches(stored *paymentVerificationStoredConfig, delivery opsTelegramDeliveryConfig, fingerprint string) bool {
	return stored.Enabled && stored.WebhookSecretEncrypted != "" &&
		stored.ConnectedWebhookURL == stored.WebhookURL &&
		stored.ConnectedBotFingerprint == fingerprint &&
		stored.ConnectedTemplateID == stored.TemplateID &&
		stored.ConnectedChatID == delivery.ChatID &&
		stored.ConnectedTopicID == paymentVerificationTopicID(delivery.TopicID)
}

func paymentVerificationTopicID(topic *int64) int64 {
	if topic == nil {
		return 0
	}
	return *topic
}

func validatePaymentVerificationWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/v1/payment/verification/telegram" {
		return paymentVerificationConfigError("按钮回调地址必须为 HTTPS，路径为 /api/v1/payment/verification/telegram，不能包含账号、查询参数或片段")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if isBlockedHostname(host) || net.ParseIP(host) != nil && isPrivateIP(net.ParseIP(host)) {
		return paymentVerificationConfigError("按钮回调地址必须使用公网域名或地址")
	}
	return nil
}

func (s *PaymentVerificationService) paymentVerificationDelivery(ctx context.Context, templateID string) (opsTelegramDeliveryConfig, string, error) {
	var delivery opsTelegramDeliveryConfig
	if s == nil || s.ops == nil {
		return delivery, "", paymentVerificationConfigUnavailable()
	}
	stored, err := s.ops.loadOpsTelegramStoredConfig(ctx)
	if err != nil {
		return delivery, "", err
	}
	template, ok := findOpsTelegramTemplate(stored, templateID)
	if !ok || !template.Enabled {
		return delivery, "", paymentVerificationConfigError("支付查验必须选择已启用的 Telegram 模板")
	}
	chatID, err := strconv.ParseInt(template.ChatID, 10, 64)
	if err != nil || chatID == 0 {
		return delivery, "", paymentVerificationConfigError("支付查验模板的 Chat ID 必须为非零数字，不能使用 @名称")
	}
	token, err := s.ops.decryptOpsTelegramBotToken(template.BotTokenEncrypted)
	if err != nil {
		return delivery, "", err
	}
	delivery = opsTelegramDeliveryConfig{
		BotToken: token, ChatID: strconv.FormatInt(chatID, 10), TopicID: template.TopicID,
		BaseURL: template.BaseURL, DisableNotification: template.DisableNotification, ProtectContent: template.ProtectContent,
	}
	if err := validateOpsTelegramDeliveryConfig(delivery); err != nil {
		return delivery, "", err
	}
	// Include the API origin: an old bot token must not authorize a replacement
	// Telegram-compatible endpoint, even when its visible bot ID is unchanged.
	digest := sha256.Sum256([]byte(normalizeOpsTelegramBaseURL(delivery.BaseURL) + "\x00" + token))
	return delivery, hex.EncodeToString(digest[:]), nil
}

func paymentVerificationConfigError(message string) error {
	return infraerrors.BadRequest("PAYMENT_VERIFICATION_CONFIG_INVALID", message)
}

func paymentVerificationConfigUnavailable() error {
	return infraerrors.InternalServer("PAYMENT_VERIFICATION_CONFIG_UNAVAILABLE", "无法读取或保存支付查验设置")
}
