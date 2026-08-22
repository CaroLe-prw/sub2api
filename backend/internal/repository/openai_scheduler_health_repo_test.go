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
	lastSuccess := time.Now().Add(-time.Minute)
	lastFailure := time.Now().Add(-2 * time.Minute)
	rows := sqlmock.NewRows([]string{
		"account_id", "model", "success_count", "failure_count", "avg_ttft_ms", "last_success_at", "last_failure_at",
	}).AddRow(int64(35), "gpt-5.6-sol", int64(18), int64(2), float64(920), lastSuccess, lastFailure)
	mock.ExpectQuery(regexp.QuoteMeta("WITH successes AS (")).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnRows(rows)

	snapshots, err := repo.GetOpenAISchedulerHealthSnapshots(context.Background(), time.Now().Add(-30*time.Minute))
	require.NoError(t, err)
	require.Len(t, snapshots, 1)
	require.Equal(t, int64(35), snapshots[0].AccountID)
	require.Equal(t, "gpt-5.6-sol", snapshots[0].Model)
	require.Equal(t, int64(18), snapshots[0].SuccessCount)
	require.Equal(t, int64(2), snapshots[0].FailureCount)
	require.NotNil(t, snapshots[0].AvgTTFTMs)
	require.InDelta(t, 920, *snapshots[0].AvgTTFTMs, 0.001)
	require.Equal(t, lastSuccess, *snapshots[0].LastSuccessAt)
	require.Equal(t, lastFailure, *snapshots[0].LastFailureAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageLogRepository_GetSchedulerUserTrafficEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	repo := newUsageLogRepositoryWithSQL(nil, db)
	successAt := time.Now().Add(-2 * time.Minute)
	failureAt := time.Now().Add(-time.Minute)
	rows := sqlmock.NewRows([]string{
		"event_id", "account_id", "model", "status", "ttft_ms", "latency_ms", "created_at",
	}).
		AddRow("usage:9", int64(35), "gpt-5.6-sol", "success", int64(420), int64(1250), successAt).
		AddRow("error:12", int64(35), "gpt-5.6-sol", "failed", nil, int64(800), failureAt)
	mock.ExpectQuery(regexp.QuoteMeta("WITH success_events AS (")).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), 60).
		WillReturnRows(rows)

	events, err := repo.GetSchedulerUserTrafficEvents(context.Background(), time.Now().Add(-30*time.Minute), []int64{35}, 60)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "usage:9", events[0].ID)
	require.Equal(t, "success", events[0].Status)
	require.Equal(t, int64(420), *events[0].TTFTMs)
	require.Equal(t, int64(1250), *events[0].LatencyMs)
	require.Equal(t, successAt, events[0].CreatedAt)
	require.Equal(t, "error:12", events[1].ID)
	require.Equal(t, "failed", events[1].Status)
	require.Nil(t, events[1].TTFTMs)
	require.Equal(t, failureAt, events[1].CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}
