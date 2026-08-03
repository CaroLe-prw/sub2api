package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateMaxAccountCostMultiplier(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{-0.001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		value := value
		require.EqualError(
			t,
			validateMaxAccountCostMultiplier(&value),
			"max_account_cost_multiplier must be a finite number >= 0",
		)
	}

	zero := 0.0
	limit := 0.05
	require.NoError(t, validateMaxAccountCostMultiplier(nil))
	require.NoError(t, validateMaxAccountCostMultiplier(&zero))
	require.NoError(t, validateMaxAccountCostMultiplier(&limit))
}
