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
			"non_order_recharge", int64(0), "", int64(11), "inviter@example.com", "inviter",
			int64(22), "invitee@example.com", "invitee", 0.0, 0.0, 2.5, "", "", createdAt,
		))

	records, total, err := repo.ListAffiliateRebateRecords(context.Background(), service.AffiliateRecordFilter{
		Page: 1, PageSize: 20,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	require.Equal(t, "non_order_recharge", records[0].SourceType)
	require.Zero(t, records[0].OrderID)
	require.Equal(t, 2.5, records[0].RebateAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}
