package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/shopspring/decimal"
)

type InvoiceFeeTier struct {
	MinAmountCent int64 `json:"min_amount_cent"`
	RateBps       int64 `json:"rate_bps"`
}
type InvoiceFeeQuote struct {
	AmountCent     int64 `json:"amount_cent"`
	FeeCent        int64 `json:"fee_cent"`
	RateBps        int64 `json:"rate_bps"`
	PricingVersion int64 `json:"pricing_version"`
}

func validateInvoicePricing(minAmount int64, tiers []InvoiceFeeTier) error {
	if minAmount < 0 || minAmount > 9000000000000000 || len(tiers) == 0 || len(tiers) > 25 {
		return invoiceBad("请配置有效起开金额和1—25个手续费档位")
	}
	for i, t := range tiers {
		if t.MinAmountCent < minAmount || t.MinAmountCent > 9000000000000000 || t.RateBps < 0 || t.RateBps > 10000 {
			return invoiceBad("档位金额无效或费率不在0—100%范围内")
		}
		if i == 0 && t.MinAmountCent != minAmount || i > 0 && t.MinAmountCent <= tiers[i-1].MinAmountCent {
			return invoiceBad("第一档须从起开金额开始，后续档位须严格递增")
		}
	}
	return nil
}
func invoiceFeeQuote(cfg *InvoiceConfig, amount int64) (*InvoiceFeeQuote, error) {
	if err := validateInvoicePricing(cfg.MinAmountCent, cfg.FeeTiers); err != nil {
		return nil, err
	}
	if amount < cfg.MinAmountCent || amount <= 0 || amount > 9000000000000000 {
		return nil, invoiceBad("开票金额未达到起开金额或超出范围")
	}
	rate := cfg.FeeTiers[0].RateBps
	for _, tier := range cfg.FeeTiers {
		if amount >= tier.MinAmountCent {
			rate = tier.RateBps
		}
	}
	fee := decimal.NewFromInt(amount).Mul(decimal.NewFromInt(rate)).Div(decimal.NewFromInt(10000)).Ceil().IntPart()
	return &InvoiceFeeQuote{amount, fee, rate, cfg.PricingVersion}, nil
}
func scanInvoiceConfig(row invoiceScanner) (*InvoiceConfig, error) {
	c := &InvoiceConfig{}
	var tiers []byte
	err := row.Scan(&c.Enabled, &c.MinAmountCent, &tiers, &c.PricingVersion)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(tiers, &c.FeeTiers); err != nil {
		return nil, err
	}
	return c, nil
}

const invoiceConfigSelect = `SELECT enabled,min_amount_cent,fee_tiers,pricing_version FROM invoice_settings WHERE id=1`

func (s *InvoiceService) Quote(ctx context.Context, user int64, ids []int64) (*InvoiceFeeQuote, error) {
	if len(ids) == 0 || len(ids) > 100 {
		return nil, invoiceBad("每次请选择1—100笔订单")
	}
	ids = slices.Clone(ids)
	slices.Sort(ids)
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, invoiceBad("发票申请暂未开放")
	}
	var amount int64
	for i, id := range ids {
		if id <= 0 || i > 0 && id == ids[i-1] {
			return nil, invoiceBad("订单不能重复")
		}
		order, err := scanInvoiceOrder(s.db.QueryRowContext(ctx, invoiceOrderSelect+` WHERE `+invoiceEligible+` AND o.id=$2 AND NOT EXISTS(SELECT 1 FROM invoice_order_links l WHERE l.order_id=o.id AND l.active)`, user, id))
		if err == sql.ErrNoRows {
			return nil, invoiceBad("订单不可开票或已被其他申请占用")
		}
		if err != nil {
			return nil, err
		}
		if amount > 9000000000000000-order.AmountCent {
			return nil, invoiceBad("金额超出范围")
		}
		amount += order.AmountCent
	}
	return invoiceFeeQuote(cfg, amount)
}
