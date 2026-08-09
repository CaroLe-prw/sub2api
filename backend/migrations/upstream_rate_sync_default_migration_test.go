package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration197EnablesRateSyncForGenericProbeAccounts(t *testing.T) {
	content, err := FS.ReadFile("197_enable_upstream_rate_sync_with_probe.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, `"upstream_billing_probe_enabled": true`)
	require.Contains(t, sql, `"newapi_sync_enabled": true`)
	require.Contains(t, sql, "upstream_billing_rate_sync_enabled")
	require.Contains(t, sql, "IS DISTINCT FROM 'true'::jsonb")
}
