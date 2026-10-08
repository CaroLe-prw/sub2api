package service

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const paymentVerificationTelegramMaxResponse = 64 * 1024

type paymentVerificationTelegramButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

type paymentVerificationTelegramKeyboard struct {
	InlineKeyboard [][]paymentVerificationTelegramButton `json:"inline_keyboard"`
}

type paymentVerificationTelegramMessage struct {
	MessageID       int64 `json:"message_id"`
	MessageThreadID int64 `json:"message_thread_id"`
	Chat            struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

type paymentVerificationTelegramUpdate struct {
	CallbackQuery *struct {
		ID   string `json:"id"`
		From struct {
			ID    int64 `json:"id"`
			IsBot bool  `json:"is_bot"`
		} `json:"from"`
		Message *paymentVerificationTelegramMessage `json:"message"`
		Data    string                              `json:"data"`
	} `json:"callback_query"`
}

// ConnectTelegram is only called by the explicit administrator connect action.
// Configuration saves, scans and application startup never register a webhook.
func (s *PaymentVerificationService) ConnectTelegram(ctx context.Context) error {
	if s == nil {
		return paymentVerificationConfigUnavailable()
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	stored, err := s.loadPaymentVerificationConfig(ctx)
	if err != nil {
		return err
	}
	if !stored.Enabled {
		return paymentVerificationConfigError("请先启用并保存支付查验设置")
	}
	if err := validatePaymentVerificationWebhookURL(stored.WebhookURL); err != nil {
		return err
	}
	delivery, fingerprint, err := s.paymentVerificationDelivery(ctx, stored.TemplateID)
	if err != nil {
		return err
	}
	var current struct {
		URL string `json:"url"`
	}
	if err := s.paymentVerificationTelegramRequest(ctx, delivery, "getWebhookInfo", struct{}{}, &current); err != nil {
		return err
	}
	if current.URL != "" && current.URL != stored.WebhookURL {
		return paymentVerificationConfigError("这个机器人已连接其他 Webhook，请为支付查验使用专用机器人；不会覆盖已有连接")
	}
	// Rotate before registering and persist the disconnected state first. A
	// failed registration cannot leave old messages authorized for a new bot.
	if err := s.rotatePaymentVerificationWebhookSecret(stored); err != nil {
		return err
	}
	stored.Generation, err = newPaymentVerificationSecret()
	if err != nil {
		return err
	}
	if err := s.savePaymentVerificationConfig(ctx, stored); err != nil {
		return err
	}
	secret, err := s.ops.encryptor.Decrypt(stored.WebhookSecretEncrypted)
	if err != nil {
		return paymentVerificationConfigUnavailable()
	}
	var registered bool
	if err := s.paymentVerificationTelegramRequest(ctx, delivery, "setWebhook", map[string]any{
		"url": stored.WebhookURL, "secret_token": secret,
		"allowed_updates": []string{"callback_query"},
	}, &registered); err != nil {
		return err
	}
	if !registered {
		return paymentVerificationTelegramError("setWebhook")
	}
	stored.ConnectedBotFingerprint = fingerprint
	stored.ConnectedWebhookURL = stored.WebhookURL
	stored.ConnectedTemplateID = stored.TemplateID
	stored.ConnectedChatID = delivery.ChatID
	stored.ConnectedTopicID = paymentVerificationTopicID(delivery.TopicID)
	return s.savePaymentVerificationConfig(ctx, stored)
}

func (s *PaymentVerificationService) paymentVerificationTelegramRequest(ctx context.Context, delivery opsTelegramDeliveryConfig, method string, input, output any) error {
	if err := validateOpsTelegramDeliveryConfig(delivery); err != nil {
		return err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return paymentVerificationTelegramError(method)
	}
	endpoint := normalizeOpsTelegramBaseURL(delivery.BaseURL) + "/bot" + delivery.BotToken + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return paymentVerificationTelegramError(method)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.ops.opsTelegramClient().Do(req)
	if err != nil {
		// Neither transport errors nor Telegram descriptions are trusted: both
		// may contain the bot token or the credential-bearing request URL.
		return paymentVerificationTelegramError(method)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, paymentVerificationTelegramMaxResponse+1))
	if err != nil || len(body) > paymentVerificationTelegramMaxResponse || response.StatusCode < 200 || response.StatusCode >= 300 {
		return paymentVerificationTelegramError(method)
	}
	var result struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(body, &result) != nil || !result.OK {
		return paymentVerificationTelegramError(method)
	}
	if output != nil && (len(result.Result) == 0 || string(result.Result) == "null" || json.Unmarshal(result.Result, output) != nil) {
		return paymentVerificationTelegramError(method)
	}
	return nil
}

func paymentVerificationTelegramError(method string) error {
	return infraerrors.InternalServer("PAYMENT_VERIFICATION_TELEGRAM_FAILED", "Telegram "+method+" 请求失败，请检查机器人配置或稍后重试")
}

func (s *PaymentVerificationService) deliverPendingAlerts(ctx context.Context) error {
	stored, err := s.loadPaymentVerificationConfig(ctx)
	if err != nil || !stored.Enabled {
		return err
	}
	delivery, fingerprint, err := s.paymentVerificationDelivery(ctx, stored.TemplateID)
	if err != nil {
		return err
	}
	if !paymentVerificationConnectionMatches(stored, delivery, fingerprint) {
		return nil // No dead buttons: an administrator must connect this bot first.
	}
	now := time.Now().UTC()
	records, err := s.repo.ClaimNotifications(ctx, s.instanceID, now, 10, 3*time.Minute)
	if err != nil {
		return err
	}
	chatID, _ := strconv.ParseInt(delivery.ChatID, 10, 64)
	var firstError error
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		binding := PaymentVerificationNotificationBinding{
			TemplateID: stored.TemplateID, ConfigGeneration: stored.Generation,
			BotFingerprint: fingerprint, TelegramChatID: chatID,
			TelegramTopicID: paymentVerificationTopicID(delivery.TopicID),
		}
		if err := s.repo.PrepareNotification(ctx, record.ID, s.instanceID, binding); err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		sendErr := s.sendPaymentVerificationAlert(ctx, delivery, record)
		if sendErr != nil {
			if retryErr := s.repo.RetryNotification(ctx, record.ID, s.instanceID, time.Now().UTC().Add(5*time.Minute)); retryErr != nil && firstError == nil {
				firstError = retryErr
			}
			if firstError == nil {
				firstError = sendErr
			}
		}
	}
	return firstError
}

