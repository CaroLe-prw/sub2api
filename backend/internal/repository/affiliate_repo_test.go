package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAffiliateUserOverviewSQLIncludesMaturedFrozenQuota(t *testing.T) {
	query := strings.Join(strings.Fields(affiliateUserOverviewSQL), " ")

	require.Contains(t, query, "ua.aff_quota + COALESCE(matured.matured_frozen_quota, 0)")
	require.Contains(t, query, "frozen_until <= NOW()")
}

func TestAffiliateRecordQueriesUseLedgerAuditFields(t *testing.T) {
	source, err := os.ReadFile("affiliate_repo.go")
	require.NoError(t, err)
	content := string(source)

	require.Contains(t, content, "LEFT JOIN payment_orders po ON po.id = ual.source_order_id")
	require.Contains(t, content, "CASE WHEN ual.source_order_id IS NULL THEN 'non_order_recharge'")
	require.Contains(t, content, "COALESCE(ual.source_amount, po.amount)::double precision")
	require.NotContains(t, content, "AND ual.source_order_id IS NOT NULL`")
	require.Contains(t, content, "ual.amount::double precision")
	require.Contains(t, content, "ual.balance_after::double precision")
	require.NotContains(t, content, "parseAffiliateRebateAmount")
	require.NotContains(t, content, `"current_balance": "u.balance"`)
}

func TestListAffiliateRebateRecordsIncludesNonOrderRecharge(t *testing.T) {
	db, mock := newSQLMock(t)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := &affiliateRepository{client: client}

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\).*LEFT JOIN payment_orders po.*WHERE ual.action = 'accrue'`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(1)))
	createdAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)SELECT CASE WHEN ual.source_order_id IS NULL THEN 'non_order_recharge'.*LIMIT \$1 OFFSET \$2`).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"source_type", "order_id", "out_trade_no", "inviter_id", "inviter_email", "inviter_username",
			"invitee_id", "invitee_email", "invitee_username", "order_amount", "pay_amount", "rebate_amount",
			"payment_type", "order_status", "created_at",
		}).AddRow(
			"non_order_recharge", nil, "", int64(11), "inviter@example.com", "inviter",
			int64(22), "invitee@example.com", "invitee", 12.5, nil, 2.5, "", "", createdAt,
		))

	records, total, err := repo.ListAffiliateRebateRecords(context.Background(), service.AffiliateRecordFilter{
		Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, "non_order_recharge", records[0].SourceType)
	require.Nil(t, records[0].OrderID)
	require.NotNil(t, records[0].OrderAmount)
	require.Equal(t, 12.5, *records[0].OrderAmount)
	require.Equal(t, 2.5, records[0].RebateAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestAffiliateRebateRecordsQueryKeepsNonOrderAccruals 锁定返利记录列出全部
// accrue 流水：订单与被邀请人均为 LEFT JOIN，且不按 source_order_id 过滤，
// 兑换码、管理员充值来源的返利才会出现在明细里。
func TestAffiliateRebateRecordsQueryKeepsNonOrderAccruals(t *testing.T) {
	source, err := os.ReadFile("affiliate_repo.go")
	require.NoError(t, err)
	content := string(source)

	require.Contains(t, content, "LEFT JOIN payment_orders po ON po.id = ual.source_order_id")
	require.Contains(t, content, "LEFT JOIN users invitee ON invitee.id = ual.source_user_id")
	require.NotContains(t, content, "\nJOIN payment_orders po ON po.id = ual.source_order_id")
	require.NotContains(t, content, "\nJOIN users invitee ON invitee.id = ual.source_user_id")
	require.NotContains(t, content, "AND ual.source_order_id IS NOT NULL")
}

// TestAffiliateTransferRecordsQueryIncludesOfflineWithdrawals 锁定提取记录同时
// 列出转入余额与线下提现两类额度流出。
func TestAffiliateTransferRecordsQueryIncludesOfflineWithdrawals(t *testing.T) {
	source, err := os.ReadFile("affiliate_repo.go")
	require.NoError(t, err)
	content := string(source)

	require.Contains(t, content, "WHERE ual.action IN ('transfer', 'withdraw')")
}

// TestAffiliateWithdrawClaimsOperationBeforeDeducting 锁定线下提现的幂等形态：
// 同一事务内先按 operation_id 唯一约束写入占位流水，冲突时不扣减，
// 且唯一约束由迁移建立。
func TestAffiliateWithdrawClaimsOperationBeforeDeducting(t *testing.T) {
	source, err := os.ReadFile("affiliate_repo.go")
	require.NoError(t, err)
	content := string(source)

	require.Contains(t, content, "ON CONFLICT (operation_id) WHERE operation_id IS NOT NULL DO NOTHING")
	withdraw := content[strings.Index(content, "func (r *affiliateRepository) WithdrawQuota("):]
	withdraw = withdraw[:strings.Index(withdraw, "\n}\n")]
	claimAt := strings.Index(withdraw, "claimAffiliateWithdrawLedger(")
	deductAt := strings.Index(withdraw, "SET aff_quota = aff_quota - $1")
	require.Positive(t, claimAt)
	require.Greater(t, deductAt, claimAt, "operation claim must precede the quota deduction")

	migration, err := os.ReadFile("../../migrations/240_affiliate_ledger_operation_id.sql")
	require.NoError(t, err)
	require.Contains(t, string(migration), "CREATE UNIQUE INDEX IF NOT EXISTS idx_user_affiliate_ledger_operation_id")
	require.Contains(t, string(migration), "WHERE operation_id IS NOT NULL")
}
