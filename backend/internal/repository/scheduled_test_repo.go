package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// --- Plan Repository ---

type scheduledTestPlanRepository struct {
	db *sql.DB
}

func NewScheduledTestPlanRepository(db *sql.DB) service.ScheduledTestPlanRepository {
	return &scheduledTestPlanRepository{db: db}
}

func (r *scheduledTestPlanRepository) Create(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_plans (account_id, model_id, cron_expression, enabled, max_results, auto_recover, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		RETURNING id, account_id, model_id, cron_expression, enabled, max_results, auto_recover, managed_by, last_run_at, next_run_at, created_at, updated_at
	`, plan.AccountID, plan.ModelID, plan.CronExpression, plan.Enabled, plan.MaxResults, plan.AutoRecover, plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) GetByID(ctx context.Context, id int64) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover, managed_by, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans WHERE id = $1
	`, id)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) ListByAccountID(ctx context.Context, accountID int64) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover, managed_by, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans WHERE account_id = $1
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) ListDue(ctx context.Context, now time.Time) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, model_id, cron_expression, enabled, max_results, auto_recover, managed_by, last_run_at, next_run_at, created_at, updated_at
		FROM scheduled_test_plans
		WHERE enabled = true AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) Update(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE scheduled_test_plans
		SET model_id = $2, cron_expression = $3, enabled = $4, max_results = $5, auto_recover = $6, next_run_at = $7, updated_at = NOW()
		WHERE id = $1
		RETURNING id, account_id, model_id, cron_expression, enabled, max_results, auto_recover, managed_by, last_run_at, next_run_at, created_at, updated_at
	`, plan.ID, plan.ModelID, plan.CronExpression, plan.Enabled, plan.MaxResults, plan.AutoRecover, plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_test_plans WHERE id = $1`, id)
	return err
}

func (r *scheduledTestPlanRepository) UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_test_plans SET last_run_at = $2, next_run_at = $3, updated_at = NOW() WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

func (r *scheduledTestPlanRepository) ReconcileChannelMonitorPlans(ctx context.Context, desired []*service.ScheduledTestPlan) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	accountIDs := make([]int64, 0, len(desired))
	models := make([]string, 0, len(desired))
	for _, plan := range desired {
		if plan == nil || plan.AccountID <= 0 || plan.ModelID == "" {
			continue
		}
		accountIDs = append(accountIDs, plan.AccountID)
		models = append(models, plan.ModelID)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO scheduled_test_plans
				(account_id, model_id, cron_expression, enabled, max_results, auto_recover, managed_by, next_run_at, created_at, updated_at)
			VALUES ($1, $2, $3, true, $4, $5, $6, $7, NOW(), NOW())
			ON CONFLICT (account_id, model_id) WHERE managed_by = 'channel_monitor'
			DO UPDATE SET enabled = true, cron_expression = EXCLUDED.cron_expression,
				max_results = EXCLUDED.max_results, auto_recover = EXCLUDED.auto_recover, updated_at = NOW()
		`, plan.AccountID, plan.ModelID, plan.CronExpression, plan.MaxResults,
			plan.AutoRecover, service.ScheduledTestManagedBySchedulerProbe, plan.NextRunAt); err != nil {
			return fmt.Errorf("upsert channel monitor plan: %w", err)
		}
	}

	if len(accountIDs) == 0 {
		_, err = tx.ExecContext(ctx, `
			UPDATE scheduled_test_plans SET enabled = false, updated_at = NOW()
			WHERE managed_by = $1 AND enabled = true
		`, service.ScheduledTestManagedBySchedulerProbe)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE scheduled_test_plans p SET enabled = false, updated_at = NOW()
			WHERE p.managed_by = $1 AND p.enabled = true
			  AND NOT EXISTS (
				SELECT 1 FROM unnest($2::bigint[], $3::text[]) AS desired(account_id, model_id)
				WHERE desired.account_id = p.account_id AND desired.model_id = p.model_id
			  )
		`, service.ScheduledTestManagedBySchedulerProbe, pq.Array(accountIDs), pq.Array(models))
	}
	if err != nil {
		return fmt.Errorf("disable stale channel monitor plans: %w", err)
	}
	return tx.Commit()
}

