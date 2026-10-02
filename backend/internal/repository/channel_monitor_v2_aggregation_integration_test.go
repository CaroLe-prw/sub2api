//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

// Execute the complete statement against PostgreSQL so malformed SQL in any
// branch fails before it can stall the production aggregation watermark.
func TestChannelMonitorV2ErrorAggregationSQLExecutes(t *testing.T) {
	tx := testTx(t)
	end := time.Date(2026, time.August, 25, 12, 0, 0, 0, time.UTC)
	_, err := tx.ExecContext(
		context.Background(),
		channelMonitorV2ErrorAggregationSQL,
		end.Add(-10*time.Minute),
		end,
	)
	require.NoError(t, err)
}

func TestChannelMonitorV2ErrorAggregationExcludesInboundUploads(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	end := time.Now().UTC().Truncate(time.Minute)
	start := end.Add(-5 * time.Minute)
	const model = "upload-health-regression"
	for _, row := range []struct {
		id, phase, owner, source, message string
		status                            int
		upstreamStatus                    any
	}{
		{"upload", "request", "client", "client_request", "Failed to read request body", 400, nil},
		{"upload", "request", "client", "client_request", "Failed to read request body", 400, nil},
		{"upstream-read", "upstream", "provider", "upstream_http", "Failed to read request body", 400, 400},
		{"upstream-eof", "upstream", "provider", "upstream_http", "unexpected EOF", 502, 502},
		{"parameters", "request", "client", "client_request", "missing required parameter: model", 400, nil},
		{"server", "internal", "platform", "gateway", "Failed to read request body", 500, nil},
		// Excluding the final inbound row after dedup must not resurrect an
		// earlier upstream error for the same logical request.
		{"dedup-upload", "upstream", "provider", "upstream_http", "upstream failure", 502, 502},
		{"dedup-upload", "request", "client", "client_request", "Failed to read request body", 400, nil},
		// A recorded upstream attempt with status 0 is not an inbound failure.
		{"attempt-zero", "request", "client", "client_request", "Failed to read request body", 400, 0},
	} {
		_, err := tx.ExecContext(ctx, `INSERT INTO ops_error_logs
			(request_id, platform, group_id, model, user_id, error_phase, error_type,
			 error_owner, error_source, error_message, status_code, upstream_status_code, created_at)
			VALUES ($1, 'openai', 0, $2, 4242, $3, 'invalid_request_error', $4, $5, $6, $7, $8, $9)`,
			row.id, model, row.phase, row.owner, row.source, row.message, row.status, row.upstreamStatus, start)
		require.NoError(t, err)
	}
	_, err := tx.ExecContext(ctx, channelMonitorV2ErrorAggregationSQL, start, end)
	require.NoError(t, err)
	var errors, upstreamAffected, attempts int
	err = tx.QueryRowContext(ctx, `SELECT error_requests, upstream_affected_requests, upstream_attempt_count
		FROM channel_monitor_v2_metrics_1m WHERE model = $1 AND bucket_start = $2`, model, start).Scan(&errors, &upstreamAffected, &attempts)
	require.NoError(t, err)
	require.Equal(t, 5, errors)
	require.Equal(t, 3, upstreamAffected)
	require.Zero(t, attempts)
	var userErrors, categoryErrors int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT SUM(error_requests) FROM channel_monitor_v2_user_metrics_1m WHERE model = $1`, model).Scan(&userErrors))
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT SUM(error_requests) FROM channel_monitor_v2_error_metrics_1m WHERE model = $1`, model).Scan(&categoryErrors))
	require.Equal(t, errors, userErrors)
	require.Equal(t, errors, categoryErrors)
	// Historical catch-up must repair even a day/half-day bucket whose bounds
	// were not crossed by this chunk. Otherwise 7d/30d health stays stale.
	repo := &channelMonitorV2Repository{}
	require.NoError(t, repo.recomputeFixedRollups(ctx, tx, end.Add(-2*time.Hour), end))
	for _, seconds := range channelMonitorV2FixedRollupSeconds {
		var rollupErrors int
		require.NoError(t, tx.QueryRowContext(ctx, `SELECT SUM(error_requests)
			FROM channel_monitor_v2_metrics_rollup WHERE model = $1 AND bucket_seconds = $2`, model, seconds).Scan(&rollupErrors))
		require.Equal(t, errors, rollupErrors)
	}
}

