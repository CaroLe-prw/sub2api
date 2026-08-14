package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// GetOpenAISchedulerHealthSnapshots returns a small account/model aggregate for
// scheduler startup and periodic hydration. Error rows are deliberately scoped
// to provider-owned upstream failures; request validation, auth, policy and
// client-cancelled rows are not channel-health evidence.
func (r *usageLogRepository) GetOpenAISchedulerHealthSnapshots(
	ctx context.Context,
	since time.Time,
) ([]service.OpenAISchedulerHealthSnapshot, error) {
	rows, err := r.sql.QueryContext(ctx, `
		WITH successes AS (
			SELECT account_id,
			       lower(COALESCE(NULLIF(TRIM(upstream_model), ''), NULLIF(TRIM(requested_model), ''), NULLIF(TRIM(model), ''), '')) AS model,
			       COUNT(*)::bigint AS success_count,
			       AVG(first_token_ms) FILTER (WHERE first_token_ms > 0)::float8 AS avg_ttft_ms
			FROM usage_logs
			WHERE created_at >= $1 AND account_id > 0 AND actual_cost > 0
			GROUP BY 1, 2
		), failures AS (
			SELECT account_id,
			       lower(COALESCE(NULLIF(TRIM(upstream_model), ''), NULLIF(TRIM(requested_model), ''), NULLIF(TRIM(model), ''), '')) AS model,
			       COUNT(DISTINCT COALESCE(NULLIF(request_id, ''), 'error:' || id::text))::bigint AS failure_count
			FROM ops_error_logs
			WHERE created_at >= $1
			  AND account_id IS NOT NULL AND account_id > 0
			  AND error_phase = 'upstream'
			  AND error_owner = 'provider'
			  AND COALESCE(upstream_status_code, status_code, 0) NOT IN (400, 401, 403, 404, 409, 422, 499)
			  AND lower(COALESCE(error_type, '')) NOT IN ('cyber_policy', 'client_cancelled', 'invalid_request')
			GROUP BY 1, 2
		)
		SELECT COALESCE(s.account_id, f.account_id), COALESCE(s.model, f.model),
		       COALESCE(s.success_count, 0), COALESCE(f.failure_count, 0), s.avg_ttft_ms
		FROM successes s
		FULL OUTER JOIN failures f ON f.account_id = s.account_id AND f.model = s.model
		ORDER BY 1, 2
	`, since.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make([]service.OpenAISchedulerHealthSnapshot, 0)
	for rows.Next() {
		var item service.OpenAISchedulerHealthSnapshot
		if err := rows.Scan(&item.AccountID, &item.Model, &item.SuccessCount, &item.FailureCount, &item.AvgTTFTMs); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