func (r *scheduledTestPlanRepository) ListChannelMonitorPoolOverview(ctx context.Context, since time.Time, accountIDs []int64) ([]*service.ChannelMonitorPoolAccount, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.id, p.account_id, a.name, a.platform, a.type, a.status, a.schedulable, a.concurrency,
		       p.model_id, p.enabled,
		       latest.status, latest.latency_ms, latest.finished_at,
		       stats.sample_count, stats.failure_count, stats.availability,
		       heartbeat.recent_results
		FROM scheduled_test_plans p
		JOIN accounts a ON a.id = p.account_id AND a.deleted_at IS NULL
		LEFT JOIN LATERAL (
			SELECT r.status, r.latency_ms, r.finished_at
			FROM scheduled_test_results r WHERE r.plan_id = p.id
			ORDER BY r.created_at DESC LIMIT 1
		) latest ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::bigint AS sample_count,
			       COUNT(*) FILTER (WHERE r.status <> 'success')::bigint AS failure_count,
			       CASE WHEN COUNT(*) = 0 THEN NULL
			            ELSE ROUND(100.0 * COUNT(*) FILTER (WHERE r.status = 'success') / COUNT(*), 2)::float8
			       END AS availability
			FROM scheduled_test_results r WHERE r.plan_id = p.id AND r.created_at >= $2
		) stats ON true
		LEFT JOIN LATERAL (
			SELECT COALESCE(jsonb_agg(jsonb_build_object(
				'id', recent.id,
				'plan_id', recent.plan_id,
				'status', recent.status,
				'ttft_ms', recent.ttft_ms,
				'latency_ms', recent.latency_ms,
				'started_at', recent.started_at,
				'finished_at', recent.finished_at,
				'created_at', recent.created_at
			) ORDER BY recent.created_at ASC), '[]'::jsonb) AS recent_results
			FROM (
				SELECT r.id, r.plan_id, r.status, r.ttft_ms, r.latency_ms,
				       r.started_at, r.finished_at, r.created_at
				FROM scheduled_test_results r
				WHERE r.plan_id = p.id
				ORDER BY r.created_at DESC
				LIMIT 12
			) recent
		) heartbeat ON true
		WHERE p.managed_by = $1 AND p.enabled = true
		  AND (COALESCE(cardinality($3::bigint[]), 0) = 0 OR p.account_id = ANY($3::bigint[]))
		ORDER BY a.priority ASC, a.id ASC, p.model_id ASC
	`, service.ScheduledTestManagedBySchedulerProbe, since, pq.Array(accountIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	accounts := make([]*service.ChannelMonitorPoolAccount, 0)
	byID := make(map[int64]*service.ChannelMonitorPoolAccount)
	for rows.Next() {
		var model service.ChannelMonitorPoolModel
		var accountID int64
		var name, platform, accountType, accountStatus string
		var schedulable bool
		var concurrency int
		var latestStatus sql.NullString
		var latestLatency sql.NullInt64
		var latestAt sql.NullTime
		var sampleCount, failureCount int64
		var availability sql.NullFloat64
		var recentResultsJSON []byte
		if err := rows.Scan(
			&model.PlanID, &accountID, &name, &platform, &accountType, &accountStatus, &schedulable, &concurrency,
			&model.Model, &model.Enabled, &latestStatus, &latestLatency, &latestAt,
			&sampleCount, &failureCount, &availability, &recentResultsJSON,
		); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(recentResultsJSON, &model.RecentResults); err != nil {
			return nil, fmt.Errorf("decode channel monitor recent results: %w", err)
		}
		if latestStatus.Valid {
			model.Status = latestStatus.String
		}
		if latestLatency.Valid {
			value := latestLatency.Int64
			model.LatencyMs = &value
		}
		if latestAt.Valid {
			value := latestAt.Time
			model.LastCheckedAt = &value
		}
		if availability.Valid {
			value := availability.Float64
			model.Availability = &value
		}
		model.SampleCount = sampleCount
		model.FailureCount = failureCount

		account := byID[accountID]
		if account == nil {
			account = &service.ChannelMonitorPoolAccount{
				AccountID: accountID, Name: name, Platform: platform, Type: accountType,
				Status: accountStatus, Schedulable: schedulable, Concurrency: concurrency,
				Models: []service.ChannelMonitorPoolModel{},
			}
			byID[accountID] = account
			accounts = append(accounts, account)
		}
		account.Models = append(account.Models, model)
	}
	return accounts, rows.Err()
}

// --- Result Repository ---

type scheduledTestResultRepository struct {
	db *sql.DB
}

func NewScheduledTestResultRepository(db *sql.DB) service.ScheduledTestResultRepository {
	return &scheduledTestResultRepository{db: db}
}

func (r *scheduledTestResultRepository) Create(ctx context.Context, result *service.ScheduledTestResult) (*service.ScheduledTestResult, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_results (plan_id, status, response_text, error_message, ttft_ms, latency_ms, started_at, finished_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		RETURNING id, plan_id, status, response_text, error_message, ttft_ms, latency_ms, started_at, finished_at, created_at
	`, result.PlanID, result.Status, result.ResponseText, result.ErrorMessage, result.TTFTMs, result.LatencyMs, result.StartedAt, result.FinishedAt)

	out := &service.ScheduledTestResult{}
	if err := row.Scan(
		&out.ID, &out.PlanID, &out.Status, &out.ResponseText, &out.ErrorMessage,
		&out.TTFTMs, &out.LatencyMs, &out.StartedAt, &out.FinishedAt, &out.CreatedAt,
	); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *scheduledTestResultRepository) ListByPlanID(ctx context.Context, planID int64, limit int) ([]*service.ScheduledTestResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, status, response_text, error_message, ttft_ms, latency_ms, started_at, finished_at, created_at
		FROM scheduled_test_results
		WHERE plan_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []*service.ScheduledTestResult
	for rows.Next() {
		r := &service.ScheduledTestResult{}
		if err := rows.Scan(
			&r.ID, &r.PlanID, &r.Status, &r.ResponseText, &r.ErrorMessage,
			&r.TTFTMs, &r.LatencyMs, &r.StartedAt, &r.FinishedAt, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (r *scheduledTestResultRepository) PruneOldResults(ctx context.Context, planID int64, keepCount int) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM scheduled_test_results
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY plan_id ORDER BY created_at DESC) AS rn
				FROM scheduled_test_results
				WHERE plan_id = $1
			) ranked
			WHERE rn > $2
		)
	`, planID, keepCount)
	return err
}

// --- scan helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanPlan(row scannable) (*service.ScheduledTestPlan, error) {
	p := &service.ScheduledTestPlan{}
	if err := row.Scan(
		&p.ID, &p.AccountID, &p.ModelID, &p.CronExpression, &p.Enabled, &p.MaxResults, &p.AutoRecover,
		&p.ManagedBy, &p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return p, nil
}

func scanPlans(rows *sql.Rows) ([]*service.ScheduledTestPlan, error) {
	var plans []*service.ScheduledTestPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}
