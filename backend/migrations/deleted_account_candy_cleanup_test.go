package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeletedAccountCandyCleanupMigrationIsScoped(t *testing.T) {
	content, err := FS.ReadFile("255_deleted_account_candy_cleanup.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "- 'candy_test' - 'candy_test_blocked'")
	require.Contains(t, sql, "WHERE deleted_at IS NOT NULL")
	require.Contains(t, sql, "l.account_id IS NOT NULL")
	require.Contains(t, sql, "l.component = 'service.account_test'")
	require.Contains(t, sql, "l.message = 'account_test.failed'")
	require.Contains(t, sql, "NOT EXISTS ( SELECT 1 FROM accounts a WHERE a.id = l.account_id AND a.deleted_at IS NULL )")
	require.NotContains(t, sql, "DELETE FROM accounts")
	require.NotContains(t, sql, "TRUNCATE")
}
