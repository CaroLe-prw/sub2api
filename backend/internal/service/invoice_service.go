package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net/mail"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type InvoiceService struct{ db *sql.DB }

func NewInvoiceService(db *sql.DB) *InvoiceService { return &InvoiceService{db: db} }
func invoiceBad(message string) error              { return infraerrors.BadRequest("INVOICE_INVALID", message) }
func invoiceConflict(message string) error         { return infraerrors.Conflict("INVOICE_CONFLICT", message) }
func invoiceDBError(err error) error {
	var pe *pq.Error
	if errors.As(err, &pe) && (pe.Constraint == "invoice_order_one_active" || pe.Constraint == "invoice_refund_guard") {
		return invoiceConflict("订单已关联发票或正在退款，请刷新后重试")
	}
	return err
}
func (s *InvoiceService) GetConfig(ctx context.Context) (*InvoiceConfig, error) {
	return scanInvoiceConfig(s.db.QueryRowContext(ctx, invoiceConfigSelect))
}
func (s *InvoiceService) UpdateConfig(ctx context.Context, in InvoiceConfig) (*InvoiceConfig, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	old, err := scanInvoiceConfig(tx.QueryRowContext(ctx, invoiceConfigSelect+` FOR UPDATE`))
	if err != nil {
		return nil, err
	}
	if in.FeeTiers == nil {
		in.MinAmountCent = old.MinAmountCent
		in.FeeTiers = old.FeeTiers
	}
	if err = validateInvoicePricing(in.MinAmountCent, in.FeeTiers); err != nil {
		return nil, err
	}
	version := old.PricingVersion
	if old.MinAmountCent != in.MinAmountCent || invoiceJSON(old.FeeTiers) != invoiceJSON(in.FeeTiers) {
		version++
	}
	_, err = tx.ExecContext(ctx, `UPDATE invoice_settings SET enabled=$1,min_amount_cent=$2,fee_tiers=$3,pricing_version=$4,updated_at=NOW() WHERE id=1`, in.Enabled, in.MinAmountCent, invoiceJSON(in.FeeTiers), version)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetConfig(ctx)
}
func validateInvoiceTitle(t *InvoiceTitle) error {
	t.CompanyName = strings.TrimSpace(t.CompanyName)
	t.TaxNumber = strings.ToUpper(strings.TrimSpace(t.TaxNumber))
	t.DeliveryEmail = strings.TrimSpace(t.DeliveryEmail)
	if n := utf8.RuneCountInString(t.CompanyName); n < 2 || n > 200 {
		return invoiceBad("企业抬头须为2—200个字符")
	}
	// 税号选填：个人或海外公司可留空；填写时须为15—20位数字或大写英文字母。
	if t.TaxNumber != "" && !regexp.MustCompile(`^[A-Z0-9]{15,20}$`).MatchString(t.TaxNumber) {
		return invoiceBad("税号须为15—20位数字或英文字母")
	}
	if t.DeliveryEmail != "" {
		a, e := mail.ParseAddress(t.DeliveryEmail)
		if e != nil || a.Address != t.DeliveryEmail || len(t.DeliveryEmail) > 254 {
			return invoiceBad("请输入正确的接收邮箱")
		}
	}
	return nil
}

// Recover the original cent principal from the stored gross and fee rate.
// Gross = principal + ceil(principal * feeRate / 100). The map is strictly
// increasing for nonnegative rates; verify the candidate, never guess a gap.
func invoicePrincipal(pay, fee, snapshot string) (int64, int64, error) {
	p, e := decimal.NewFromString(pay)
	if e != nil || !p.GreaterThan(decimal.Zero) || !p.Equal(p.Round(2)) {
		return 0, 0, invoiceBad("订单实付金额无效")
	}
	cents := p.Mul(decimal.NewFromInt(100))
	if cents.GreaterThan(decimal.NewFromInt(9000000000000000)) {
		return 0, 0, invoiceBad("订单金额超出范围")
	}
	gross := cents.IntPart()
	rate, e := decimal.NewFromString(fee)
	if e != nil || rate.IsNegative() {
		return 0, 0, invoiceBad("订单手续费率无效")
	}
	base := cents.Div(decimal.NewFromInt(1).Add(rate.Div(decimal.NewFromInt(100)))).Floor().IntPart()
	if snapshot != "" {
		base, e = strconv.ParseInt(snapshot, 10, 64)
		if e != nil {
			return 0, 0, invoiceBad("本金快照无效")
		}
	}
	expected := decimal.NewFromInt(base).Add(decimal.NewFromInt(base).Mul(rate).Div(decimal.NewFromInt(100)).Ceil())
	if base <= 0 || !expected.Equal(cents) {
		return 0, 0, invoiceBad("无法核实历史订单本金，需要人工核对")
	}
	return base, gross, nil
}

