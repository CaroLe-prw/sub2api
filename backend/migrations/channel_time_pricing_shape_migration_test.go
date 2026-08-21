package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelTimePricingShapeMigrationWrapsOnlyLegacyArrays(t *testing.T) {
	content, err := FS.ReadFile("234_normalize_channel_time_pricing_shape.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "UPDATE channel_model_pricing")
	require.Contains(t, sql, "SET time_pricing = jsonb_build_object( 'timezone', 'Asia/Shanghai', 'periods', time_pricing )")
	require.Contains(t, sql, "jsonb_typeof(time_pricing) = 'array'")
	require.NotContains(t, sql, "jsonb_typeof(time_pricing) = 'object'")
}
