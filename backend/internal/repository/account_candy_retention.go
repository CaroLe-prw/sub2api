package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const listCandyTestHistorySQL = `SELECT id, extra->'candy_test' FROM accounts
WHERE id > $1 AND deleted_at IS NULL AND jsonb_typeof(extra->'candy_test') = 'object'
ORDER BY id LIMIT $2`

// Compare-and-set protects concurrent test results; jsonb_set preserves changes
// to unrelated extra fields. No eligibility changes, so no scheduler outbox work.
const updateCandyTestHistorySQL = `UPDATE accounts
SET extra = jsonb_set(extra, '{candy_test}', $1::jsonb, false)
WHERE id = $2 AND deleted_at IS NULL AND extra->'candy_test' = $3::jsonb`

// These failure events are shared by candy and connection tests. Never delete
// request/billing/audit logs. tableoid also makes this safe with partitioned logs.
const cleanupAccountTestLogsSQL = `DELETE FROM ops_system_logs
WHERE (tableoid, ctid) IN (
    SELECT tableoid, ctid FROM ops_system_logs
    WHERE created_at < $1 AND account_id IS NOT NULL
      AND component = 'service.account_test' AND message = 'account_test.failed'
    ORDER BY created_at, id LIMIT $2 FOR UPDATE SKIP LOCKED
)`

func (r *accountRepository) CleanupCandyTestHistory(ctx context.Context, before time.Time, afterID int64, limit int) (int64, int64, error) {
	if limit <= 0 {
		return afterID, 0, fmt.Errorf("candy cleanup batch size must be positive")
	}
	rows, err := r.sql.QueryContext(ctx, listCandyTestHistorySQL, afterID, limit)
	if err != nil {
		return afterID, 0, err
	}
	defer rows.Close()
	type entry struct {
		id  int64
		raw []byte
	}
	batch := make([]entry, 0, limit)
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.id, &e.raw); err != nil {
			return afterID, 0, err
		}
		batch = append(batch, e)
	}
	if err := rows.Err(); err != nil {
		return afterID, 0, err
	}
	// Release the read connection before writes, even with a single-connection pool.
	if err := rows.Close(); err != nil {
		return afterID, 0, err
	}
	var cleaned int64
	for _, e := range batch {
		if err := ctx.Err(); err != nil {
			return afterID, cleaned, err
		}
		afterID = e.id
		trimmed, err := service.PruneCandyTestHistory(e.raw, before)
		if err != nil {
			// One malformed legacy row must not block every other account.
			logger.LegacyPrintf("service.candy_test_cleanup", "[CandyTest] skip invalid history account_id=%d: %v", e.id, err)
			continue
		}
		if trimmed == nil {
			continue
		}
		result, err := r.sql.ExecContext(ctx, updateCandyTestHistorySQL, string(trimmed), e.id, string(e.raw))
		if err != nil {
			return afterID, cleaned, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return afterID, cleaned, err
		}
		cleaned += affected
	}
	return afterID, cleaned, nil
}

func (r *accountRepository) CleanupAccountTestLogs(ctx context.Context, before time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("candy cleanup batch size must be positive")
	}
	result, err := r.sql.ExecContext(ctx, cleanupAccountTestLogsSQL, before, limit)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