func TestInboundBodyReadHealthMigrationRepairsHistoryIdempotently(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	// Avoid fixtures from other tests while covering UTC hourly/daily boundaries.
	when := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	_, err := tx.ExecContext(ctx, `INSERT INTO ops_error_logs
		(platform, group_id, model, error_phase, error_type, error_owner, error_source, error_message, status_code, created_at)
		VALUES ('openai', 424242, 'upload-migration', 'request', 'invalid_request_error', 'client', 'client_request', 'Failed to read request body', 400, $1)`, when)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO ops_error_logs
		(platform, group_id, model, error_phase, error_type, error_owner, error_source, error_message, status_code, upstream_status_code, created_at)
		VALUES ('openai', 424242, 'upload-migration', 'upstream', 'invalid_request_error', 'provider', 'upstream_http', 'Failed to read request body', 400, 400, $1)`, when)
	require.NoError(t, err)
	// Test only the group dimension to avoid colliding with unrelated fixtures.
	_, err = tx.ExecContext(ctx, `INSERT INTO ops_metrics_hourly
		(bucket_start, platform, group_id, success_count, error_count_total, error_count_sla)
		VALUES ($1, 'openai', 424242, 10, 2, 2)`, when)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO ops_metrics_daily
		(bucket_date, platform, group_id, success_count, error_count_total, error_count_sla)
		VALUES (($1::timestamptz AT TIME ZONE 'UTC')::date, 'openai', 424242, 10, 2, 2)`, when)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `INSERT INTO channel_monitor_v2_watermarks (id, data_through)
		VALUES (1, $1) ON CONFLICT (id) DO UPDATE SET data_through = EXCLUDED.data_through`, when.Add(time.Hour))
	require.NoError(t, err)

	body, err := migrations.FS.ReadFile("242_exclude_inbound_body_read_failures.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = tx.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	for _, table := range []string{"ops_metrics_hourly", "ops_metrics_daily"} {
		var success, total, sla, excluded int
		err = tx.QueryRowContext(ctx, `SELECT success_count, error_count_total, error_count_sla, business_limited_count FROM `+table+` WHERE group_id = 424242`).Scan(&success, &total, &sla, &excluded)
		require.NoError(t, err)
		require.Equal(t, 10, success)
		require.Equal(t, 2, total)
		require.Equal(t, 1, sla)
		require.Equal(t, 1, excluded)
	}
	var inboundExcluded, upstreamExcluded bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT is_business_limited FROM ops_error_logs WHERE model = 'upload-migration' AND error_phase = 'request'`).Scan(&inboundExcluded))
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT is_business_limited FROM ops_error_logs WHERE model = 'upload-migration' AND error_phase = 'upstream'`).Scan(&upstreamExcluded))
	require.True(t, inboundExcluded)
	require.False(t, upstreamExcluded)
	var through time.Time
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT data_through FROM channel_monitor_v2_watermarks WHERE id = 1`).Scan(&through))
	require.True(t, through.Equal(when.Add(time.Hour)))
	var queued int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM channel_monitor_v2_health_repairs
		WHERE bucket_date = ($1::timestamptz AT TIME ZONE 'UTC')::date`, when).Scan(&queued))
	require.Equal(t, 1, queued)
}

func TestChannelMonitorV2HistoricalHealthRepairPreservesLiveCoverage(t *testing.T) {
	ctx := context.Background()
	db := integrationDB
	repo := &channelMonitorV2Repository{db: db}
	now := time.Now().UTC().Truncate(time.Minute)
	day := now.Truncate(24 * time.Hour).Add(-24 * time.Hour)
	const model = "upload-health-repair-worker"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM ops_error_logs WHERE model = $1`, model)
		_, _ = db.ExecContext(ctx, `DELETE FROM channel_monitor_v2_health_repairs WHERE bucket_date = $1`, day)
		for _, table := range []string{
			"channel_monitor_v2_metrics_1m", "channel_monitor_v2_user_metrics_1m", "channel_monitor_v2_error_metrics_1m",
			"channel_monitor_v2_metrics_rollup", "channel_monitor_v2_user_metrics_rollup", "channel_monitor_v2_error_metrics_rollup",
		} {
			_, _ = db.ExecContext(ctx, `DELETE FROM `+table+` WHERE model = $1`, model)
		}
	})
	_, err := db.ExecContext(ctx, `INSERT INTO ops_error_logs
		(platform, group_id, model, error_phase, error_type, error_owner, error_source, error_message, status_code, created_at)
		VALUES ('openai', 424243, $1, 'request', 'invalid_request_error', 'client', 'client_request', 'Failed to read request body', 400, $2),
		       ('openai', 424243, $1, 'upstream', 'upstream_error', 'provider', 'upstream_http', 'unexpected EOF', 502, $2)`, model, day)
	require.NoError(t, err)
	// Give the worker an isolated repair queue; this harness uses a disposable DB.
	_, err = db.ExecContext(ctx, `DELETE FROM channel_monitor_v2_health_repairs`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO channel_monitor_v2_health_repairs (bucket_date) VALUES ($1)`, day)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO channel_monitor_v2_watermarks (id, data_through)
		VALUES (1, $1) ON CONFLICT (id) DO UPDATE SET data_through = EXCLUDED.data_through`, now)
	require.NoError(t, err)

	repaired, err := repo.RepairNextInboundBodyReadHealth(ctx)
	require.NoError(t, err)
	require.True(t, repaired)
	for _, seconds := range channelMonitorV2FixedRollupSeconds {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT SUM(error_requests) FROM channel_monitor_v2_metrics_rollup
			WHERE model = $1 AND bucket_seconds = $2`, model, seconds).Scan(&count))
		require.Equal(t, 1, count)
	}
	var through time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT data_through FROM channel_monitor_v2_watermarks WHERE id = 1`).Scan(&through))
	require.True(t, through.Equal(now))
	repaired, err = repo.RepairNextInboundBodyReadHealth(ctx)
	require.NoError(t, err)
	require.False(t, repaired)
}
