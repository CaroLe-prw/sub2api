//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNormalizeChannelPriceCurrency(t *testing.T) {
	for _, tt := range []struct{ input, want string }{{"", "USD"}, {"usd", "USD"}, {" rmb ", "CNY"}, {"CNY", "CNY"}, {"eur", "EUR"}, {"JPY", "JPY"}} {
		t.Run(tt.input, func(t *testing.T) {
			got, err := NormalizeChannelPriceCurrency(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
	_, err := NormalizeChannelPriceCurrency("invalid")
	require.Error(t, err)
}

func TestChannelCurrencyCreateAndUpdate(t *testing.T) {
	for _, currency := range []string{"", "RMB", "EUR"} {
		t.Run(currency, func(t *testing.T) {
			var saved *Channel
			repo := &mockChannelRepository{
				createFn:  func(_ context.Context, ch *Channel) error { ch.ID = 1; saved = ch; return nil },
				getByIDFn: func(_ context.Context, _ int64) (*Channel, error) { return saved, nil },
			}
			svc := NewChannelService(repo, nil, nil, nil, nil)
			created, err := svc.Create(context.Background(), &CreateChannelInput{Name: "currency-test", PriceCurrency: currency})
			require.NoError(t, err)
			expected, _ := NormalizeChannelPriceCurrency(currency)
			require.Equal(t, expected, created.PriceCurrency)
		})
	}
	price := 2e-6
	ch := &Channel{PriceCurrency: "CNY", ModelPricing: []ChannelModelPricing{{InputPrice: &price}}}
	svc := &ChannelService{}
	require.NoError(t, svc.applyUpdateInput(context.Background(), ch, &UpdateChannelInput{}))
	require.Equal(t, "CNY", ch.PriceCurrency)
	currency := "EUR"
	require.NoError(t, svc.applyUpdateInput(context.Background(), ch, &UpdateChannelInput{PriceCurrency: &currency}))
	require.Equal(t, "EUR", ch.PriceCurrency)
	require.Equal(t, 2e-6, *ch.ModelPricing[0].InputPrice)
	currency = "INVALID"
	require.Error(t, svc.applyUpdateInput(context.Background(), ch, &UpdateChannelInput{PriceCurrency: &currency}))
	require.Equal(t, "EUR", ch.PriceCurrency)
}

func TestListAvailable_NativeCurrencyNeverUsesUSDFallback(t *testing.T) {
	for _, currency := range []string{"USD", "CNY", "EUR"} {
		t.Run(currency, func(t *testing.T) {
			price := 2e-6
			svc := newAvailableChannelService([]Channel{{ID: 1, Name: "quotes", Status: StatusActive, PriceCurrency: currency,
				ModelPricing: []ChannelModelPricing{
					{Platform: "openai", Models: []string{"configured"}, BillingMode: BillingModeToken, InputPrice: &price},
					{Platform: "openai", Models: []string{"unpriced"}, BillingMode: BillingModeToken},
				},
			}}, &stubGroupRepoForAvailable{})
			svc.pricingService = newStubPricingServiceFromMap(map[string]*LiteLLMModelPricing{
				"unpriced": {Mode: "chat", InputCostPerToken: 5e-6},
			})
			result, err := svc.ListAvailable(context.Background())
			require.NoError(t, err)
			require.Len(t, result, 1)
			require.Equal(t, currency, result[0].PriceCurrency)
			require.Len(t, result[0].SupportedModels, 2)
			for _, model := range result[0].SupportedModels {
				if model.Name == "configured" {
					require.Equal(t, price, *model.Pricing.InputPrice)
				} else if currency == "USD" {
					require.NotNil(t, model.Pricing)
					require.NotNil(t, model.Pricing.InputPrice)
					require.Equal(t, 5e-6, *model.Pricing.InputPrice)
				} else {
					require.True(t, model.Pricing == nil || model.Pricing.InputPrice == nil)
				}
			}
		})
	}
}
