package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration229AllowsProbeRequestTypeAndNullableSystemIdentity(t *testing.T) {
	content, err := FS.ReadFile("229_usage_logs_system_probe_identity.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "ALTER COLUMN user_id DROP NOT NULL")
	require.Contains(t, sql, "ALTER COLUMN api_key_id DROP NOT NULL")
	require.Contains(t, sql, "FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL")
	require.Contains(t, sql, "FOREIGN KEY (api_key_id) REFERENCES api_keys(id) ON DELETE SET NULL")
	require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS usage_logs_request_type_check")
	require.Contains(t, sql, "ADD CONSTRAINT usage_logs_request_type_check")
	require.Contains(t, sql, "CHECK (request_type >= 0 AND request_type <= 6)")
}
