package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	paymentVerificationBatchSize = 10
	paymentVerificationTimeout   = 3 * time.Minute
	paymentVerificationActionTTL = 24 * time.Hour
)

// PaymentVerificationService only reconciles evidence. It never calls payment
// fulfillment, changes order payment state, deducts funds, or automatically bans.
type PaymentVerificationService struct {
	repo       PaymentVerificationRepository
	payments   *PaymentService
	ops        *OpsService
	authCache  APIKeyAuthCacheInvalidator
	lockCache  LeaderLockCache
	db         *sql.DB
	instanceID string
	configMu   sync.Mutex
	scanMu     sync.Mutex
	startOnce  sync.Once
	stopOnce   sync.Once
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewPaymentVerificationService(repo PaymentVerificationRepository, payments *PaymentService, ops *OpsService, authCache APIKeyAuthCacheInvalidator) *PaymentVerificationService {
	ctx, cancel := context.WithCancel(context.Background())
	return &PaymentVerificationService{repo: repo, payments: payments, ops: ops, authCache: authCache,
		instanceID: uuid.NewString(), ctx: ctx, cancel: cancel}
}

func (s *PaymentVerificationService) SetLeaderLock(lockCache LeaderLockCache, db *sql.DB) {
	s.lockCache, s.db = lockCache, db
}

func (s *PaymentVerificationService) Start() {
	if s == nil || s.repo == nil || s.payments == nil || s.ops == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			var lastRun time.Time
			for {
				cfg, err := s.GetConfig(s.ctx)
				if err == nil && cfg.Enabled && (lastRun.IsZero() || time.Since(lastRun) >= paymentVerificationInterval(cfg)) {
					lastRun = time.Now()
					if _, err := s.Scan(s.ctx); err != nil && s.ctx.Err() == nil {
						slog.Warn("payment verification scan failed")
					}
				}
				select {
				case <-s.ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (s *PaymentVerificationService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { s.cancel(); s.wg.Wait() })
}

func paymentVerificationInterval(cfg *PaymentVerificationConfig) time.Duration {
	minutes := cfg.IntervalMinutes
	if minutes < 1 {
		minutes = 5
	}
	if minutes > 60 {
		minutes = 60
	}
	return time.Duration(minutes) * time.Minute
}

func (s *PaymentVerificationService) Scan(ctx context.Context) (*PaymentVerificationScanResult, error) {
	result := &PaymentVerificationScanResult{}
	if s == nil || s.repo == nil || s.payments == nil || s.payments.entClient == nil {
		return nil, infraerrors.ServiceUnavailable("PAYMENT_VERIFICATION_UNAVAILABLE", "payment verification is unavailable")
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return result, nil
	}
	if !s.scanMu.TryLock() {
		return result, nil
	}
	defer s.scanMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, paymentVerificationTimeout)
	defer cancel()
	lockCtx, lockCancel := context.WithTimeout(ctx, 2*time.Second)
	release, acquired := tryAcquireSingletonLeaderLock(lockCtx, s.lockCache, s.db, "payment:verification:leader", s.instanceID, 5*time.Minute)
	lockCancel()
	if !acquired {
		return result, nil
	}
	defer release()
	now := time.Now().UTC()
	days := cfg.LookbackDays
	if days < 1 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	ids, err := s.repo.ListCandidates(ctx, now.Add(-time.Duration(days)*24*time.Hour), now, paymentVerificationBatchSize)
	if err != nil {
		return nil, fmt.Errorf("list payment verification candidates: %w", err)
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		record, err := s.checkOrder(ctx, id, time.Now().UTC())
		if err != nil {
			return result, err
		}
		if _, err := s.repo.SaveCheck(ctx, record, record.CheckedAt.Add(paymentVerificationInterval(cfg))); err != nil {
			return result, fmt.Errorf("save payment verification: %w", err)
		}
		result.Checked++
		switch record.Result {
		case PaymentVerificationVerified:
			result.Verified++
		case PaymentVerificationQueryError:
			result.Errors++
		default:
			result.Anomalies++
		}
	}
	if err := s.deliverPendingAlerts(ctx); err != nil {
		return result, err
	}
	return result, nil
}

func (s *PaymentVerificationService) ListAlerts(ctx context.Context, limit int) ([]PaymentVerificationRecord, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	return s.repo.ListAlerts(ctx, limit)
}

func (s *PaymentVerificationService) GetAlertByToken(ctx context.Context, token string) (*PaymentVerificationRecord, error) {
	if len(token) != 32 {
		return nil, ErrPaymentVerificationNotFound
	}
	record, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if record.State != PaymentVerificationPending || !record.ExpiresAt.After(time.Now()) {
		return nil, ErrPaymentVerificationChanged
	}
	return record, nil
}

func (s *PaymentVerificationService) ResolveAlert(ctx context.Context, id int64, action, actor string) (*PaymentVerificationRecord, error) {
	if action != "ban" && action != "ignore" {
		return nil, infraerrors.BadRequest("INVALID_ACTION", "action must be ban or ignore")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" || len(actor) > 100 {
		return nil, infraerrors.BadRequest("INVALID_ACTOR", "a valid operator is required")
	}
	record, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.State != PaymentVerificationPending {
		return nil, ErrPaymentVerificationChanged
	}
	if action == "ban" {
		// Never act on stale Telegram evidence. Only a fresh, conclusive mismatch
		// for this order permits a human-requested status change.
		fresh, err := s.checkOrder(ctx, record.OrderID, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		record, err = s.repo.SaveCheck(ctx, fresh, fresh.CheckedAt.Add(5*time.Minute))
		if err != nil {
			return nil, err
		}
		if fresh.Result == PaymentVerificationVerified || fresh.State == PaymentVerificationResolved {
			return record, ErrPaymentVerificationRecovered
		}
		if fresh.Result == PaymentVerificationQueryError {
			return record, ErrPaymentVerificationUnsafeBan
		}
		if record.State != PaymentVerificationPending {
			return nil, ErrPaymentVerificationChanged
		}
	}
	resolved, err := s.repo.Resolve(ctx, record.ID, record.CheckedAt, action, actor, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if action == "ban" && s.authCache != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		s.authCache.InvalidateAuthCacheByUserID(cacheCtx, resolved.UserID)
	}
	return resolved, nil
}

func paymentVerificationToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func paymentVerificationEligible(order *dbent.PaymentOrder) bool {
	return order != nil && order.Status == OrderStatusCompleted && order.OrderType == payment.OrderTypeBalance &&
		order.RefundAmount == 0 && order.RefundAt == nil && order.RefundRequestedAt == nil
}

func (s *PaymentVerificationService) checkOrder(ctx context.Context, id int64, now time.Time) (*PaymentVerificationRecord, error) {
	order, err := s.payments.entClient.PaymentOrder.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load payment verification order: %w", err)
	}
	token, err := paymentVerificationToken()
	if err != nil {
		return nil, fmt.Errorf("create payment verification token: %w", err)
	}
	record := &PaymentVerificationRecord{OrderID: order.ID, UserID: order.UserID, Email: order.UserEmail,
		OutTradeNo: order.OutTradeNo, Amount: order.Amount, PayAmount: order.PayAmount, Currency: PaymentOrderCurrency(order),
		OrderStatus: order.Status, Result: PaymentVerificationQueryError, State: PaymentVerificationPending,
		CheckedAt: now, CreatedAt: now, ActionToken: token, ExpiresAt: now.Add(paymentVerificationActionTTL)}
	if !paymentVerificationEligible(order) {
		record.State, record.Reason = PaymentVerificationResolved, "订单已退款或不再属于已完成余额充值，本次复核关闭"
		return record, nil
	}
	// Legacy merchant guessing is unacceptable for an enforcement decision.
	if !psHasPinnedProviderInstance(order) {
		record.Reason = "订单缺少原支付渠道绑定，无法核实"
		return record, nil
	}
	inst, err := s.payments.getOrderProviderInstance(ctx, order)
	if err != nil || inst == nil || inst.ProviderKey != payment.TypeEasyPay {
		record.Reason = "原易支付渠道不存在或绑定不一致，无法核实"
		return record, nil
	}
	record.ProviderName = inst.Name
	prov, err := s.payments.createProviderFromInstance(ctx, inst)
	if err != nil {
		record.Reason = "无法加载原易支付渠道配置"
		return record, nil
	}
	queryRef := strings.TrimSpace(order.OutTradeNo)
	if queryRef == "" {
		record.Reason = "订单缺少商户订单号，无法核实"
		return record, nil
	}
	resp, err := prov.QueryOrder(ctx, queryRef)
	if err != nil || resp == nil {
		// Do not store provider text: errors can contain credentials or gateway
		// payloads. All untyped errors are operational failures, not fraud proof.
		record.Reason = "支付平台查询失败，尚不能判断是否真实付款"
		return record, nil
	}
	if err := validateProviderNotificationMetadata(order, payment.TypeEasyPay, resp.Metadata); err != nil {
		record.Reason = "支付渠道商户信息不一致，需检查渠道配置"
		return record, nil
	}
	record.Result, record.Reason = paymentVerificationOutcome(order, resp)
	if isValidProviderAmount(resp.Amount) {
		amount := resp.Amount
		record.UpstreamAmount = &amount
	}
	if record.Result == PaymentVerificationVerified {
		record.State = PaymentVerificationResolved
	}
	return record, nil
}

func paymentVerificationOutcome(order *dbent.PaymentOrder, resp *payment.QueryOrderResponse) (string, string) {
	if resp == nil {
		return PaymentVerificationQueryError, "支付平台未返回有效查询结果"
	}
	if resp.Status != payment.ProviderStatusPaid {
		if resp.Status == payment.ProviderStatusPending && resp.Metadata["payment_status_known"] == "true" {
			return PaymentVerificationUnpaid, "本地已入账，但支付平台明确返回未支付"
		}
		return PaymentVerificationQueryError, "支付平台状态无法确认，需人工核实"
	}
	if !isValidProviderAmount(resp.Amount) || !isValidProviderAmount(order.PayAmount) {
		return PaymentVerificationQueryError, "支付金额数据无效，需人工核实"
	}
	if math.Abs(resp.Amount-order.PayAmount) > paymentAmountToleranceForCurrency(PaymentOrderCurrency(order)) {
		return PaymentVerificationAmountMismatch, "支付平台已付金额与本地订单应付金额不一致"
	}
	local, upstream := strings.TrimSpace(order.PaymentTradeNo), strings.TrimSpace(resp.TradeNo)
	if local != "" && local != order.OutTradeNo && upstream != "" && upstream != order.OutTradeNo && local != upstream {
		return PaymentVerificationTradeMismatch, "支付平台交易号与本地入账交易号不一致"
	}
	return PaymentVerificationVerified, "支付平台确认已支付，订单金额一致"
}
