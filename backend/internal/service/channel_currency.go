package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const DefaultChannelPriceCurrency = "USD"

// NormalizeChannelPriceCurrency normalizes price metadata only. It never changes
// price amounts or converts currencies. Legacy channels without a code use USD.
func NormalizeChannelPriceCurrency(currency string) (string, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return DefaultChannelPriceCurrency, nil
	}
	if currency == "RMB" {
		currency = "CNY"
	}
	switch currency {
	case "USD", "CNY", "EUR", "GBP", "JPY", "HKD", "SGD", "AUD", "CAD", "CHF", "KRW":
		return currency, nil
	default:
		return "", infraerrors.BadRequest("INVALID_CHANNEL_PRICE_CURRENCY", "unsupported channel price currency")
	}
}

func (c *Channel) EffectivePriceCurrency() string {
	if c == nil || c.PriceCurrency == "" {
		return DefaultChannelPriceCurrency
	}
	return c.PriceCurrency
}
