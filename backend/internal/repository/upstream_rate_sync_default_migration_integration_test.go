//go:build integration

package repository

import (
	"context"
	"testing"

	dbmigrations "github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestMigration197EnablesRateSyncOnlyForGenericProbeAccounts(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	migrationSQL, err := dbmigrations.FS.ReadFile("197_enable_upstream_rate_sync_with_probe.sql")
	require.NoError(t, err)

	insertAccount := func(name, extra string) int64 {
		var id int64
		require.NoError(t, tx.QueryRowContext(ctx, `
INSERT INTO accounts (name, platform, type, extra)
VALUES ($1, 'openai', 'apikey', $2::jsonb)
RETURNING id
`, name, extra).Scan(&id))
		return id
	}

	genericID := insertAccount("migration-197-generic", `{
  "upstream_billing_probe_enabled": true,
  "upstream_billing_rate_sync_enabled": false
}`)
	newAPIID := insertAccount("migration-197-newapi", `{
  "upstream_billing_probe_enabled": true,
  "upstream_billing_rate_sync_enabled": false,
  "newapi_sync_enabled": true
}`)
	disabledID := insertAccount("migration-197-disabled", `{
  "upstream_billing_probe_enabled": false,
  "upstream_billing_rate_sync_enabled": false
}`)

	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)

	readRateSync := func(id int64) bool {
		var enabled bool
		require.NoError(t, tx.QueryRowContext(ctx, `
SELECT (extra ->> 'upstream_billing_rate_sync_enabled')::boolean
FROM accounts
WHERE id = $1
`, id).Scan(&enabled))
		return enabled
	}

	require.True(t, readRateSync(genericID))
	require.False(t, readRateSync(newAPIID))
	require.False(t, readRateSync(disabledID))
}