func (s *PaymentVerificationService) sendPaymentVerificationAlert(ctx context.Context, delivery opsTelegramDeliveryConfig, record PaymentVerificationRecord) error {
	if len(record.ActionToken) < 16 || len("pv:b:"+record.ActionToken) > 64 || !record.ExpiresAt.After(time.Now().UTC()) || record.State != PaymentVerificationPending {
		return infraerrors.Conflict("PAYMENT_VERIFICATION_ALERT_EXPIRED", "支付查验消息已失效")
	}
	buttons := []paymentVerificationTelegramButton{}
	if paymentVerificationCanOfferBan(record.Result) {
		buttons = append(buttons, paymentVerificationTelegramButton{Text: "封禁账号", CallbackData: "pv:b:" + record.ActionToken})
	}
	buttons = append(buttons, paymentVerificationTelegramButton{Text: "忽略本次", CallbackData: "pv:i:" + record.ActionToken})
	var message paymentVerificationTelegramMessage
	if err := s.paymentVerificationTelegramRequest(ctx, delivery, "sendMessage", map[string]any{
		"chat_id": delivery.ChatID, "text": buildPaymentVerificationTelegramText(&record),
		"parse_mode": opsTelegramParseModeMarkdownV2, "message_thread_id": delivery.TopicID,
		"disable_notification": delivery.DisableNotification, "protect_content": delivery.ProtectContent,
		"reply_markup": paymentVerificationTelegramKeyboard{InlineKeyboard: [][]paymentVerificationTelegramButton{buttons}},
	}, &message); err != nil {
		return err
	}
	expectedChat, _ := strconv.ParseInt(delivery.ChatID, 10, 64)
	if message.MessageID <= 0 || message.Chat.ID != expectedChat || message.MessageThreadID != paymentVerificationTopicID(delivery.TopicID) {
		return paymentVerificationTelegramError("sendMessage")
	}
	return s.repo.MarkNotified(ctx, record.ID, s.instanceID, message.MessageID, time.Now().UTC())
}

func paymentVerificationCanOfferBan(result string) bool {
	return result == PaymentVerificationUnpaid || result == PaymentVerificationAmountMismatch || result == PaymentVerificationTradeMismatch
}