const invoiceOrderSelect = `SELECT o.id,o.out_trade_no,o.pay_amount::text,o.fee_rate::text,
 COALESCE(o.provider_snapshot->>'invoice_principal_cent',''),o.created_at
 FROM payment_orders o`
const invoiceEligible = `o.user_id=$1 AND o.status='COMPLETED' AND o.order_type='balance'
 AND o.refund_amount=0 AND o.refund_at IS NULL AND o.refund_requested_at IS NULL
 AND COALESCE(NULLIF(o.provider_snapshot->>'currency',''),'CNY')='CNY'`

type invoiceScanner interface{ Scan(...any) error }

func scanInvoiceOrder(row invoiceScanner) (InvoiceOrder, error) {
	var o InvoiceOrder
	var pay, fee, snapshot string
	err := row.Scan(&o.ID, &o.OutTradeNo, &pay, &fee, &snapshot, &o.CreatedAt)
	if err != nil {
		return o, err
	}
	o.AmountCent, o.PayAmountCent, err = invoicePrincipal(pay, fee, snapshot)
	o.PaymentFeeCent = o.PayAmountCent - o.AmountCent
	return o, err
}
func (s *InvoiceService) EligibleOrders(ctx context.Context, user int64, after int64) (*InvoiceOrderPage, error) {
	rows, err := s.db.QueryContext(ctx, invoiceOrderSelect+` WHERE `+invoiceEligible+` AND o.id>$2 AND NOT EXISTS(SELECT 1 FROM invoice_order_links l WHERE l.order_id=o.id AND active) ORDER BY o.id LIMIT 201`, user, after)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := &InvoiceOrderPage{Items: []InvoiceOrder{}, NextAfter: after}
	count := 0
	for rows.Next() {
		count++
		if count > 200 {
			out.HasMore = true
			break
		}
		o, e := scanInvoiceOrder(rows)
		if e != nil {
			var appErr *infraerrors.ApplicationError
			if !errors.As(e, &appErr) {
				return nil, e
			}
		}
		// Advance over invalid historical amounts too, otherwise an entire invalid
		// page would hide all valid orders that follow it.
		out.NextAfter = o.ID
		if e == nil {
			out.Items = append(out.Items, o)
		}
	}
	return out, rows.Err()
}

func invoiceJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func invoiceEvent(ctx context.Context, tx *sql.Tx, id, actor, action, message string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO invoice_events(application_id,actor,action,message) VALUES($1,$2,$3,$4)`, id, actor, action, message)
	return err
}
func (s *InvoiceService) Create(ctx context.Context, user int64, in InvoiceCreate) (*InvoiceRecord, error) {
	if err := validateInvoiceTitle(&in.InvoiceTitle); err != nil {
		return nil, err
	}

	if in.InvoiceType == "" {
		in.InvoiceType = "NORMAL"
	}
	if in.InvoiceType != "NORMAL" {
		return nil, invoiceBad("目前仅支持普通发票")
	}
	if in.InvoiceCategory == "" {
		in.InvoiceCategory = "生产生活服务"
	}
	if in.InvoiceCategory != "生产生活服务" {
		return nil, invoiceBad("发票类目不支持")
	}
	in.Quantity = strings.TrimSpace(in.Quantity)
	if in.Quantity != "" {
		quantity, quantityErr := decimal.NewFromString(in.Quantity)
		if !regexp.MustCompile(`^[0-9]{1,12}(\.[0-9]{1,6})?$`).MatchString(in.Quantity) || quantityErr != nil || !quantity.GreaterThan(decimal.Zero) {
			return nil, invoiceBad("数量须大于0，最多12位整数和6位小数")
		}
	}
	if in.UnitPriceCent != nil && (*in.UnitPriceCent <= 0 || *in.UnitPriceCent > 9000000000000000) {
		return nil, invoiceBad("单价须为大于0的整数分")
	}
	in.InvoiceItem = strings.TrimSpace(in.InvoiceItem)
	if in.InvoiceItem == "" {
		in.InvoiceItem = "技术服务费"
	}
	if utf8.RuneCountInString(in.InvoiceItem) > 200 {
		return nil, invoiceBad("开票项目最多200个字符")
	}
	in.Specification = strings.TrimSpace(in.Specification)
	in.Unit = strings.TrimSpace(in.Unit)
	if utf8.RuneCountInString(in.Remark) > 300 {
		return nil, invoiceBad("备注最多300个字符")
	}
	if utf8.RuneCountInString(in.Specification) > 200 {
		return nil, invoiceBad("规格型号最多200个字符")
	}
	if utf8.RuneCountInString(in.Unit) > 32 {
		return nil, invoiceBad("单位最多32个字符")
	}
	if len(in.OrderIDs) == 0 || len(in.OrderIDs) > 100 {
		return nil, invoiceBad("每次请选择1—100笔充值订单")
	}
	slices.Sort(in.OrderIDs)
	for i, id := range in.OrderIDs {
		if id <= 0 || i > 0 && id == in.OrderIDs[i-1] {
			return nil, invoiceBad("订单不能重复")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	cfg, err := scanInvoiceConfig(tx.QueryRowContext(ctx, invoiceConfigSelect+` FOR SHARE`))
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, invoiceBad("发票申请暂未开放")
	}
	record := &InvoiceRecord{ID: "INV_" + strings.ReplaceAll(uuid.NewString(), "-", ""), UserID: user, Status: InvoiceReview, Manual: true, InvoiceType: in.InvoiceType, InvoiceCategory: in.InvoiceCategory, Quantity: in.Quantity, UnitPriceCent: in.UnitPriceCent, InvoiceTitle: in.InvoiceTitle, Remark: in.Remark, InvoiceItem: in.InvoiceItem, Specification: in.Specification, Unit: in.Unit, Orders: []InvoiceOrder{}}
	for _, id := range in.OrderIDs {
		order, e := scanInvoiceOrder(tx.QueryRowContext(ctx, invoiceOrderSelect+` WHERE `+invoiceEligible+` AND o.id=$2 FOR UPDATE OF o`, user, id))
		if errors.Is(e, sql.ErrNoRows) {
			return nil, invoiceBad("订单不属于您、未完成、非人民币支付或已申请退款")
		}
		if e != nil {
			return nil, e
		}
		if record.AmountCent > math.MaxInt64-order.AmountCent {
			return nil, invoiceBad("金额超出范围")
		}
		record.AmountCent += order.AmountCent
		record.Orders = append(record.Orders, order)
	}
	if record.AmountCent > 9000000000000000 {
		return nil, invoiceBad("合计金额超出范围")
	}
	quote, err := invoiceFeeQuote(cfg, record.AmountCent)
	if err != nil {
		return nil, err
	}
	if in.ExpectedAmountCent != nil && *in.ExpectedAmountCent != quote.AmountCent || in.ExpectedFeeCent != nil && *in.ExpectedFeeCent != quote.FeeCent || in.PricingVersion != 0 && in.PricingVersion != quote.PricingVersion || quote.FeeCent > 0 && (in.ExpectedFeeCent == nil || in.PricingVersion != quote.PricingVersion) {
		return nil, invoiceConflict("手续费报价已变化，请重新核对后提交")
	}
	record.ServiceFeeCent = quote.FeeCent
	record.FeeRateBps = quote.RateBps
	record.FeePricingVersion = quote.PricingVersion
	if quote.FeeCent > 0 {
		record.Status = InvoicePendingPayment
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO invoice_applications(id,user_id,status,data) VALUES($1,$2,$3,$4)`, record.ID, user, record.Status, invoiceJSON(record)); err != nil {
		return nil, err
	}
	for _, o := range record.Orders {
		_, err = tx.ExecContext(ctx, `INSERT INTO invoice_order_links(application_id,order_id,amount_cent) VALUES($1,$2,$3)`, record.ID, o.ID, o.AmountCent)
		if err != nil {
			return nil, invoiceDBError(err)
		}
	}
	if in.SaveTitle {
		_, err = tx.ExecContext(ctx, `INSERT INTO invoice_titles(user_id,company_name,tax_number,delivery_email) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,tax_number) DO UPDATE SET company_name=EXCLUDED.company_name,delivery_email=EXCLUDED.delivery_email`, user, in.CompanyName, in.TaxNumber, in.DeliveryEmail)
		if err != nil {
			return nil, err
		}
	}
	if err = invoiceEvent(ctx, tx, record.ID, strconv.FormatInt(user, 10), "CREATE", ""); err != nil {
		return nil, err
	}
	// Persist the notification atomically with the application. SMTP failures
	// must not roll back a valid application or cause duplicate submissions.
	if record.Status == InvoiceReview {
		if _, err = tx.ExecContext(ctx, `INSERT INTO invoice_admin_notifications(application_id) VALUES($1)`, record.ID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.get(ctx, record.ID, user)
}

const invoiceSelect = `SELECT id,user_id,status,data,created_at,updated_at,
 EXISTS(SELECT 1 FROM invoice_files f WHERE f.application_id=invoice_applications.id),
 (payload IS NOT NULL AND NOT COALESCE((data->>'manual')::boolean,FALSE)),
 COALESCE((SELECT status FROM payment_orders WHERE id=NULLIF(invoice_applications.data->>'fee_payment_order_id','0')::bigint),''),
 EXISTS(SELECT 1 FROM invoice_fee_payments fp JOIN payment_orders po ON po.id=fp.payment_order_id WHERE fp.application_id=invoice_applications.id AND fp.refund_required AND po.status IN ('PAID','RECHARGING','COMPLETED','REFUNDING','REFUND_PENDING','REFUND_FAILED','PARTIALLY_REFUNDED'))
 FROM invoice_applications`

func scanInvoice(row invoiceScanner) (*InvoiceRecord, error) {
	var id, status string
	var uid int64
	var raw []byte
	var created, updated time.Time
	var downloadable, requiresManualCheck, refundRequired bool
	var paymentStatus string
	if err := row.Scan(&id, &uid, &status, &raw, &created, &updated, &downloadable, &requiresManualCheck, &paymentStatus, &refundRequired); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, infraerrors.NotFound("INVOICE_NOT_FOUND", "发票申请不存在")
		}
		return nil, err
	}
	r := &InvoiceRecord{}
	if err := json.Unmarshal(raw, r); err != nil {
		return nil, err
	}
	r.Downloadable = downloadable && status == InvoiceCompleted
	r.RequiresManualCheck = requiresManualCheck
	r.FeePaymentStatus = paymentStatus
	r.FeeRefundRequired = refundRequired
	r.ID = id
	r.UserID = uid
	r.Status = status
	r.CreatedAt = created
	r.UpdatedAt = updated
	return r, nil
}
func (s *InvoiceService) get(ctx context.Context, id string, user int64) (*InvoiceRecord, error) {
	return scanInvoice(s.db.QueryRowContext(ctx, invoiceSelect+` WHERE id=$1 AND ($2::bigint=0 OR user_id=$2)`, id, user))
}
func (s *InvoiceService) List(ctx context.Context, user int64, page, size int, status, q string) (*InvoicePage, error) {
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	if size < 1 || size > 100 {
		size = 20
	}
	if len(q) > 200 {
		q = q[:200]
	}
	where := ` WHERE ($1::bigint=0 OR user_id=$1) AND ($2='' OR status=$2) AND ($3='' OR id ILIKE '%'||$3||'%' OR data->>'company_name' ILIKE '%'||$3||'%' OR user_id::text=$3)`
	result := &InvoicePage{Items: []*InvoiceRecord{}, Page: page, PageSize: size}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM invoice_applications`+where, user, status, q).Scan(&result.Total); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, invoiceSelect+where+` ORDER BY created_at DESC,id DESC LIMIT $4 OFFSET $5`, user, status, q, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, e := scanInvoice(rows)
		if e != nil {
			return nil, e
		}
		result.Items = append(result.Items, r)
	}
	return result, rows.Err()
}
func (s *InvoiceService) Detail(ctx context.Context, id string, user int64) (*InvoiceDetail, error) {
	r, err := s.get(ctx, id, user)
	if err != nil {
		return nil, err
	}
	out := &InvoiceDetail{InvoiceRecord: r, Events: []InvoiceEvent{}}
	rows, err := s.db.QueryContext(ctx, `SELECT actor,action,message,created_at FROM invoice_events WHERE application_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e InvoiceEvent
		if err = rows.Scan(&e.Actor, &e.Action, &e.Message, &e.CreatedAt); err != nil {
			return nil, err
		}
		out.Events = append(out.Events, e)
	}
	return out, rows.Err()
}
func (s *InvoiceService) Titles(ctx context.Context, user int64) ([]InvoiceTitle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,company_name,tax_number,delivery_email FROM invoice_titles WHERE user_id=$1 ORDER BY id DESC LIMIT 100`, user)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []InvoiceTitle{}
	for rows.Next() {
		var t InvoiceTitle
		if err = rows.Scan(&t.ID, &t.CompanyName, &t.TaxNumber, &t.DeliveryEmail); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *InvoiceService) DeleteTitle(ctx context.Context, user, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM invoice_titles WHERE id=$1 AND user_id=$2`, id, user)
	return err
}

