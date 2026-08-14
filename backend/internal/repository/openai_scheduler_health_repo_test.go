package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepository_GetOpenAISchedulerHealthSnapshots(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := newUsageLogRepositoryWithSQL(nil, db)
	rows := sqlmock.NewRows([]string{"account_id", "model", "success_count", "failure_count", "avg_ttft_ms"}).
		AddRow(int64(35), "gpt-5.6-sol", int64(18), int64(2), float64(920))
	mock.ExpectQuery(regexp.QuoteMeta("WITH successes AS (")).WithArgs(sqlmock.AnyArg()).WillReturnRows(rows)

	snapshots, err := repo.GetOpenAISchedulerHealthSnapshots(context.Background(), time.Now().Add(-30*time.Minute))
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, int64(35), snapshots[0].AccountID)
	require.Equal(t, "gpt-5.6-sol", snapshots[0].Model)
	require.Equal(t, int64(18), snapshots[0].SuccessCount)
	require.Equal(t, int64(2), snapshots[0].FailureCount)
	require.NotNil(t, snapshots[0].AvgTTFTMs)
	require.InDelta(t, 920, *snapshots[0].AvgTTFTMs, 0.001)
	require.NoError(t, mock.ExpectationsWereMet())
}
