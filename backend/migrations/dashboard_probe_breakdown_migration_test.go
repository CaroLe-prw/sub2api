package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration233AddsDashboardProbeBreakdownColumns(t *testing.T) {
	content, err := FS.ReadFile("233_dashboard_probe_usage_breakdown.sql")
	require.NoError(t, err)

	sql := string(content)
	for _, table := range []string{"usage_dashboard_hourly", "usage_dashboard_daily"} {
		require.Contains(t, sql, "ALTER TABLE "+table)
	}
	for _, column := range []string{
		"probe_requests",
		"probe_input_tokens",
		"probe_output_tokens",
		"probe_cache_creation_tokens",
		"probe_cache_read_tokens",
		"probe_total_cost",
		"probe_account_cost",
		"probe_duration_ms",
	} {
		require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS "+column)
	}
}