// A single state transition is transactional with its audit event and release.
func saveInvoice(ctx context.Context, tx *sql.Tx, r *InvoiceRecord, actor, action string) error {
	_, err := tx.ExecContext(ctx, `UPDATE invoice_applications SET status=$2,data=$3,updated_at=NOW(),next_check_at='infinity',lease_until=NULL,lease_token='' WHERE id=$1`, r.ID, r.Status, invoiceJSON(r))
	if err != nil {
		return err
	}
	if slices.Contains([]string{InvoiceRejected, InvoiceWithdrawn, InvoiceCancelled, InvoiceFailed}, r.Status) {
		if _, err = tx.ExecContext(ctx, `UPDATE invoice_fee_payments SET refund_required=TRUE WHERE application_id=$1`, r.ID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE invoice_order_links SET active=FALSE WHERE application_id=$1`, r.ID); err != nil {
			return err
		}
	}
	return invoiceEvent(ctx, tx, r.ID, actor, action, r.ReviewNote)
}
func (s *InvoiceService) Action(ctx context.Context, id string, user, actor int64, action, note string, confirmNotIssued bool) (*InvoiceRecord, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 500 {
		return nil, invoiceBad("审核意见最多500个字符")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	r, err := scanInvoice(tx.QueryRowContext(ctx, invoiceSelect+` WHERE id=$1 AND ($2::bigint=0 OR user_id=$2) FOR UPDATE`, id, user))
	if err != nil {
		return nil, err
	}
	switch action {
	case "withdraw":
		if r.Status != InvoiceReview && r.Status != InvoicePendingPayment {
			return nil, invoiceConflict("只有待审核申请可撤回")
		}
		r.Status = InvoiceWithdrawn
	case "reject":
		if r.Status != InvoiceReview || note == "" {
			return nil, invoiceBad("仅待审核申请可驳回，并须填写原因")
		}
		r.Status = InvoiceRejected
		r.ReviewNote = note
	case "approve":
		if r.ServiceFeeCent > 0 && (!r.FeePaid || r.FeePaymentStatus != "COMPLETED") {
			return nil, invoiceConflict("手续费尚未支付，不能审核开票")
		}
		if r.Status != InvoiceReview || r.RequiresManualCheck {
			return nil, invoiceConflict("申请已处理，请刷新")
		}
		var enabled bool
		if err = tx.QueryRowContext(ctx, `SELECT enabled FROM invoice_settings WHERE id=1 FOR SHARE`).Scan(&enabled); err != nil {
			return nil, err
		}
		if !enabled {
			return nil, invoiceBad("开票申请已关闭")
		}
		for _, order := range r.Orders {
			current, e := scanInvoiceOrder(tx.QueryRowContext(ctx, invoiceOrderSelect+` WHERE `+invoiceEligible+` AND o.id=$2 FOR UPDATE OF o`, r.UserID, order.ID))
			if e != nil || current.AmountCent != order.AmountCent {
				return nil, invoiceConflict("关联订单或本金已变化，请核对")
			}
		}
		r.Status = InvoiceApproved
		r.Manual = true
		r.ReviewNote = note
	case "cancel":
		if !slices.Contains([]string{InvoicePendingPayment, InvoiceApproved, InvoiceSubmitting, InvoiceProcessing, InvoiceUnknown, InvoiceAttention, InvoiceCancelling}, r.Status) {
			return nil, invoiceConflict("当前状态不允许取消")
		}
		if !confirmNotIssued || note == "" {
			return nil, invoiceBad("请确认尚未开票、外部申请已结束，并填写取消原因")
		}
		r.Status = InvoiceCancelled
		r.Manual = true
		r.ReviewNote = note
	default:
		return nil, invoiceBad("不支持的操作")
	}
	if action == "approve" || action == "reject" {
		now := time.Now()
		r.ReviewedAt = &now
		r.ReviewedBy = actor
	}
	if err = saveInvoice(ctx, tx, r, strconv.FormatInt(actor, 10), action); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.get(ctx, id, user)
}

// Translate the database-level race guard into a useful payment API error.
func invoiceRefundError(err error) error {
	var p *pq.Error
	if errors.As(err, &p) && p.Constraint == "invoice_fee_refund_guard" {
		return invoiceConflict("请先驳回或取消对应发票申请后退还手续费")
	}
	if errors.As(err, &p) && p.Constraint == "invoice_refund_guard" {
		return invoiceConflict("订单已有发票申请，请先撤回或确认取消；已开票订单需人工处理")
	}
	return err
}
