package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduledTestOverviewRepoStub struct {
	accounts []*ChannelMonitorPoolAccount
}

func (s *scheduledTestOverviewRepoStub) Create(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	panic("unexpected Create")
}

func (s *scheduledTestOverviewRepoStub) GetByID(context.Context, int64) (*ScheduledTestPlan, error) {
	panic("unexpected GetByID")
}

func (s *scheduledTestOverviewRepoStub) ListByAccountID(context.Context, int64) ([]*ScheduledTestPlan, error) {
	panic("unexpected ListByAccountID")
}

func (s *scheduledTestOverviewRepoStub) ListDue(context.Context, time.Time) ([]*ScheduledTestPlan, error) {
	panic("unexpected ListDue")
}

func (s *scheduledTestOverviewRepoStub) Update(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	panic("unexpected Update")
}

func (s *scheduledTestOverviewRepoStub) Delete(context.Context, int64) error {
	panic("unexpected Delete")
}

func (s *scheduledTestOverviewRepoStub) UpdateAfterRun(context.Context, int64, time.Time, time.Time) error {
	panic("unexpected UpdateAfterRun")
}

func (s *scheduledTestOverviewRepoStub) ReconcileChannelMonitorPlans(context.Context, []*ScheduledTestPlan) error {
	panic("unexpected ReconcileChannelMonitorPlans")
}

func (s *scheduledTestOverviewRepoStub) ListChannelMonitorPoolOverview(context.Context, time.Time, []int64) ([]*ChannelMonitorPoolAccount, error) {
	return s.accounts, nil
}

type scheduledTestTrafficRepoStub struct {
	snapshots []OpenAISchedulerHealthSnapshot
}

func (s *scheduledTestTrafficRepoStub) GetSchedulerUserTrafficSnapshots(context.Context, time.Time, []int64) ([]OpenAISchedulerHealthSnapshot, error) {
	return s.snapshots, nil
}

func TestScheduledTestService_ListChannelMonitorPoolOverviewIncludesUserTraffic(t *testing.T) {
	avgTTFT := 920.0
	lastSuccess := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	lastFailure := lastSuccess.Add(-time.Minute)
	planRepo := &scheduledTestOverviewRepoStub{accounts: []*ChannelMonitorPoolAccount{{
		AccountID: 35,
		Models:    []ChannelMonitorPoolModel{{PlanID: 81, Model: "GPT-5.6-SOL"}},
	}}}
	svc := NewScheduledTestService(planRepo, nil)
	svc.SetSchedulerUserTrafficRepository(&scheduledTestTrafficRepoStub{snapshots: []OpenAISchedulerHealthSnapshot{{
		AccountID: 35, Model: "gpt-5.6-sol", SuccessCount: 18, FailureCount: 2,
		AvgTTFTMs: &avgTTFT, LastSuccessAt: &lastSuccess, LastFailureAt: &lastFailure,
	}}})

	accounts, err := svc.ListChannelMonitorPoolOverview(context.Background(), []int64{35})
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Len(t, accounts[0].Models, 1)
	require.Equal(t, &ChannelMonitorUserTraffic{
		WindowMinutes: 30,
		SuccessCount:  18,
		FailureCount:  2,
		AvgTTFTMs:     &avgTTFT,
		LastSuccessAt: &lastSuccess,
		LastFailureAt: &lastFailure,
	}, accounts[0].Models[0].UserTraffic)
}
