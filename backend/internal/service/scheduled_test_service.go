package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var scheduledTestCronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ScheduledTestService provides CRUD operations for scheduled test plans and results.
type ScheduledTestService struct {
	planRepo        ScheduledTestPlanRepository
	resultRepo      ScheduledTestResultRepository
	userTrafficRepo schedulerUserTrafficRepository
}

type schedulerUserTrafficRepository interface {
	GetSchedulerUserTrafficSnapshots(ctx context.Context, since time.Time, accountIDs []int64) ([]OpenAISchedulerHealthSnapshot, error)
}

const schedulerUserTrafficWindow = 30 * time.Minute

// NewScheduledTestService creates a new ScheduledTestService.
func NewScheduledTestService(
	planRepo ScheduledTestPlanRepository,
	resultRepo ScheduledTestResultRepository,
) *ScheduledTestService {
	return &ScheduledTestService{
		planRepo:   planRepo,
		resultRepo: resultRepo,
	}
}

func (s *ScheduledTestService) SetSchedulerUserTrafficRepository(repo schedulerUserTrafficRepository) {
	s.userTrafficRepo = repo
}

// CreatePlan validates the cron expression, computes next_run_at, and persists the plan.
func (s *ScheduledTestService) CreatePlan(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	plan.NextRunAt = &nextRun

	if plan.MaxResults <= 0 {
		plan.MaxResults = 50
	}

	return s.planRepo.Create(ctx, plan)
}

// GetPlan retrieves a plan by ID.
func (s *ScheduledTestService) GetPlan(ctx context.Context, id int64) (*ScheduledTestPlan, error) {
	return s.planRepo.GetByID(ctx, id)
}

// ListPlansByAccount returns all plans for a given account.
func (s *ScheduledTestService) ListPlansByAccount(ctx context.Context, accountID int64) ([]*ScheduledTestPlan, error) {
	return s.planRepo.ListByAccountID(ctx, accountID)
}

// UpdatePlan validates cron and updates the plan.
func (s *ScheduledTestService) UpdatePlan(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	nextRun, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	plan.NextRunAt = &nextRun

	return s.planRepo.Update(ctx, plan)
}

// DeletePlan removes a plan and its results (via CASCADE).
func (s *ScheduledTestService) DeletePlan(ctx context.Context, id int64) error {
	return s.planRepo.Delete(ctx, id)
}

// ListResults returns the most recent results for a plan.
func (s *ScheduledTestService) ListResults(ctx context.Context, planID int64, limit int) ([]*ScheduledTestResult, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.resultRepo.ListByPlanID(ctx, planID, limit)
}

func (s *ScheduledTestService) ListChannelMonitorPoolOverview(ctx context.Context, accountIDs []int64) ([]*ChannelMonitorPoolAccount, error) {
	accounts, err := s.planRepo.ListChannelMonitorPoolOverview(ctx, time.Now().Add(-7*24*time.Hour), accountIDs)
	if err != nil || s.userTrafficRepo == nil {
		return accounts, err
	}

	snapshots, err := s.userTrafficRepo.GetSchedulerUserTrafficSnapshots(ctx, time.Now().Add(-schedulerUserTrafficWindow), accountIDs)
	if err != nil {
		return nil, err
	}
	type trafficKey struct {
		accountID int64
		model     string
	}
	byModel := make(map[trafficKey]OpenAISchedulerHealthSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byModel[trafficKey{accountID: snapshot.AccountID, model: strings.ToLower(strings.TrimSpace(snapshot.Model))}] = snapshot
	}
	for _, account := range accounts {
		if account == nil {
			continue
		}
		for i := range account.Models {
			model := &account.Models[i]
			snapshot := byModel[trafficKey{accountID: account.AccountID, model: strings.ToLower(strings.TrimSpace(model.Model))}]
			model.UserTraffic = &ChannelMonitorUserTraffic{
				WindowMinutes: int(schedulerUserTrafficWindow / time.Minute),
				SuccessCount:  snapshot.SuccessCount,
				FailureCount:  snapshot.FailureCount,
				AvgTTFTMs:     snapshot.AvgTTFTMs,
				LastSuccessAt: snapshot.LastSuccessAt,
				LastFailureAt: snapshot.LastFailureAt,
			}
		}
	}
	return accounts, nil
}

// SaveResult inserts a result and prunes old entries beyond maxResults.
func (s *ScheduledTestService) SaveResult(ctx context.Context, planID int64, maxResults int, result *ScheduledTestResult) error {
	result.PlanID = planID
	created, err := s.resultRepo.Create(ctx, result)
	if err != nil {
		return err
	}
	*result = *created
	return s.resultRepo.PruneOldResults(ctx, planID, maxResults)
}

func computeNextRun(cronExpr string, from time.Time) (time.Time, error) {
	sched, err := scheduledTestCronParser.Parse(cronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(from), nil
}
