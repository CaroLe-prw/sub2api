package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// GetOpenAISchedulerHealthSnapshots returns a small account/model aggregate for
// scheduler startup and periodic hydration. Error rows are deliberately scoped
// to provider-owned upstream failures; request validation, auth, policy and
// client-cancelled rows are not channel-health evidence.
func (r *usageLogRepository) GetOpenAISchedulerHealthSnapshots(
	ctx context.Context,
	since time.Time,
) ([]service.OpenAISchedulerHealthSnapshot, error) {
	return r.GetSchedulerUserTrafficSnapshots(ctx, since, nil)
}

// GetSchedulerUserTrafficSnapshots returns account/model health evidence from
// real user requests, optionally restricted to the accounts visible on the
// current admin page.
func (r *usageLogRepository) GetSchedulerUserTrafficSnapshots(
	ctx context.Context,
	since time.Time,
	accountIDs []int64,
) ([]service.OpenAISchedulerHealthSnapshot, error) {
	rows, err := r.sql.QueryContext(ctx, `
		WITH successes AS (
			SELECT account_id,
			       lower(COALESCE(NULLIF(TRIM(upstream_model), ''), NULLIF(TRIM(requested_model), ''), NULLIF(TRIM(model), ''), '')) AS model,
			       COUNT(*)::bigint AS success_count,
			       AVG(first_token_ms) FILTER (WHERE first_token_ms > 0)::float8 AS avg_ttft_ms,
			       MAX(created_at) AS last_success_at
			FROM usage_logs
			WHERE created_at >= $1 AND account_id > 0
			  AND user_id > 0 AND request_type NOT IN (4, 6)
			  AND (COALESCE(cardinality($2::bigint[]), 0) = 0 OR account_id = ANY($2::bigint[]))
			GROUP BY 1, 2
		), failures AS (
			SELECT account_id,
			       lower(COALESCE(NULLIF(TRIM(upstream_model), ''), NULLIF(TRIM(requested_model), ''), NULLIF(TRIM(model), ''), '')) AS model,
			       COUNT(DISTINCT COALESCE(NULLIF(request_id, ''), 'error:' || id::text))::bigint AS failure_count,
			       MAX(created_at) AS last_failure_at
			FROM ops_error_logs
			WHERE created_at >= $1
			  AND account_id IS NOT NULL AND account_id > 0
			  AND user_id IS NOT NULL AND user_id > 0
			  AND COALESCE(request_type, 0) NOT IN (4, 6)
			  AND (COALESCE(cardinality($2::bigint[]), 0) = 0 OR account_id = ANY($2::bigint[]))
			  AND error_phase = 'upstream'
			  AND error_owner = 'provider'
			  AND COALESCE(upstream_status_code, status_code, 0) NOT IN (400, 401, 403, 404, 409, 422, 499)
			  AND lower(COALESCE(error_type, '')) NOT IN ('cyber_policy', 'client_cancelled', 'invalid_request')
			GROUP BY 1, 2
		)
		SELECT COALESCE(s.account_id, f.account_id), COALESCE(s.model, f.model),
		       COALESCE(s.success_count, 0), COALESCE(f.failure_count, 0), s.avg_ttft_ms,
		       s.last_success_at, f.last_failure_at
		FROM successes s
		FULL OUTER JOIN failures f ON f.account_id = s.account_id AND f.model = s.model
		ORDER BY 1, 2
	`, since.UTC(), pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.OpenAISchedulerHealthSnapshot, 0)
	for rows.Next() {
		var item service.OpenAISchedulerHealthSnapshot
		if err := rows.Scan(
			&item.AccountID, &item.Model, &item.SuccessCount, &item.FailureCount, &item.AvgTTFTMs,
			&item.LastSuccessAt, &item.LastFailureAt,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// GetSchedulerUserTrafficEvents returns the most recent real user requests per
// account/model. Successes and provider-owned upstream failures use the same
// health-evidence filters as GetSchedulerUserTrafficSnapshots.
func (r *usageLogRepository) GetSchedulerUserTrafficEvents(
	ctx context.Context,
	since time.Time,
	accountIDs []int64,
	limitPerModel int,
) ([]service.ChannelMonitorUserTrafficEvent, error) {
	if limitPerModel <= 0 {
		limitPerModel = 60
	}
	rows, err := r.sql.QueryContext(ctx, `
		WITH success_events AS (
			SELECT 'usage:' || u.id::text AS event_id,
			       u.account_id,
			       lower(COALESCE(NULLIF(TRIM(u.upstream_model), ''), NULLIF(TRIM(u.requested_model), ''), NULLIF(TRIM(u.model), ''), '')) AS model,
			       'success'::text AS status,
			       u.first_token_ms::bigint AS ttft_ms,
			       u.duration_ms::bigint AS latency_ms,
			       u.created_at
			FROM usage_logs u
			WHERE u.created_at >= $1 AND u.account_id > 0
			  AND u.user_id > 0 AND u.request_type NOT IN (4, 6)
			  AND (COALESCE(cardinality($2::bigint[]), 0) = 0 OR u.account_id = ANY($2::bigint[]))
		), failure_events AS (
			SELECT DISTINCT ON (
				e.account_id,
				lower(COALESCE(NULLIF(TRIM(e.upstream_model), ''), NULLIF(TRIM(e.requested_model), ''), NULLIF(TRIM(e.model), ''), '')),
				COALESCE(NULLIF(e.request_id, ''), 'error:' || e.id::text)
			)
			       'error:' || e.id::text AS event_id,
			       e.account_id,
			       lower(COALESCE(NULLIF(TRIM(e.upstream_model), ''), NULLIF(TRIM(e.requested_model), ''), NULLIF(TRIM(e.model), ''), '')) AS model,
			       'failed'::text AS status,
			       e.time_to_first_token_ms AS ttft_ms,
			       e.duration_ms::bigint AS latency_ms,
			       e.created_at
			FROM ops_error_logs e
			WHERE e.created_at >= $1
			  AND e.account_id IS NOT NULL AND e.account_id > 0
			  AND e.user_id IS NOT NULL AND e.user_id > 0
			  AND COALESCE(e.request_type, 0) NOT IN (4, 6)
			  AND (COALESCE(cardinality($2::bigint[]), 0) = 0 OR e.account_id = ANY($2::bigint[]))
			  AND e.error_phase = 'upstream'
			  AND e.error_owner = 'provider'
			  AND COALESCE(e.upstream_status_code, e.status_code, 0) NOT IN (400, 401, 403, 404, 409, 422, 499)
			  AND lower(COALESCE(e.error_type, '')) NOT IN ('cyber_policy', 'client_cancelled', 'invalid_request')
			ORDER BY e.account_id,
			         lower(COALESCE(NULLIF(TRIM(e.upstream_model), ''), NULLIF(TRIM(e.requested_model), ''), NULLIF(TRIM(e.model), ''), '')),
			         COALESCE(NULLIF(e.request_id, ''), 'error:' || e.id::text),
			         e.created_at DESC,
			         e.id DESC
		), ranked AS (
			SELECT combined.*,
			       ROW_NUMBER() OVER (
				   PARTITION BY combined.account_id, combined.model
				   ORDER BY combined.created_at DESC, combined.event_id DESC
			       ) AS row_number
			FROM (
				SELECT * FROM success_events
				UNION ALL
				SELECT * FROM failure_events
			) combined
		)
		SELECT event_id, account_id, model, status, ttft_ms, latency_ms, created_at
		FROM ranked
		WHERE row_number <= $3
		ORDER BY account_id, model, created_at ASC, event_id ASC
	`, since.UTC(), pq.Array(accountIDs), limitPerModel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.ChannelMonitorUserTrafficEvent, 0)
	for rows.Next() {
		var item service.ChannelMonitorUserTrafficEvent
		if err := rows.Scan(
			&item.ID,
			&item.AccountID,
			&item.Model,
			&item.Status,
			&item.TTFTMs,
			&item.LatencyMs,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
