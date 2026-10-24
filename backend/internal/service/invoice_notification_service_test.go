package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type invoiceMailTestUsers struct{ admin *User }

func (u invoiceMailTestUsers) GetFirstAdmin(context.Context) (*User, error) { return u.admin, nil }
func (u invoiceMailTestUsers) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Username: "Applicant", Email: "applicant@example.com"}, nil
}

type invoiceMailTestSettings string

func (s invoiceMailTestSettings) GetFrontendURL(context.Context) string { return string(s) }

type invoiceMailTestSender struct {
	mu      sync.Mutex
	inputs  []NotificationEmailSendInput
	err     error
	started chan struct{}
	release chan struct{}
}

func (s *invoiceMailTestSender) Send(_ context.Context, in NotificationEmailSendInput) error {
	s.mu.Lock()
	s.inputs = append(s.inputs, in)
	err := s.err
	s.mu.Unlock()
	if s.started != nil {
		s.started <- struct{}{}
		<-s.release
	}
	return err
}
func invoiceMailTestAdmin() *User {
	return &User{ID: 2, Role: RoleAdmin, Status: StatusActive, Username: "Admin", Email: "admin@example.com"}
}

func TestInvoiceMailOutboxSendsToAdminOnce(t *testing.T) {
	db, svc := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, svc)
	sender := &invoiceMailTestSender{}
	worker := NewInvoiceNotificationService(db, sender, invoiceMailTestUsers{invoiceMailTestAdmin()}, invoiceMailTestSettings("https://site.example"))
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM invoice_admin_notifications WHERE application_id=$1`, r.ID).Scan(&state))
	require.Equal(t, "pending", state)
	processed, err := worker.processNext(context.Background())
	require.NoError(t, err)
	require.True(t, processed)
	require.Len(t, sender.inputs, 1)
	in := sender.inputs[0]
	require.Equal(t, "admin@example.com", in.RecipientEmail)
	require.Equal(t, NotificationEmailEventInvoiceApplication, in.Event)
	require.Equal(t, r.ID, in.SourceID)
	require.Equal(t, "100.00", in.Variables["invoice_amount"])
	require.Equal(t, "applicant@example.com", in.Variables["applicant_email"])
	require.Contains(t, in.Variables["invoice_review_url"], "/admin/invoices?q="+r.ID)
	processed, err = worker.processNext(context.Background())
	require.NoError(t, err)
	require.False(t, processed)
	require.Len(t, sender.inputs, 1)
	require.NoError(t, db.QueryRow(`SELECT state FROM invoice_admin_notifications WHERE application_id=$1`, r.ID).Scan(&state))
	require.Equal(t, "sent", state)
}

func TestInvoiceMailFailureRetriesWithoutLosingApplication(t *testing.T) {
	db, svc := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, svc)
	sender := &invoiceMailTestSender{err: errors.New("SMTP unavailable with private detail")}
	worker := NewInvoiceNotificationService(db, sender, invoiceMailTestUsers{invoiceMailTestAdmin()}, invoiceMailTestSettings(""))
	processed, err := worker.processNext(context.Background())
	require.True(t, processed)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private detail")
	current, err := svc.get(context.Background(), r.ID, 1)
	require.NoError(t, err)
	require.Equal(t, InvoiceReview, current.Status)
	var state, lastError string
	var next time.Time
	require.NoError(t, db.QueryRow(`SELECT state,last_error,next_attempt_at FROM invoice_admin_notifications WHERE application_id=$1`, r.ID).Scan(&state, &lastError, &next))
	require.Equal(t, "pending", state)
	require.NotContains(t, lastError, "private detail")
	require.True(t, next.After(time.Now()))
	processed, err = worker.processNext(context.Background())
	require.NoError(t, err)
	require.False(t, processed)
	sender.err = nil
	_, err = db.Exec(`UPDATE invoice_admin_notifications SET next_attempt_at=NOW()`)
	require.NoError(t, err)
	processed, err = worker.processNext(context.Background())
	require.NoError(t, err)
	require.True(t, processed)
	require.Len(t, sender.inputs, 2)
	require.Equal(t, "none", sender.inputs[1].Variables["invoice_review_display"])
}

func TestInvoiceMailNoTaskForDuplicateAndSkipsWithdrawn(t *testing.T) {
	db, svc := reviewInvoiceDB(t)
	r := reviewInvoiceCreate(t, db, svc)
	_, err := svc.Create(context.Background(), 1, InvoiceCreate{InvoiceTitle: r.InvoiceTitle, OrderIDs: []int64{r.Orders[0].ID}})
	require.Error(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM invoice_admin_notifications`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = svc.Action(context.Background(), r.ID, 1, 1, "withdraw", "", false)
	require.NoError(t, err)
	sender := &invoiceMailTestSender{}
	worker := NewInvoiceNotificationService(db, sender, invoiceMailTestUsers{invoiceMailTestAdmin()}, invoiceMailTestSettings(""))
	processed, err := worker.processNext(context.Background())
	require.NoError(t, err)
	require.True(t, processed)
	require.Empty(t, sender.inputs)
	var state string
	require.NoError(t, db.QueryRow(`SELECT state FROM invoice_admin_notifications`).Scan(&state))
	require.Equal(t, "skipped", state)
}

