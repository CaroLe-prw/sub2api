package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountGetQuotaUsageMultiplier(t *testing.T) {
	t.Parallel()

	accountRate := 0.033

	tests := []struct {
		name    string
		account *Account
		want    float64
	}{
		{
			name:    "nil account defaults to one",
			account: nil,
			want:    1,
		},
		{
			name: "legacy account falls back to account billing multiplier",
			account: &Account{
				RateMultiplier: &accountRate,
			},
			want: accountRate,
		},
		{
			name: "explicit upstream quota multiplier is independent",
			account: &Account{
				RateMultiplier: &accountRate,
				Extra: map[string]any{
					AccountQuotaUsageMultiplierExtraKey: 1.0,
				},
			},
			want: 1,
		},
		{
			name: "zero multiplier is allowed",
			account: &Account{
				RateMultiplier: &accountRate,
				Extra: map[string]any{
					AccountQuotaUsageMultiplierExtraKey: 0.0,
				},
			},
			want: 0,
		},
		{
			name: "invalid explicit value falls back safely",
			account: &Account{
				RateMultiplier: &accountRate,
				Extra: map[string]any{
					AccountQuotaUsageMultiplierExtraKey: math.NaN(),
				},
			},
			want: accountRate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.InDelta(t, tt.want, tt.account.GetQuotaUsageMultiplier(), 1e-12)
		})
	}
}

func TestValidateQuotaResetConfigRejectsInvalidUsageMultiplier(t *testing.T) {
	t.Parallel()

	for _, value := range []any{-0.1, math.NaN(), math.Inf(1), "not-a-number"} {
		err := ValidateQuotaResetConfig(map[string]any{
			AccountQuotaUsageMultiplierExtraKey: value,
		})
		require.EqualError(t, err, "quota_usage_multiplier must be a finite number >= 0")
	}

	require.NoError(t, ValidateQuotaResetConfig(map[string]any{
		AccountQuotaUsageMultiplierExtraKey: 1.0,
	}))
	require.NoError(t, ValidateQuotaResetConfig(map[string]any{
		AccountQuotaUsageMultiplierExtraKey: 0.0,
	}))
}

func TestBuildUsageBillingCommandUsesIndependentAccountQuotaMultiplier(t *testing.T) {
	t.Parallel()

	accountRate := 0.033
	account := &Account{
		ID:             3,
		Type:           AccountTypeAPIKey,
		RateMultiplier: &accountRate,
		Extra: map[string]any{
			"quota_daily_limit":                 500.0,
			AccountQuotaUsageMultiplierExtraKey: 1.0,
		},
	}
	params := &postUsageBillingParams{
		Cost:    &CostBreakdown{TotalCost: 500, ActualCost: 500},
		User:    &User{ID: 1},
		APIKey:  &APIKey{ID: 2},
		Account: account,
	}

	cmd := buildUsageBillingCommand("req-quota-multiplier", nil, params)

	require.NotNil(t, cmd)
	require.InDelta(t, 500, cmd.AccountQuotaCost, 1e-12)
	require.InDelta(t, accountRate, account.BillingRateMultiplier(), 1e-12)
}

func TestBuildAccountForCreateDefaultsQuotaUsageMultiplierToOne(t *testing.T) {
	t.Parallel()

	accountRate := 0.033
	account, err := buildAccountForCreate(&CreateAccountInput{
		Name:           "quota account",
		Platform:       PlatformOpenAI,
		Type:           AccountTypeAPIKey,
		Credentials:    map[string]any{"api_key": "test"},
		Extra:          map[string]any{"quota_daily_limit": 500.0},
		RateMultiplier: &accountRate,
	}, map[string]any{"quota_daily_limit": 500.0})

	require.NoError(t, err)
	require.Equal(t, 1.0, account.Extra[AccountQuotaUsageMultiplierExtraKey])
	require.InDelta(t, 1, account.GetQuotaUsageMultiplier(), 1e-12)
	require.InDelta(t, accountRate, account.BillingRateMultiplier(), 1e-12)
}
