package service

import (
	"context"
	"time"
)

// ScheduledTestPlan represents a scheduled test plan domain model.
type ScheduledTestPlan struct {
	ID             int64      `json:"id"`
	AccountID      int64      `json:"account_id"`
	ModelID        string     `json:"model_id"`
	CronExpression string     `json:"cron_expression"`
	Enabled        bool       `json:"enabled"`
	MaxResults     int        `json:"max_results"`
	AutoRecover    bool       `json:"auto_recover"`
	ManagedBy      string     `json:"managed_by"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const ScheduledTestManagedByChannelMonitor = "channel_monitor"

// ChannelMonitorPoolModel is the admin-only active-probe snapshot for one
// account/model pair. Scheduled account tests are streaming by design.
type ChannelMonitorPoolModel struct {
	PlanID        int64                         `json:"plan_id"`
	Model         string                        `json:"model"`
	Enabled       bool                          `json:"enabled"`
	Status        string                        `json:"status"`
	LatencyMs     *int64                        `json:"latency_ms"`
	Availability  *float64                      `json:"availability"`
	SampleCount   int64                         `json:"sample_count"`
	FailureCount  int64                         `json:"failure_count"`
	LastCheckedAt *time.Time                    `json:"last_checked_at"`
	RecentResults []ChannelMonitorPoolHeartbeat `json:"recent_results"`
}

// ChannelMonitorPoolHeartbeat is the compact result shape embedded in the
// pool overview. It keeps overview payloads small while still supporting the
// admin heartbeat tooltips without one history request per plan.
type ChannelMonitorPoolHeartbeat struct {
	ID         int64     `json:"id"`
	PlanID     int64     `json:"plan_id"`
	Status     string    `json:"status"`
	TTFTMs     *int64    `json:"ttft_ms"`
	LatencyMs  int64     `json:"latency_ms"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// ChannelMonitorPoolAccount groups automatically managed probes by upstream
// account so the admin UI can switch between account and model perspectives.
type ChannelMonitorPoolAccount struct {
	AccountID   int64                     `json:"account_id"`
	Name        string                    `json:"name"`
	Platform    string                    `json:"platform"`
	Type        string                    `json:"type"`
	Status      string                    `json:"status"`
	Schedulable bool                      `json:"schedulable"`
	Concurrency int                       `json:"concurrency"`
	Models      []ChannelMonitorPoolModel `json:"models"`
}

// ScheduledTestResult represents a single test execution result.
type ScheduledTestResult struct {
	ID           int64     `json:"id"`
	PlanID       int64     `json:"plan_id"`
	Status       string    `json:"status"`
	ResponseText string    `json:"response_text"`
	ErrorMessage string    `json:"error_message"`
	TTFTMs       *int64    `json:"ttft_ms"`
	LatencyMs    int64     `json:"latency_ms"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// ScheduledTestPlanRepository defines the data access interface for test plans.
type ScheduledTestPlanRepository interface {
	Create(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	GetByID(ctx context.Context, id int64) (*ScheduledTestPlan, error)
	ListByAccountID(ctx context.Context, accountID int64) ([]*ScheduledTestPlan, error)
	ListDue(ctx context.Context, now time.Time) ([]*ScheduledTestPlan, error)
	Update(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	Delete(ctx context.Context, id int64) error
	UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error
	ReconcileChannelMonitorPlans(ctx context.Context, desired []*ScheduledTestPlan) error
	ListChannelMonitorPoolOverview(ctx context.Context, since time.Time) ([]*ChannelMonitorPoolAccount, error)
}

// ScheduledTestResultRepository defines the data access interface for test results.
type ScheduledTestResultRepository interface {
	Create(ctx context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error)
	ListByPlanID(ctx context.Context, planID int64, limit int) ([]*ScheduledTestResult, error)
	PruneOldResults(ctx context.Context, planID int64, keepCount int) error
}
