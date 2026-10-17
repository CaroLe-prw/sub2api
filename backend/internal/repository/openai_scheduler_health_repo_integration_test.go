//go:build integration

package repository

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSchedulerUserTrafficSnapshots_DecaysOnlyMeasuredRealRequests(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	// Temporary tables isolate the aggregation contract from unrelated fixtures
	// while executing the production query against PostgreSQL.
	_, err := tx.ExecContext(ctx, `
		CREATE TEMP TABLE usage_logs (
			account_id bigint, user_id bigint, model text, requested_model text, upstream_model text,
			first_token_ms integer, created_at timestamptz, request_type integer,
			input_tokens integer DEFAULT 100, cache_creation_tokens integer DEFAULT 0,
			cache_read_tokens integer DEFAULT 50
		) ON COMMIT DROP;
		CREATE TEMP TABLE ops_error_logs (
			id bigint, account_id bigint, user_id bigint, model text, requested_model text, upstream_model text,
			request_id text, created_at timestamptz, request_type integer, error_phase text, error_owner text,
			upstream_status_code integer, status_code integer, error_type text
		) ON COMMIT DROP;
	`)
	require.NoError(t, err)
	base := time.Now().UTC().Truncate(time.Microsecond)
	for _, row := range []struct {
		accountID   int64
		userID      int64
		requestType int
		ttft        any
		age         time.Duration
	}{
		{1, 1, 1, 100, 10 * time.Minute},
		{1, 1, 1, 1000, 5 * time.Minute},
		{1, 1, 1, nil, 0},
		{1, 1, 1, 0, 0},
		{1, 1, 1, -5, 0},
		{1, 1, 1, 100000, -time.Hour}, // Future TTFT cannot gain confidence.
		{1, 1, 1, 100000, 31 * time.Minute},
		{1, 1, 4, 100000, 0}, // Probe/test request types stay excluded.
		{1, 1, 6, 100000, 0},
		{1, 0, 1, 100000, 0},
		{2, 1, 1, 100000, 0}, // Account filter stays in force.
	} {
		_, err = tx.ExecContext(ctx, `INSERT INTO usage_logs
			(account_id, user_id, request_type, first_token_ms, created_at, model, requested_model, upstream_model)
			VALUES ($1, $2, $3, $4, $5, 'billing-model', 'requested-model', ' GPT-5 ')`,
			row.accountID, row.userID, row.requestType, row.ttft, base.Add(-row.age))
		require.NoError(t, err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ops_error_logs
		(id, account_id, user_id, model, upstream_model, request_id, created_at, request_type,
		 error_phase, error_owner, upstream_status_code, status_code, error_type)
		VALUES
		(1, 1, 1, 'billing-model', ' GPT-5 ', 'failure-1', $1, 1, 'upstream', 'provider', 502, 502, 'upstream_error'),
		(2, 1, 1, 'billing-model', ' GPT-5 ', 'failure-1', $1, 1, 'upstream', 'provider', 502, 502, 'upstream_error'),
		(3, 1, 1, 'billing-model', ' GPT-5 ', 'client-error', $1, 1, 'request', 'client', NULL, 400, 'invalid_request'),
		(4, 3, 1, 'billing-model', ' GPT-5 ', 'failure-3', $1, 1, 'upstream', 'provider', 502, 502, 'upstream_error')`, base)
	require.NoError(t, err)

	repo := newUsageLogRepositoryWithSQL(nil, tx)
	snapshots, err := repo.GetSchedulerUserTrafficSnapshots(ctx, base.Add(-30*time.Minute), []int64{1, 3})
	require.NoError(t, err)
	require.Len(t, snapshots, 2)
	measured := snapshots[0]
	require.EqualValues(t, 1, measured.AccountID)
	require.Equal(t, "gpt-5", measured.Model)
	require.EqualValues(t, 6, measured.SuccessCount)
	require.EqualValues(t, 1, measured.FailureCount)
	// The legacy admin average and cache aggregates retain their existing
	// semantics; only the scheduler's new evidence ignores future timestamps.
	require.NotNil(t, measured.AvgTTFTMs)
	require.InDelta(t, 33700, *measured.AvgTTFTMs, 1e-8)
	require.EqualValues(t, 300, measured.CacheReadTokens)
	require.EqualValues(t, 900, measured.CacheEligibleTokens)
	require.EqualValues(t, 6, measured.CacheSampleCount)
	require.EqualValues(t, 2, measured.TTFTSampleCount)
	require.NotNil(t, measured.LastTTFTSampleAt)
	require.True(t, measured.LastTTFTSampleAt.Equal(base.Add(-5*time.Minute)))
	require.NotNil(t, measured.WeightedTTFTMs)
	require.InDelta(t, 700, *measured.WeightedTTFTMs, 1e-8)
	decaySinceBase := math.Exp2(-measured.TTFTObservedAt.Sub(base).Seconds() / 300)
	require.InDelta(t, 0.75*decaySinceBase, measured.TTFTEffectiveSamples, 1e-8)
	require.EqualValues(t, 3, snapshots[1].AccountID)
	require.EqualValues(t, 1, snapshots[1].FailureCount)
	require.Zero(t, snapshots[1].TTFTSampleCount)
	require.Nil(t, snapshots[1].LastTTFTSampleAt)
	require.Nil(t, snapshots[1].WeightedTTFTMs)
	require.Zero(t, snapshots[1].TTFTEffectiveSamples)

	refreshed, err := repo.GetSchedulerUserTrafficSnapshots(ctx, base.Add(-30*time.Minute), []int64{1})
	require.NoError(t, err)
	require.Len(t, refreshed, 1)
	require.Equal(t, measured.LastTTFTSampleAt, refreshed[0].LastTTFTSampleAt)
	require.InDelta(t, *measured.WeightedTTFTMs, *refreshed[0].WeightedTTFTMs, 1e-8)
	require.LessOrEqual(t, refreshed[0].TTFTEffectiveSamples, measured.TTFTEffectiveSamples)
}
