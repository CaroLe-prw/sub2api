package service

import (
	"time"
)

const (
	InvoicePendingPayment = "PENDING_PAYMENT"
	InvoiceReview         = "PENDING_REVIEW"
	InvoiceApproved       = "APPROVED"
	InvoiceSubmitting     = "SUBMITTING"
	InvoiceProcessing     = "PROCESSING"
	InvoiceUnknown        = "UNKNOWN"
	InvoiceAttention      = "NEEDS_ATTENTION"
	InvoiceCancelling     = "CANCELLING"
	InvoiceCompleted      = "COMPLETED"
	InvoiceCancelled      = "CANCELLED"
	InvoiceFailed         = "FAILED"
	InvoiceRejected       = "REJECTED"
	InvoiceWithdrawn      = "WITHDRAWN"
)

// Historical API snapshots are retained for existing records only. New
// applications are handled manually and do not select an upstream product.
type InvoiceOption struct {
	ID                     string   `json:"id"`
	Label                  string   `json:"label"`
	InvoiceCategory        string   `json:"invoiceCategory"`
	CategoryName           string   `json:"categoryName"`
	TaxRateBps             int64    `json:"taxRateBps"`
	CustomerFeeRateBps     int64    `json:"customerFeeRateBps"`
	MinAmountCent          string   `json:"minAmountCent"`
	MaxAmountCent          string   `json:"maxAmountCent"`
	AllowedInvoiceItems    []string `json:"allowedInvoiceItems"`
	AllowCustomInvoiceItem bool     `json:"allowCustomInvoiceItem"`
	IssuanceTiming         string   `json:"issuanceTiming"`
}
type InvoiceConfig struct {
	Enabled        bool             `json:"enabled"`
	MinAmountCent  int64            `json:"min_amount_cent"`
	FeeTiers       []InvoiceFeeTier `json:"fee_tiers"`
	PricingVersion int64            `json:"pricing_version"`
}
type InvoiceTitle struct {
	ID            int64  `json:"id"`
	CompanyName   string `json:"company_name"`
	TaxNumber     string `json:"tax_number"`
	DeliveryEmail string `json:"delivery_email"`
}
type InvoiceCreate struct {
	ExpectedAmountCent *int64 `json:"expected_amount_cent,omitempty"`
	ExpectedFeeCent    *int64 `json:"expected_fee_cent,omitempty"`
	PricingVersion     int64  `json:"pricing_version"`
	InvoiceType        string `json:"invoice_type"`
	InvoiceCategory    string `json:"invoice_category"`
	Quantity           string `json:"quantity"`
	UnitPriceCent      *int64 `json:"unit_price_cent,omitempty"`
	InvoiceTitle
	OrderIDs      []int64 `json:"order_ids"`
	InvoiceItem   string  `json:"invoice_item"`
	Remark        string  `json:"remark"`
	Specification string  `json:"specification"`
	Unit          string  `json:"unit"`
	SaveTitle     bool    `json:"save_title"`
}
type InvoiceOrder struct {
	ID             int64     `json:"id"`
	OutTradeNo     string    `json:"out_trade_no"`
	PayAmountCent  int64     `json:"pay_amount_cent"`
	AmountCent     int64     `json:"amount_cent"`
	PaymentFeeCent int64     `json:"payment_fee_cent"`
	CreatedAt      time.Time `json:"created_at"`
}
type InvoiceOrderPage struct {
	Items     []InvoiceOrder `json:"items"`
	NextAfter int64          `json:"next_after"`
	HasMore   bool           `json:"has_more"`
}
type InvoiceRecord struct {
	ServiceFeeCent      int64      `json:"service_fee_cent"`
	FeeRateBps          int64      `json:"fee_rate_bps"`
	FeePricingVersion   int64      `json:"fee_pricing_version"`
	FeePaid             bool       `json:"fee_paid"`
	FeePaymentOrderID   int64      `json:"fee_payment_order_id"`
	FeePaymentStatus    string     `json:"fee_payment_status"`
	FeeRefundRequired   bool       `json:"fee_refund_required"`
	InvoiceType         string     `json:"invoice_type"`
	InvoiceCategory     string     `json:"invoice_category"`
	Quantity            string     `json:"quantity"`
	UnitPriceCent       *int64     `json:"unit_price_cent,omitempty"`
	Manual              bool       `json:"manual"`
	RequiresManualCheck bool       `json:"requires_manual_check"`
	InvoiceNumber       string     `json:"invoice_number"`
	UploadedBy          int64      `json:"uploaded_by"`
	CompletedAt         *time.Time `json:"completed_at"`
	ID                  string     `json:"id"`
	UserID              int64      `json:"user_id"`
	Status              string     `json:"status"`
	InvoiceTitle
	Remark             string         `json:"remark"`
	InvoiceItem        string         `json:"invoice_item"`
	Specification      string         `json:"specification"`
	Unit               string         `json:"unit"`
	Option             InvoiceOption  `json:"option"`
	Orders             []InvoiceOrder `json:"orders"`
	AmountCent         int64          `json:"amount_cent"`
	EstimatedFeeCent   int64          `json:"estimated_fee_cent"`
	FeeAmountCent      int64          `json:"fee_amount_cent"`
	UpstreamID         string         `json:"upstream_id"`
	ApplicationNo      string         `json:"application_no"`
	LocalStatus        string         `json:"local_status"`
	Downloadable       bool           `json:"downloadable"`
	ReviewNote         string         `json:"review_note"`
	ReviewedBy         int64          `json:"reviewed_by"`
	ReviewedAt         *time.Time     `json:"reviewed_at"`
	LastError          string         `json:"last_error"`
	ErrorCode          string         `json:"error_code"`
	TraceID            string         `json:"trace_id"`
	CancelRequested    bool           `json:"cancel_requested"`
	SubmissionRejected bool           `json:"submission_rejected"`
	LastSyncedAt       *time.Time     `json:"last_synced_at"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}
type InvoiceEvent struct {
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}
type InvoiceDetail struct {
	*InvoiceRecord
	Events []InvoiceEvent `json:"events"`
}
type InvoicePage struct {
	Items    []*InvoiceRecord `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}