func TestInvoiceMailConcurrentWorkersDoNotDuplicate(t *testing.T) {
	db, svc := reviewInvoiceDB(t)
	reviewInvoiceCreate(t, db, svc)
	sender := &invoiceMailTestSender{started: make(chan struct{}, 1), release: make(chan struct{})}
	worker := NewInvoiceNotificationService(db, sender, invoiceMailTestUsers{invoiceMailTestAdmin()}, invoiceMailTestSettings(""))
	firstDone := make(chan error, 1)
	go func() { _, err := worker.processNext(context.Background()); firstDone <- err }()
	select {
	case <-sender.started:
	case <-time.After(5 * time.Second):
		t.Fatal("sender was not called")
	}
	processed, err := worker.processNext(context.Background())
	close(sender.release)
	require.NoError(t, err)
	require.False(t, processed)
	require.NoError(t, <-firstDone)
	require.Len(t, sender.inputs, 1)
}

func TestInvoiceMailOnlyUsesActiveAdministrator(t *testing.T) {
	db, svc := reviewInvoiceDB(t)
	reviewInvoiceCreate(t, db, svc)
	sender := &invoiceMailTestSender{}
	admin := invoiceMailTestAdmin()
	admin.Role = "user"
	worker := NewInvoiceNotificationService(db, sender, invoiceMailTestUsers{admin}, invoiceMailTestSettings(""))
	_, err := worker.processNext(context.Background())
	require.Error(t, err)
	require.Empty(t, sender.inputs)
}

func TestInvoiceMailTemplatesEscapeBuyerAndPreserveReviewURL(t *testing.T) {
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	for _, locale := range []string{"zh", "en"} {
		template, err := svc.GetTemplate(context.Background(), NotificationEmailEventInvoiceApplication, locale)
		require.NoError(t, err)
		preview, err := svc.PreviewTemplate(context.Background(), NotificationEmailPreviewInput{Event: NotificationEmailEventInvoiceApplication, Locale: locale, Subject: template.Subject, HTML: template.HTML, Variables: map[string]string{"invoice_company": "<script>bad</script>", "invoice_amount": "800.00", "invoice_review_url": "https://site.example/admin/invoices?q=INV-1"}})
		require.NoError(t, err)
		require.NotContains(t, preview.HTML, "<script>")
		require.Contains(t, preview.HTML, "&lt;script&gt;")
		require.Contains(t, preview.HTML, "800.00")
		require.Contains(t, preview.HTML, "https://site.example/admin/invoices?q=INV-1")
		require.False(t, strings.Contains(preview.Subject, "\n"))
	}
	require.Empty(t, invoiceReviewURL("javascript:alert(1)", "INV-1"))
	require.Empty(t, invoiceReviewURL("", "INV-1"))
}
