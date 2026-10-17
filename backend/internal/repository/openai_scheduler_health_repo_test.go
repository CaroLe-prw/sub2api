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
	lastTTFT := time.Now().Add(-3 * time.Minute)
	rows := sqlmock.NewRows([]string{
		"account_id", "model", "success_count", "failure_count", "avg_ttft_ms", "last_success_at", "last_failure_at",
		"cache_read_tokens", "cache_eligible_tokens", "cache_sample_count", "last_cache_sample_at",
		"ttft_sample_count", "last_ttft_sample_at", "weighted_ttft_ms", "ttft_effective_samples",
	}).
		AddRow(int64(35), "gpt-5.6-sol", int64(18), int64(2), float64(920), lastSuccess, lastFailure, int64(9000), int64(10000), int64(18), lastSuccess, int64(12), lastTTFT, float64(840), float64(4.25)).
		AddRow(int64(36), "gpt-5.6-sol", int64(0), int64(1), nil, nil, lastFailure, int64(0), int64(0), int64(0), nil, int64(0), nil, nil, float64(0))
	mock.ExpectQuery(`(?s)WITH successes AS.*SUM\(GREATEST\(input_tokens, 0\)::bigint \+ GREATEST\(cache_creation_tokens, 0\)::bigint \+ GREATEST\(cache_read_tokens, 0\)::bigint\).*AS cache_sample_count.*user_id > 0 AND request_type NOT IN \(4, 6\).*COALESCE\(s.cache_read_tokens, 0\)`).WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), float64(300)).WillReturnRows(rows)

	before := time.Now()
	snapshots, err := repo.GetOpenAISchedulerHealthSnapshots(context.Background(), time.Now().Add(-30*time.Minute))
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	require.Equal(t, int64(35), snapshots[0].AccountID)
	require.Equal(t, "gpt-5.6-sol", snapshots[0].Model)
	require.Equal(t, int64(18), snapshots[0].SuccessCount)
	require.Equal(t, int64(2), snapshots[0].FailureCount)
	require.NotNil(t, snapshots[0].AvgTTFTMs)
	require.InDelta(t, 920, *snapshots[0].AvgTTFTMs, 0.001)
	require.Equal(t, lastSuccess, *snapshots[0].LastSuccessAt)
	require.Equal(t, lastFailure, *snapshots[0].LastFailureAt)
	require.EqualValues(t, 9000, snapshots[0].CacheReadTokens)
	require.EqualValues(t, 10000, snapshots[0].CacheEligibleTokens)
	require.EqualValues(t, 18, snapshots[0].CacheSampleCount)
	require.Equal(t, lastSuccess, *snapshots[0].LastCacheSampleAt)
	require.InDelta(t, 0.9, *snapshots[0].CacheHitRate(), 0.0001)
	require.EqualValues(t, 12, snapshots[0].TTFTSampleCount)
	require.Equal(t, lastTTFT, *snapshots[0].LastTTFTSampleAt)
	require.InDelta(t, 840, *snapshots[0].WeightedTTFTMs, 0.0001)
	require.InDelta(t, 4.25, snapshots[0].TTFTEffectiveSamples, 0.0001)
	require.False(t, snapshots[0].TTFTObservedAt.Before(before))
	require.False(t, snapshots[0].TTFTObservedAt.After(time.Now()))
	require.Equal(t, time.UTC, snapshots[0].TTFTObservedAt.Location())
	require.Zero(t, snapshots[1].TTFTSampleCount)
	require.Nil(t, snapshots[1].LastTTFTSampleAt)
	require.Nil(t, snapshots[1].WeightedTTFTMs)
	require.Zero(t, snapshots[1].TTFTEffectiveSamples)
	require.Equal(t, snapshots[0].TTFTObservedAt, snapshots[1].TTFTObservedAt)
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

func TestUsageLogRepository_GetSchedulerFirstOutputEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := newUsageLogRepositoryWithSQL(nil, db)
	now := time.Now()
	rows := sqlmock.NewRows([]string{"account_id", "model", "first_token_ms", "created_at"}).
		AddRow(int64(478), "gpt-5.6-sol", int64(57130), now)
	// Check both the evidence boundary and the per-model latest-three limit.
	mock.ExpectQuery(`(?s)WITH measured AS.*user_id > 0.*request_type NOT IN \(4, 6\) AND first_token_ms > 0.*PARTITION BY account_id, model ORDER BY created_at DESC, id DESC.*rn <= 3`).
		WithArgs(sqlmock.AnyArg()).WillReturnRows(rows)
	events, err := repo.GetSchedulerFirstOutputEvents(context.Background(), now.Add(-30*time.Minute))
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(478), events[0].AccountID)
	require.Equal(t, "gpt-5.6-sol", events[0].Model)
	require.Equal(t, "success", events[0].Status)
	require.Equal(t, int64(57130), *events[0].TTFTMs)
	require.Equal(t, now, events[0].CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}
