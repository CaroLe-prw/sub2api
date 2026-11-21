//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func candyCleanupExtra() map[string]any {
	return map[string]any{
		service.CandyTestExtraKey:        map[string]any{"history": []any{map[string]any{"response_text": "answer 21"}}},
		service.CandyTestBlockedExtraKey: true,
		"unrelated_setting":              "keep",
	}
}

func insertCandyCleanupLog(t *testing.T, db sqlExecutor, id int64, component, message string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO ops_system_logs (level, component, message, account_id) VALUES ('warn', $1, $2, $3)`, component, message, id)
	require.NoError(t, err)
}

func assertCandyCleanupLogCount(t *testing.T, db sqlQueryer, id int64, component string, want int) {
	t.Helper()
	var count int
	require.NoError(t, scanSingleRow(context.Background(), db, `SELECT COUNT(*) FROM ops_system_logs WHERE account_id = $1 AND component = $2`, []any{id, component}, &count))
	require.Equal(t, want, count)
}

func assertCandyCleanupExtra(t *testing.T, db sqlQueryer, id int64, deleted bool) {
	t.Helper()
	var raw []byte
	var deletedAt sql.NullTime
	require.NoError(t, scanSingleRow(context.Background(), db, `SELECT extra, deleted_at FROM accounts WHERE id = $1`, []any{id}, &raw, &deletedAt))
	var extra map[string]any
	require.NoError(t, json.Unmarshal(raw, &extra))
	require.Equal(t, deleted, deletedAt.Valid)
	require.Equal(t, "keep", extra["unrelated_setting"])
	if deleted {
		require.NotContains(t, extra, service.CandyTestExtraKey)
		require.NotContains(t, extra, service.CandyTestBlockedExtraKey)
	} else {
		require.Contains(t, extra, service.CandyTestExtraKey)
		require.Equal(t, true, extra[service.CandyTestBlockedExtraKey])
	}
}

func TestAccountCandyCleanupPreservesOtherData(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	repo := newAccountRepositoryWithSQL(tx.Client(), tx, nil)
	deleted := mustCreateAccount(t, tx.Client(), &service.Account{Name: "candy-cleanup-deleted", Extra: candyCleanupExtra()})
	active := mustCreateAccount(t, tx.Client(), &service.Account{Name: "candy-cleanup-active", Extra: candyCleanupExtra()})
	insertCandyCleanupLog(t, tx, deleted.ID, "service.account_test", "account_test.failed")
	insertCandyCleanupLog(t, tx, active.ID, "service.account_test", "account_test.failed")
	insertCandyCleanupLog(t, tx, deleted.ID, "openai.gateway", "upstream.failed")
	insertCandyCleanupLog(t, tx, deleted.ID, "audit", "account.deleted")

	require.NoError(t, repo.Delete(ctx, deleted.ID))
	assertCandyCleanupExtra(t, tx, deleted.ID, true)
	assertCandyCleanupExtra(t, tx, active.ID, false)
	assertCandyCleanupLogCount(t, tx, deleted.ID, "service.account_test", 0)
	assertCandyCleanupLogCount(t, tx, active.ID, "service.account_test", 1)
	assertCandyCleanupLogCount(t, tx, deleted.ID, "openai.gateway", 1)
	assertCandyCleanupLogCount(t, tx, deleted.ID, "audit", 1)
	require.ErrorIs(t, repo.UpdateExtra(ctx, deleted.ID, candyCleanupExtra()), service.ErrAccountNotFound)
	assertCandyCleanupExtra(t, tx, deleted.ID, true)
}

func TestAccountCandyCleanupSerializesLateLogs(t *testing.T) {
	ctx := context.Background()
	account := mustCreateAccount(t, integrationEntClient, &service.Account{Name: "candy-cleanup-locks", Extra: candyCleanupExtra()})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM ops_system_logs WHERE account_id = $1`, account.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id = $1`, account.ID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, account.ID)
	})
	logTx := testTx(t)
	input := &service.OpsInsertSystemLogInput{Level: "warn", Component: "service.account_test", Message: "account_test.failed", AccountID: &account.ID}
	active, err := lockActiveAccountTestLogAccounts(ctx, logTx, []*service.OpsInsertSystemLogInput{input})
	require.NoError(t, err)
	require.Contains(t, active, account.ID)

	// FOR SHARE must serialize the non-key UPDATE used by soft deletion.
	deleteTx := testTx(t)
	_, err = deleteTx.ExecContext(ctx, `SET LOCAL lock_timeout = '100ms'`)
	require.NoError(t, err)
	_, err = deleteTx.ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, account.ID)
	var lockErr *pq.Error
	require.ErrorAs(t, err, &lockErr)
	require.Equal(t, pq.ErrorCode("55P03"), lockErr.Code)
	require.NoError(t, deleteTx.Rollback())

	insertCandyCleanupLog(t, logTx, account.ID, "service.account_test", "account_test.failed")
	require.NoError(t, logTx.Commit())
	repo := newAccountRepositoryWithSQL(integrationEntClient, integrationDB, nil)
	require.NoError(t, repo.Delete(ctx, account.ID))
	assertCandyCleanupLogCount(t, integrationDB, account.ID, "service.account_test", 0)

	ops := NewOpsRepository(integrationDB).(*opsRepository)
	inserted, err := ops.BatchInsertSystemLogs(ctx, []*service.OpsInsertSystemLogInput{input})
	require.NoError(t, err)
	require.Zero(t, inserted, "queued failures must not recreate a deleted account's logs")
	assertCandyCleanupLogCount(t, integrationDB, account.ID, "service.account_test", 0)
	assertCandyCleanupExtra(t, integrationDB, account.ID, true)
}

func TestAccountCandyCleanupBackfillIsIdempotent(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	deleted := mustCreateAccount(t, tx.Client(), &service.Account{Name: "candy-backfill-deleted", Extra: candyCleanupExtra()})
	active := mustCreateAccount(t, tx.Client(), &service.Account{Name: "candy-backfill-active", Extra: candyCleanupExtra()})
	_, err := tx.ExecContext(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1`, deleted.ID)
	require.NoError(t, err)
	insertCandyCleanupLog(t, tx, deleted.ID, "service.account_test", "account_test.failed")
	insertCandyCleanupLog(t, tx, active.ID, "service.account_test", "account_test.failed")
	const missingAccount = int64(-255)
	insertCandyCleanupLog(t, tx, missingAccount, "service.account_test", "account_test.failed")
	insertCandyCleanupLog(t, tx, deleted.ID, "openai.gateway", "upstream.failed")
	insertCandyCleanupLog(t, tx, deleted.ID, "audit", "account.deleted")
	migration, err := migrations.FS.ReadFile("255_deleted_account_candy_cleanup.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = tx.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		assertCandyCleanupExtra(t, tx, deleted.ID, true)
		assertCandyCleanupExtra(t, tx, active.ID, false)
		assertCandyCleanupLogCount(t, tx, deleted.ID, "service.account_test", 0)
		assertCandyCleanupLogCount(t, tx, missingAccount, "service.account_test", 0)
		assertCandyCleanupLogCount(t, tx, active.ID, "service.account_test", 1)
		assertCandyCleanupLogCount(t, tx, deleted.ID, "openai.gateway", 1)
		assertCandyCleanupLogCount(t, tx, deleted.ID, "audit", 1)
	}
}
