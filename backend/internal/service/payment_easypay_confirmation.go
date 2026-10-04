package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
)

// confirmEasyPayUpstream treats the callback as a hint only. The endpoint,
// merchant credentials and query reference all come from the persisted order.
func (s *PaymentService) confirmEasyPayUpstream(ctx context.Context, order *dbent.PaymentOrder, callbackTradeNo string) (*payment.QueryOrderResponse, error) {
	prov, err := s.getOrderProvider(ctx, order)
	if err != nil {
		return nil, fmt.Errorf("easypay confirmation provider: %w", err)
	}
	if prov.ProviderKey() != payment.TypeEasyPay {
		return nil, fmt.Errorf("easypay confirmation provider mismatch")
	}
	queryRef := strings.TrimSpace(order.OutTradeNo)
	if queryRef == "" {
		// Pre-out_trade_no orders used the server-generated sub2_<database ID>.
		queryRef = fmt.Sprintf("%s%d", orderIDPrefix, order.ID)
	}
	finish := servertiming.ObserveDependency(ctx, "payment")
	resp, err := prov.QueryOrder(ctx, queryRef)
	finish()
	if err != nil {
		return nil, fmt.Errorf("easypay confirmation query: %w", err)
	}
	if resp == nil || resp.Status != payment.ProviderStatusPaid {
		return nil, fmt.Errorf("easypay upstream has not confirmed payment")
	}
	if !isValidProviderAmount(resp.Amount) || math.Abs(resp.Amount-order.PayAmount) > paymentAmountToleranceForCurrency(PaymentOrderCurrency(order)) {
		return nil, fmt.Errorf("easypay confirmation amount mismatch")
	}
	if err := validateProviderNotificationMetadata(order, payment.TypeEasyPay, resp.Metadata); err != nil {
		return nil, fmt.Errorf("easypay confirmation metadata: %w", err)
	}
	// Some legacy gateways omit trade_no; QueryOrder then returns queryRef.
	upstreamTradeNo := strings.TrimSpace(resp.TradeNo)
	if upstreamTradeNo != "" && upstreamTradeNo != queryRef && strings.TrimSpace(callbackTradeNo) != "" && upstreamTradeNo != strings.TrimSpace(callbackTradeNo) {
		return nil, fmt.Errorf("easypay confirmation trade number mismatch")
	}
	if upstreamTradeNo == "" || upstreamTradeNo == queryRef {
		resp.TradeNo = callbackTradeNo
	}
	return resp, nil
}
