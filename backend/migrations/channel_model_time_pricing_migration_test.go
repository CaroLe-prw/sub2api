package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration234AddsChannelModelTimePricing(t *testing.T) {
	content, err := FS.ReadFile("234_channel_model_time_pricing.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS time_pricing JSONB NOT NULL DEFAULT '[]'::jsonb")
	require.Contains(t, sql, "jsonb_typeof(time_pricing) = 'array'")
	require.Contains(t, sql, "Asia/Shanghai")
}