func buildPaymentVerificationTelegramText(record *PaymentVerificationRecord) string {
	title := "🟠 *支付查验异常*"
	result := "待核实，暂不能确认平台收款情况"
	switch record.Result {
	case PaymentVerificationUnpaid:
		result = "支付平台返回未支付"
	case PaymentVerificationAmountMismatch:
		result = "平台金额与本地订单不一致"
	case PaymentVerificationTradeMismatch:
		result = "平台交易号与本地订单不一致"
	case PaymentVerificationVerified:
		result = "复查已确认支付"
	default:
		title = "🟡 *支付查验待核实*"
	}
	lines := []string{
		title, "",
		opsTelegramQuoteLine("用户", fmt.Sprintf("%s（#%d）", truncateString(record.Email, 256), record.UserID)),
		opsTelegramQuoteLine("订单", fmt.Sprintf("#%d / %s", record.OrderID, truncateString(record.OutTradeNo, 128))),
		opsTelegramQuoteLine("本地订单状态", truncateString(record.OrderStatus, 64)),
		opsTelegramQuoteLine("支付渠道", truncateString(record.ProviderName, 128)),
		opsTelegramQuoteLine("本地到账", fmt.Sprintf("USD %.2f", record.Amount)),
		opsTelegramQuoteLine("订单应付", fmt.Sprintf("%s %.2f", record.Currency, record.PayAmount)),
		opsTelegramQuoteLine("查验结果", result),
	}
	if record.UpstreamAmount != nil && (record.Result == PaymentVerificationVerified || paymentVerificationCanOfferBan(record.Result)) {
		lines = append(lines, opsTelegramQuoteLine("平台查询金额", fmt.Sprintf("%s %.2f", record.Currency, *record.UpstreamAmount)))
	}
	if record.Reason != "" {
		lines = append(lines, opsTelegramQuoteLine("说明", truncateString(record.Reason, 512)))
	}
	lines = append(lines, opsTelegramQuoteLine("查验时间", opsTelegramTime(record.CheckedAt)))
	switch record.State {
	case PaymentVerificationBanned:
		lines = append(lines, "", "⛔ *已封禁账号*")
	case PaymentVerificationIgnored:
		lines = append(lines, "", "✅ *已忽略本次告警*", "仅忽略告警，不影响支付查验与入账条件。")
	case PaymentVerificationResolved:
		lines = append(lines, "", "✅ *复查已恢复，未封禁账号*")
	}
	if record.HandledBy != "" {
		lines = append(lines, opsTelegramQuoteLine("处理者", record.HandledBy))
	}
	return strings.Join(lines, "\n")
}

