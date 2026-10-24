package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

type invoiceNotificationSender interface {
	Send(context.Context, NotificationEmailSendInput) error
}
type invoiceNotificationUsers interface {
	GetFirstAdmin(context.Context) (*User, error)
	GetByID(context.Context, int64) (*User, error)
}
type invoiceNotificationSettings interface{ GetFrontendURL(context.Context) string }

// This worker sends mail only; invoice issuance remains entirely manual.
type InvoiceNotificationService struct {
	db        *sql.DB
	sender    invoiceNotificationSender
	users     invoiceNotificationUsers
	settings  invoiceNotificationSettings
	cancel    context.CancelFunc
	done      chan struct{}
	startOnce sync.Once
}

func NewInvoiceNotificationService(db *sql.DB, sender invoiceNotificationSender, users invoiceNotificationUsers, settings invoiceNotificationSettings) *InvoiceNotificationService {
	return &InvoiceNotificationService{db: db, sender: sender, users: users, settings: settings, done: make(chan struct{})}
}
func (s *InvoiceNotificationService) Start() {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		go func() {
			defer close(s.done)
			timer := time.NewTicker(5 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					for i := 0; i < 10; i++ {
						jobCtx, jobCancel := context.WithTimeout(ctx, 90*time.Second)
						processed, err := s.processNext(jobCtx)
						jobCancel()
						if err != nil {
							if ctx.Err() == nil {
								slog.Warn("invoice administrator email pending retry", "error", err)
							}
							break
						}
						if !processed {
							break
						}
					}
				}
			}
		}()
	})
}
func (s *InvoiceNotificationService) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

func invoiceReviewURL(base, id string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return ""
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/admin/invoices"
	u.RawPath = ""
	u.Fragment = ""
	u.RawQuery = url.Values{"q": []string{id}}.Encode()
	return u.String()
}

func (s *InvoiceNotificationService) processNext(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var id string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT application_id,attempts FROM invoice_admin_notifications WHERE state='pending' AND next_attempt_at<=NOW() ORDER BY next_attempt_at,created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	r, err := scanInvoice(tx.QueryRowContext(ctx, invoiceSelect+` WHERE id=$1`, id))
	if err != nil {
		return true, err
	}
	if r.Status != InvoiceReview {
		_, err = tx.ExecContext(ctx, `UPDATE invoice_admin_notifications SET state='skipped',last_error='' WHERE application_id=$1`, id)
		if err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	admin, sendErr := s.users.GetFirstAdmin(ctx)
	if sendErr == nil && (admin == nil || admin.Role != RoleAdmin || admin.Status != StatusActive || strings.TrimSpace(admin.Email) == "") {
		sendErr = errors.New("no active administrator email is configured")
	}
	var applicant *User
	if sendErr == nil {
		applicant, sendErr = s.users.GetByID(ctx, r.UserID)
		if errors.Is(sendErr, ErrUserNotFound) {
			applicant = nil
			sendErr = nil
		}
	}
	if sendErr == nil {
		applicantName := "#" + strconv.FormatInt(r.UserID, 10)
		applicantEmail := "-"
		if applicant != nil {
			if applicant.Username != "" {
				applicantName = applicant.Username
			}
			if applicant.Email != "" {
				applicantEmail = applicant.Email
			}
		}
		tax := r.TaxNumber
		if tax == "" {
			tax = "-"
		}
		base := ""
		if s.settings != nil {
			base = s.settings.GetFrontendURL(ctx)
		}
		reviewURL := invoiceReviewURL(base, r.ID)
		reviewDisplay := "none"
		if reviewURL != "" {
			reviewDisplay = "block"
		}
		adminName := strings.TrimSpace(admin.Username)
		if adminName == "" {
			adminName = "Administrator"
		}
		if s.sender == nil {
			sendErr = errors.New("notification email service is unavailable")
		} else {
			sendErr = s.sender.Send(ctx, NotificationEmailSendInput{
				Event: NotificationEmailEventInvoiceApplication, RecipientEmail: admin.Email, RecipientName: adminName, UserID: admin.ID,
				SourceType: "invoice_application", SourceID: r.ID, ReminderKey: "admin_review",
				Variables: map[string]string{
					"invoice_id": r.ID, "applicant_name": applicantName, "applicant_email": applicantEmail,
					"invoice_company": r.CompanyName, "invoice_tax_number": tax,
					"invoice_amount": decimal.NewFromInt(r.AmountCent).Div(decimal.NewFromInt(100)).StringFixed(2),
					"invoice_item":   r.InvoiceItem, "invoice_order_count": strconv.Itoa(len(r.Orders)),
					"invoice_created_at": r.CreatedAt.Format("2006-01-02 15:04:05 MST"), "invoice_review_url": reviewURL, "invoice_review_display": reviewDisplay,
				},
			})
		}
	}
	if sendErr != nil {
		// Back off up to one hour, including while SMTP is not configured. Store
		// only a generic failure message here; provider errors can contain secrets.
		delay := time.Minute * time.Duration(1<<min(attempts, 6))
		if delay > time.Hour {
			delay = time.Hour
		}
		_, err = tx.ExecContext(ctx, `UPDATE invoice_admin_notifications SET attempts=attempts+1,next_attempt_at=NOW()+$2*interval '1 second',last_error='邮件发送失败，等待重试，请检查管理员邮箱和SMTP配置' WHERE application_id=$1`, id, delay.Seconds())
		if err != nil {
			return true, err
		}
		if err = tx.Commit(); err != nil {
			return true, err
		}
		return true, fmt.Errorf("invoice %s administrator notification could not be sent", id)
	}
	_, err = tx.ExecContext(ctx, `UPDATE invoice_admin_notifications SET state='sent',attempts=attempts+1,recipient_id=$2,sent_at=NOW(),last_error='' WHERE application_id=$1`, id, admin.ID)
	if err != nil {
		return true, err
	}
	return true, tx.Commit()
}
