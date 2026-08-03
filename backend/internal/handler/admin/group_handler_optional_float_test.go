package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionalNullableFloatFieldTracksMissingValueAndNull(t *testing.T) {
	t.Parallel()

	var missing UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &missing))
	require.False(t, missing.MaxAccountCostMultiplier.IsSet())
	require.Nil(t, missing.MaxAccountCostMultiplier.Value())

	var cleared UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"max_account_cost_multiplier":null}`), &cleared))
	require.True(t, cleared.MaxAccountCostMultiplier.IsSet())
	require.Nil(t, cleared.MaxAccountCostMultiplier.Value())

	var explicit UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"max_account_cost_multiplier":0.05}`), &explicit))
	require.True(t, explicit.MaxAccountCostMultiplier.IsSet())
	require.NotNil(t, explicit.MaxAccountCostMultiplier.Value())
	require.InDelta(t, 0.05, *explicit.MaxAccountCostMultiplier.Value(), 1e-12)
}
