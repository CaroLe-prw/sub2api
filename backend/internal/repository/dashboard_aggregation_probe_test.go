//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestDashboardAggregationRepositoryHourlyActiveUsersExcludeSystemProbeRows(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectExec(`(?s)INSERT INTO usage_dashboard_hourly_users.*FROM usage_logs.*WHERE created_at >= \$1 AND created_at < \$2.*AND user_id IS NOT NULL`).
		WithArgs(start, end, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.insertHourlyActiveUsers(context.Background(), start, end))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDashboardAggregationRepositoryHourlyAggregateSeparatesProbeUsage(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := newDashboardAggregationRepositoryWithSQL(db)
	start := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	mock.ExpectExec(`(?s)INSERT INTO usage_dashboard_hourly \(.*probe_requests.*probe_input_tokens.*probe_output_tokens.*probe_cache_creation_tokens.*probe_cache_read_tokens.*probe_total_cost.*probe_account_cost.*probe_duration_ms.*\).*ON CONFLICT.*probe_requests = EXCLUDED\.probe_requests.*probe_account_cost = EXCLUDED\.probe_account_cost.*probe_duration_ms = EXCLUDED\.probe_duration_ms`).
		WithArgs(start, end, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.upsertHourlyAggregates(context.Background(), start, end))
	require.NoError(t, mock.ExpectationsWereMet())
}
