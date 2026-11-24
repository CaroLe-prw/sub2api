//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCandyRetentionPostgresPreservesAccountState(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `CREATE TEMP TABLE accounts (
        id BIGINT PRIMARY KEY, extra JSONB, deleted_at TIMESTAMPTZ,
        schedulable BOOLEAN DEFAULT true, updated_at TIMESTAMPTZ DEFAULT '2026-11-01T00:00:00Z'
    ) ON COMMIT DROP;
    INSERT INTO accounts (id, extra) VALUES
        (1, '{"keep":"setting","candy_test_blocked":true,"candy_test":{"total":3,"failed":2,"errors":1,"recent":[{"status":"failed","checked_at":"2026-11-21T00:00:00Z","response_text":"old1"},{"status":"failed","checked_at":"2026-11-22T00:00:00Z","response_text":"old2"},{"status":"error","checked_at":"2026-11-23T00:00:00Z"}],"schedules":{"7":{"model":"test","checked_at":"2026-11-23T00:00:00Z"}}}}'),
        (2, '{"keep":"disabled","candy_test":{"total":1,"passed":1,"recent":[{"status":"passed","checked_at":"2026-11-20T00:00:00Z"}]}}'),
        (3, '{"keep":"no history"}');
    UPDATE accounts SET schedulable = false WHERE id = 2;
    `)
	require.NoError(t, err)
	repo := &accountRepository{sql: tx}
	cutoff := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	last, count, err := repo.CleanupCandyTestHistory(ctx, cutoff, 0, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), last)
	require.Equal(t, int64(1), count)
	next, count, err := repo.CleanupCandyTestHistory(ctx, cutoff, last, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), next)
	require.Equal(t, int64(1), count, "disabled accounts must also expire old answers")
	last, count, err = repo.CleanupCandyTestHistory(ctx, cutoff, next, 1)
	require.NoError(t, err)
	require.Equal(t, next, last)
	require.Zero(t, count)
	_, count, err = repo.CleanupCandyTestHistory(ctx, cutoff, 0, 100)
	require.NoError(t, err)
	require.Zero(t, count, "retention must be idempotent")

	var raw []byte
	var enabled bool
	var updatedAt time.Time
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra, schedulable, updated_at FROM accounts WHERE id = 1`).Scan(&raw, &enabled, &updatedAt))
	require.True(t, enabled)
	require.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), updatedAt.UTC())
	var extra map[string]any
	require.NoError(t, json.Unmarshal(raw, &extra))
	require.Equal(t, "setting", extra["keep"])
	require.Equal(t, true, extra[service.CandyTestBlockedExtraKey])
	snapshot := extra[service.CandyTestExtraKey].(map[string]any)
	require.Equal(t, float64(3), snapshot["total"])
	require.Equal(t, float64(2), snapshot["consecutive_failures"])
	require.Equal(t, "failed", snapshot["last_verdict"])
	require.Len(t, snapshot["recent"], 1)
	require.Len(t, snapshot["schedules"], 1)
	require.NotContains(t, string(raw), "old1")
	require.NotContains(t, string(raw), "old2")
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT schedulable FROM accounts WHERE id = 2`).Scan(&enabled))
	require.False(t, enabled)
}

func TestCandyRetentionPostgresCompareAndSet(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `CREATE TEMP TABLE accounts (id BIGINT PRIMARY KEY, extra JSONB, deleted_at TIMESTAMPTZ) ON COMMIT DROP`)
	require.NoError(t, err)
	old := `{"total":1,"passed":1,"recent":[{"status":"passed","checked_at":"2026-11-20T00:00:00Z"}]}`
	trimmed, err := service.PruneCandyTestHistory([]byte(old), time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO accounts VALUES (1, jsonb_build_object('candy_test', $1::jsonb), NULL), (2, jsonb_build_object('candy_test', $1::jsonb), NULL), (3, jsonb_build_object('candy_test', $1::jsonb), NOW())`, old)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra = jsonb_set(extra, '{candy_test,total}', '2') WHERE id = 1;
        UPDATE accounts SET extra = extra || '{"concurrent_setting":"keep"}' WHERE id = 2`)
	require.NoError(t, err)
	for _, tc := range []struct{ id, want int64 }{{1, 0}, {2, 1}, {3, 0}} {
		result, err := tx.ExecContext(ctx, updateCandyTestHistorySQL, string(trimmed), tc.id, old)
		require.NoError(t, err)
		count, err := result.RowsAffected()
		require.NoError(t, err)
		require.Equal(t, tc.want, count)
	}
	var kept string
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT extra->>'concurrent_setting' FROM accounts WHERE id = 2`).Scan(&kept))
	require.Equal(t, "keep", kept)
}

func TestCandyRetentionPostgresDeletesOnlyExpiredTestLogsAcrossPartitions(t *testing.T) {
	ctx := context.Background()
	tx := testTx(t)
	_, err := tx.ExecContext(ctx, `CREATE TEMP TABLE ops_system_logs (
        id BIGINT, created_at TIMESTAMPTZ, account_id BIGINT, component TEXT, message TEXT
    ) PARTITION BY RANGE (created_at);
    CREATE TEMP TABLE candy_logs_nov PARTITION OF ops_system_logs FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
    CREATE TEMP TABLE candy_logs_dec PARTITION OF ops_system_logs FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');
    INSERT INTO ops_system_logs VALUES
        (1, '2026-11-22T00:00:00Z', 42, 'service.account_test', 'account_test.failed'),
        (2, '2026-11-23T00:00:00Z', 42, 'service.account_test', 'account_test.failed'),
        (3, '2026-11-22T00:00:00Z', 42, 'openai.gateway', 'account_test.failed'),
        (4, '2026-11-22T00:00:00Z', 42, 'service.account_test', 'another.event'),
        (5, '2026-11-22T00:00:00Z', NULL, 'service.account_test', 'account_test.failed'),
        (6, '2026-11-22T01:00:00Z', 43, 'service.account_test', 'account_test.failed'),
        (7, '2026-11-22T00:00:00Z', 42, 'audit', 'account.deleted'),
        (100, '2026-12-01T00:00:00Z', 42, 'service.account_test', 'account_test.failed');`)
	require.NoError(t, err)
	var sameCTID bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT a.ctid = b.ctid FROM ops_system_logs a, ops_system_logs b WHERE a.id = 1 AND b.id = 100`).Scan(&sameCTID))
	require.True(t, sameCTID, "fixture must exercise a cross-partition ctid collision")
	repo := &accountRepository{sql: tx}
	cutoff := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	for _, want := range []int64{1, 1, 0} {
		deleted, err := repo.CleanupAccountTestLogs(ctx, cutoff, 1)
		require.NoError(t, err)
		require.Equal(t, want, deleted)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM ops_system_logs ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []int64{2, 3, 4, 5, 7, 100}, ids)
}
