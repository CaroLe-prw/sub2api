package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	PaymentVerificationVerified       = "verified"
	PaymentVerificationUnpaid         = "unpaid"
	PaymentVerificationAmountMismatch = "amount_mismatch"
	PaymentVerificationTradeMismatch  = "trade_mismatch"
	PaymentVerificationQueryError     = "query_error"
	PaymentVerificationPending        = "pending"
	PaymentVerificationIgnored        = "ignored"
	PaymentVerificationBanned         = "banned"
	PaymentVerificationResolved       = "resolved"
)

var (
	ErrPaymentVerificationNotFound  = infraerrors.NotFound("PAYMENT_VERIFICATION_NOT_FOUND", "payment verification alert not found")
	ErrPaymentVerificationChanged   = infraerrors.Conflict("PAYMENT_VERIFICATION_CHANGED", "payment verification alert has changed or was already handled")
	ErrPaymentVerificationUnsafeBan = infraerrors.BadRequest("PAYMENT_VERIFICATION_UNCONFIRMED", "payment could not be verified; account cannot be disabled")
	ErrPaymentVerificationRecovered = infraerrors.BadRequest("PAYMENT_VERIFICATION_RECOVERED", "payment verification alert was resolved; account was not disabled")
)

// PaymentVerificationRecord is a bounded verification snapshot, never a raw
// gateway response. Action tokens and delivery bindings must not reach admin JSON.
type PaymentVerificationRecord struct {
	ID             int64      `json:"id"`
	OrderID        int64      `json:"order_id"`
	UserID         int64      `json:"user_id"`
	Email          string     `json:"email"`
	ProviderName   string     `json:"provider_name"`
	OutTradeNo     string     `json:"out_trade_no"`
	Amount         float64    `json:"amount"`
	PayAmount      float64    `json:"pay_amount"`
	Currency       string     `json:"currency"`
	OrderStatus    string     `json:"order_status"`
	Result         string     `json:"result"`
	Reason         string     `json:"reason"`
	UpstreamAmount *float64   `json:"upstream_amount"`
	State          string     `json:"state"`
	CheckedAt      time.Time  `json:"checked_at"`
	CreatedAt      time.Time  `json:"created_at"`
	HandledAt      *time.Time `json:"handled_at"`
	HandledBy      string     `json:"handled_by"`

	ActionToken       string     `json:"-"`
	ExpiresAt         time.Time  `json:"-"`
	TemplateID        string     `json:"-"`
	ConfigGeneration  string     `json:"-"`
	BotFingerprint    string     `json:"-"`
	TelegramChatID    int64      `json:"-"`
	TelegramMessageID int64      `json:"-"`
	TelegramTopicID   int64      `json:"-"`
	NotifiedAt        *time.Time `json:"-"`
}

type PaymentVerificationNotificationBinding struct {
	TemplateID       string
	ConfigGeneration string
	BotFingerprint   string
	TelegramChatID   int64
	TelegramTopicID  int64
}

type PaymentVerificationScanResult struct {
	Checked   int `json:"checked"`
	Verified  int `json:"verified"`
	Anomalies int `json:"anomalies"`
	Errors    int `json:"errors"`
}

type PaymentVerificationRepository interface {
	ListCandidates(ctx context.Context, since, now time.Time, limit int) ([]int64, error)
	SaveCheck(ctx context.Context, record *PaymentVerificationRecord, nextCheckAt time.Time) (*PaymentVerificationRecord, error)
	ListAlerts(ctx context.Context, limit int) ([]PaymentVerificationRecord, error)
	GetByID(ctx context.Context, id int64) (*PaymentVerificationRecord, error)
	GetByToken(ctx context.Context, token string) (*PaymentVerificationRecord, error)
	// Resolve atomically claims an unchanged pending alert, changes user status
	// for ban, and appends the payment audit entry. It must reject admin users.
	Resolve(ctx context.Context, id int64, checkedAt time.Time, action, actor string, now time.Time) (*PaymentVerificationRecord, error)
	ClaimNotifications(ctx context.Context, owner string, now time.Time, limit int, lease time.Duration) ([]PaymentVerificationRecord, error)
	PrepareNotification(ctx context.Context, id int64, owner string, binding PaymentVerificationNotificationBinding) error
	MarkNotified(ctx context.Context, id int64, owner string, messageID int64, now time.Time) error
	RetryNotification(ctx context.Context, id int64, owner string, retryAt time.Time) error
}
