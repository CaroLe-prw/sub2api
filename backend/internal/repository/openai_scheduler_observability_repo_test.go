package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestOpenAISchedulerObservabilityRepositoryReturnsUnitIntervalRates(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	cutoff := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT COALESCE\(SUM\(request_count\),0\)`).
		WithArgs(cutoff).
		WillReturnRows(sqlmock.NewRows([]string{
			"requests", "sticky_detected", "sticky", "switched", "switches", "cache_read", "cache_eligible",
		}).AddRow(13, 10, 9, 4, 7, 261504, 309864))
	mock.ExpectQuery(`SELECT group_id,MAX\(group_name\)`).
		WithArgs(cutoff).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "group_name"}))
	mock.ExpectQuery(`SELECT payload FROM openai_scheduler_observability_abnormal_traces`).
		WithArgs(cutoff).
		WillReturnRows(sqlmock.NewRows([]string{"payload"}))

	repo := NewOpenAISchedulerObservabilityRepository(db)
	data, err := repo.LoadOpenAISchedulerObservability(context.Background(), cutoff, nil)
	require.NoError(t, err)
	require.InDelta(t, 9.0/10.0, data.Metrics.StickyHitRate, 0.000001)
	require.InDelta(t, 4.0/13.0, data.Metrics.SwitchRate, 0.000001)
	require.InDelta(t, 261504.0/309864.0, data.Metrics.FollowUpCacheRate, 0.000001)
	require.NoError(t, mock.ExpectationsWereMet())
}