func (s *PaymentVerificationService) HandleTelegramCallback(ctx context.Context, suppliedSecret string, body []byte) error {
	if s == nil {
		return paymentVerificationConfigUnavailable()
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	stored, err := s.loadPaymentVerificationConfig(ctx)
	if err != nil {
		return err
	}
	unauthorized := func() error {
		return infraerrors.Forbidden("PAYMENT_VERIFICATION_CALLBACK_FORBIDDEN", "支付查验按钮未获授权或已失效")
	}
	if !stored.Enabled || stored.WebhookSecretEncrypted == "" || s.ops.encryptor == nil || suppliedSecret == "" {
		return unauthorized()
	}
	secret, err := s.ops.encryptor.Decrypt(stored.WebhookSecretEncrypted)
	if err != nil || secret == "" || subtle.ConstantTimeCompare([]byte(secret), []byte(suppliedSecret)) != 1 {
		return unauthorized()
	}
	delivery, fingerprint, err := s.paymentVerificationDelivery(ctx, stored.TemplateID)
	if err != nil {
		return err
	}
	if !paymentVerificationConnectionMatches(stored, delivery, fingerprint) {
		return unauthorized()
	}
	var update paymentVerificationTelegramUpdate
	if len(body) > 64*1024 || json.Unmarshal(body, &update) != nil {
		return infraerrors.BadRequest("PAYMENT_VERIFICATION_CALLBACK_INVALID", "无效的 Telegram 回调")
	}
	callback := update.CallbackQuery
	if callback == nil {
		return nil
	}
	if callback.ID == "" || len(callback.ID) > 256 || callback.From.IsBot || !slices.Contains(stored.AdminTelegramIDs, callback.From.ID) {
		return unauthorized()
	}
	parts := strings.Split(callback.Data, ":")
	if len(callback.Data) > 64 || len(parts) != 3 || parts[0] != "pv" || (parts[1] != "b" && parts[1] != "i") || len(parts[2]) < 16 {
		return unauthorized()
	}
	record, err := s.repo.GetByToken(ctx, parts[2])
	if errors.Is(err, ErrPaymentVerificationNotFound) || record == nil && err == nil {
		return s.answerPaymentVerificationCallback(ctx, delivery, callback.ID, "这条告警已失效，请查看最新消息")
	}
	if err != nil {
		return err
	}
	if record.TemplateID != stored.TemplateID || record.ConfigGeneration != stored.Generation || record.BotFingerprint != fingerprint {
		return s.answerPaymentVerificationCallback(ctx, delivery, callback.ID, "通知设置已变更，这条消息的操作按钮已失效")
	}
	chatID, _ := strconv.ParseInt(delivery.ChatID, 10, 64)
	if callback.Message == nil || record.TelegramChatID != chatID || callback.Message.Chat.ID != record.TelegramChatID ||
		record.TelegramTopicID != paymentVerificationTopicID(delivery.TopicID) || callback.Message.MessageThreadID != record.TelegramTopicID {
		return unauthorized()
	}
	if record.TelegramMessageID == 0 {
		// Telegram can deliver a click before sendMessage's response has been
		// persisted. Returning a retryable error never authorizes that race.
		return infraerrors.InternalServer("PAYMENT_VERIFICATION_MESSAGE_PENDING", "告警消息正在登记，请稍后重试")
	}
	if callback.Message.MessageID != record.TelegramMessageID {
		return unauthorized()
	}
	if !record.ExpiresAt.After(time.Now().UTC()) {
		return s.answerPaymentVerificationCallback(ctx, delivery, callback.ID, "这条告警已过期，请在后台重新查验")
	}
	if record.State != PaymentVerificationPending {
		return s.finishPaymentVerificationCallback(ctx, delivery, callback.ID, record)
	}
	if parts[1] == "b" && !paymentVerificationCanOfferBan(record.Result) {
		return s.answerPaymentVerificationCallback(ctx, delivery, callback.ID, "收款情况尚未查明，不能通过这条消息封禁账号")
	}
	action := "ignore"
	if parts[1] == "b" {
		action = "ban"
	}
	resolved, err := s.ResolveAlert(ctx, record.ID, action, fmt.Sprintf("telegram:%d", callback.From.ID))
	if err != nil {
		if errors.Is(err, ErrPaymentVerificationRecovered) || errors.Is(err, ErrPaymentVerificationChanged) {
			latest, getErr := s.repo.GetByID(ctx, record.ID)
			if getErr == nil && latest != nil && latest.State != PaymentVerificationPending {
				return s.finishPaymentVerificationCallback(ctx, delivery, callback.ID, latest)
			}
		}
		if errors.Is(err, ErrPaymentVerificationUnsafeBan) || errors.Is(err, ErrPaymentVerificationRecovered) || errors.Is(err, ErrPaymentVerificationChanged) {
			return s.answerPaymentVerificationCallback(ctx, delivery, callback.ID, "查验结果已变化或暂不能确认，请在后台重新查验")
		}
		return err
	}
	return s.finishPaymentVerificationCallback(ctx, delivery, callback.ID, resolved)
}

func (s *PaymentVerificationService) answerPaymentVerificationCallback(ctx context.Context, delivery opsTelegramDeliveryConfig, callbackID, text string) error {
	return s.paymentVerificationTelegramRequest(ctx, delivery, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackID, "text": text, "show_alert": true,
	}, nil)
}

func (s *PaymentVerificationService) finishPaymentVerificationCallback(ctx context.Context, delivery opsTelegramDeliveryConfig, callbackID string, record *PaymentVerificationRecord) error {
	if record == nil {
		return infraerrors.InternalServer("PAYMENT_VERIFICATION_ALERT_UNAVAILABLE", "无法读取已处理的支付告警")
	}
	text := "告警已处理"
	switch record.State {
	case PaymentVerificationBanned:
		text = "账号已封禁"
	case PaymentVerificationIgnored:
		text = "已忽略本次告警，支付查验仍然生效"
	case PaymentVerificationResolved:
		text = "复查已恢复，未封禁账号"
	}
	answerErr := s.answerPaymentVerificationCallback(ctx, delivery, callbackID, text)
	// Editing the message is cosmetic. A stale callback may outlive Telegram's
	// editable-message window; do not undo or repeat the committed action.
	editErr := s.paymentVerificationTelegramRequest(ctx, delivery, "editMessageText", map[string]any{
		"chat_id": record.TelegramChatID, "message_id": record.TelegramMessageID,
		"text": buildPaymentVerificationTelegramText(record), "parse_mode": opsTelegramParseModeMarkdownV2,
		"reply_markup": paymentVerificationTelegramKeyboard{InlineKeyboard: [][]paymentVerificationTelegramButton{}},
	}, nil)
	if answerErr != nil || editErr != nil {
		// Telegram may reject an identical edit on a repeated callback. The
		// database state remains authoritative and must not be replayed.
		slog.Warn("payment verification Telegram feedback failed after resolution", "alert_id", record.ID)
	}
	return nil
}
