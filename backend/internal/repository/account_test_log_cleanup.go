package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const deleteAccountCandyExtraSQL = `UPDATE accounts
SET extra = COALESCE(extra, '{}'::jsonb) - 'candy_test' - 'candy_test_blocked'
WHERE id = $1 AND deleted_at IS NOT NULL
  AND (extra ? 'candy_test' OR extra ? 'candy_test_blocked')`

// Candy checks share this failure event with connection tests. Preserve all
// business-request and audit events; only this account's test failures go away.
const deleteAccountTestLogsSQL = `DELETE FROM ops_system_logs
WHERE account_id = $1 AND component = 'service.account_test' AND message = 'account_test.failed'`

const lockActiveAccountTestLogAccountsSQL = `SELECT id FROM accounts
WHERE id = ANY($1) AND deleted_at IS NULL ORDER BY id FOR SHARE`

func isAccountTestFailureLog(input *service.OpsInsertSystemLogInput) bool {
	return input != nil && input.AccountID != nil &&
		strings.TrimSpace(input.Component) == "service.account_test" &&
		strings.TrimSpace(input.Message) == "account_test.failed"
}

func lockActiveAccountTestLogAccounts(ctx context.Context, tx *sql.Tx, inputs []*service.OpsInsertSystemLogInput) (active map[int64]struct{}, err error) {
	unique := make(map[int64]struct{})
	for _, input := range inputs {
		if isAccountTestFailureLog(input) && *input.AccountID > 0 {
			unique[*input.AccountID] = struct{}{}
		}
	}
	if len(unique) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, err := tx.QueryContext(ctx, lockActiveAccountTestLogAccountsSQL, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()
	active = make(map[int64]struct{}, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		active[id] = struct{}{}
	}
	return active, rows.Err()
}
