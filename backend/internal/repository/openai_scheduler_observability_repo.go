package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAISchedulerObservabilityRepository struct{ db *sql.DB }

func NewOpenAISchedulerObservabilityRepository(db *sql.DB) service.OpenAISchedulerObservabilityRepository {
	return &openAISchedulerObservabilityRepository{db: db}
}

func (r *openAISchedulerObservabilityRepository) ApplyOpenAISchedulerObservabilityBatch(ctx context.Context, batch service.OpenAISchedulerObservabilityPersistentBatch) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("nil scheduler observability repository")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, item := range batch.Metrics {
		_, err = tx.ExecContext(ctx, `INSERT INTO openai_scheduler_observability_minute_metrics
  (bucket_start,group_id,group_name,request_count,sticky_detected_request_count,sticky_request_count,switched_request_count,switch_count,failed_request_count,cache_read_tokens,cache_eligible_tokens)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (bucket_start,group_id) DO UPDATE SET
  group_name=EXCLUDED.group_name,
  request_count=GREATEST(0,openai_scheduler_observability_minute_metrics.request_count+EXCLUDED.request_count),
  sticky_detected_request_count=GREATEST(0,openai_scheduler_observability_minute_metrics.sticky_detected_request_count+EXCLUDED.sticky_detected_request_count),
  sticky_request_count=GREATEST(0,openai_scheduler_observability_minute_metrics.sticky_request_count+EXCLUDED.sticky_request_count),
  switched_request_count=GREATEST(0,openai_scheduler_observability_minute_metrics.switched_request_count+EXCLUDED.switched_request_count),
  switch_count=GREATEST(0,openai_scheduler_observability_minute_metrics.switch_count+EXCLUDED.switch_count),
  failed_request_count=GREATEST(0,openai_scheduler_observability_minute_metrics.failed_request_count+EXCLUDED.failed_request_count),
  cache_read_tokens=GREATEST(0,openai_scheduler_observability_minute_metrics.cache_read_tokens+EXCLUDED.cache_read_tokens),
  cache_eligible_tokens=GREATEST(0,openai_scheduler_observability_minute_metrics.cache_eligible_tokens+EXCLUDED.cache_eligible_tokens),
  updated_at=NOW()`, item.BucketStart.UTC(), item.GroupID, item.GroupName, item.Requests, item.StickyDetectedRequests, item.StickyRequests,
			item.SwitchedRequests, item.Switches, item.FailedRequests, item.CacheReadTokens, item.CacheEligibleTokens)
		if err != nil {
			return err
		}
	}
	for _, mutation := range batch.Traces {
		if !mutation.Abnormal {
			if _, err = tx.ExecContext(ctx, `DELETE FROM openai_scheduler_observability_abnormal_traces WHERE request_id=$1`, mutation.Trace.RequestID); err != nil {
				return err
			}
			continue
		}
		payload, marshalErr := json.Marshal(mutation.Trace)
		if marshalErr != nil {
			return marshalErr
		}
		occurredAt := parseRepositorySchedulerTraceTime(mutation.Trace.CreatedAt)
		_, err = tx.ExecContext(ctx, `INSERT INTO openai_scheduler_observability_abnormal_traces
  (request_id,occurred_at,group_id,payload) VALUES ($1,$2,$3,$4::jsonb)
ON CONFLICT (request_id) DO UPDATE SET occurred_at=EXCLUDED.occurred_at,group_id=EXCLUDED.group_id,payload=EXCLUDED.payload,updated_at=NOW()`,
			mutation.Trace.RequestID, occurredAt, mutation.Trace.GroupID, string(payload))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *openAISchedulerObservabilityRepository) LoadOpenAISchedulerObservability(ctx context.Context, cutoff time.Time, groupID *int64) (service.OpenAISchedulerObservabilityPersistentData, error) {
	result := service.OpenAISchedulerObservabilityPersistentData{
		Groups: make([]service.OpenAISchedulerObservabilityGroup, 0),
		Traces: make([]service.OpenAISchedulerObservabilityTrace, 0),
	}
	if r == nil || r.db == nil {
		return result, fmt.Errorf("nil scheduler observability repository")
	}
	metricQuery := `SELECT COALESCE(SUM(request_count),0),COALESCE(SUM(sticky_detected_request_count),0),COALESCE(SUM(sticky_request_count),0),
COALESCE(SUM(switched_request_count),0),COALESCE(SUM(switch_count),0),COALESCE(SUM(cache_read_tokens),0),COALESCE(SUM(cache_eligible_tokens),0)
FROM openai_scheduler_observability_minute_metrics WHERE bucket_start >= $1`
	metricArgs := []any{cutoff.UTC().Truncate(time.Minute)}
	if groupID != nil {
		metricQuery += ` AND group_id = $2`
		metricArgs = append(metricArgs, *groupID)
	}
	var requests, stickyDetected, sticky, switched, switches int64
	if err := r.db.QueryRowContext(ctx, metricQuery, metricArgs...).Scan(&requests, &stickyDetected, &sticky, &switched, &switches,
		&result.Metrics.CacheReadTokens, &result.Metrics.CacheEligibleTokens); err != nil {
		return result, err
	}
	result.Metrics.Requests = int(requests)
	result.Metrics.StickyDetectedRequests = int(stickyDetected)
	result.Metrics.StickyRequests = int(sticky)
	result.Metrics.SwitchedRequests = int(switched)
	result.Metrics.Switches = int(switches)
	if requests > 0 {
		result.Metrics.SwitchRate = float64(switched) / float64(requests)
	}
	if stickyDetected > 0 {
		result.Metrics.StickyHitRate = float64(sticky) / float64(stickyDetected)
	}
	if result.Metrics.CacheEligibleTokens > 0 {
		result.Metrics.FollowUpCacheRate = float64(result.Metrics.CacheReadTokens) / float64(result.Metrics.CacheEligibleTokens)
	}

	groupRows, err := r.db.QueryContext(ctx, `SELECT group_id,MAX(group_name) FROM openai_scheduler_observability_minute_metrics
WHERE bucket_start >= $1 AND group_id > 0 GROUP BY group_id ORDER BY group_id`, cutoff.UTC().Truncate(time.Minute))
	if err != nil {
		return result, err
	}
	for groupRows.Next() {
		var group service.OpenAISchedulerObservabilityGroup
		if err := groupRows.Scan(&group.ID, &group.Name); err != nil {
			_ = groupRows.Close()
			return result, err
		}
		result.Groups = append(result.Groups, group)
	}
	if err := groupRows.Close(); err != nil {
		return result, err
	}

	traceQuery := `SELECT payload FROM openai_scheduler_observability_abnormal_traces WHERE occurred_at >= $1`
	traceArgs := []any{cutoff.UTC()}
	if groupID != nil {
		traceQuery += ` AND group_id = $2`
		traceArgs = append(traceArgs, *groupID)
	}
	traceQuery += ` ORDER BY occurred_at DESC LIMIT 10000`
	rows, err := r.db.QueryContext(ctx, traceQuery, traceArgs...)
	if err != nil {
		return result, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return result, err
		}
		var trace service.OpenAISchedulerObservabilityTrace
		if err := json.Unmarshal(payload, &trace); err != nil {
			return result, err
		}
		if trace.AccountPath == nil {
			trace.AccountPath = []service.OpenAISchedulerObservabilityAccount{}
		}
		if trace.Attempts == nil {
			trace.Attempts = []service.OpenAISchedulerObservabilityAttempt{}
		}
		if trace.Candidates == nil {
			trace.Candidates = []service.OpenAISchedulerObservabilityCandidate{}
		}
		result.Traces = append(result.Traces, trace)
	}
	return result, rows.Err()
}

func (r *openAISchedulerObservabilityRepository) DeleteOpenAISchedulerObservabilityBefore(ctx context.Context, cutoff time.Time) error {
	if r == nil || r.db == nil {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM openai_scheduler_observability_minute_metrics WHERE bucket_start < $1`, cutoff.UTC()); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM openai_scheduler_observability_abnormal_traces WHERE occurred_at < $1`, cutoff.UTC())
	return err
}

func parseRepositorySchedulerTraceTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().UTC()
	}
	return parsed.UTC()
}
