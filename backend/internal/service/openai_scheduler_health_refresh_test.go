package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type openAISchedulerHealthRepoStub struct {
	UsageLogRepository
	snapshots []OpenAISchedulerHealthSnapshot
	calls     int
}

func (s *openAISchedulerHealthRepoStub) GetOpenAISchedulerHealthSnapshots(
	_ context.Context,
	_ time.Time,
) ([]OpenAISchedulerHealthSnapshot, error) {
	s.calls++
	return s.snapshots, nil
}

func TestOpenAIGatewayService_RefreshOpenAISchedulerHealthHydratesAndThrottles(t *testing.T) {
	repo := &openAISchedulerHealthRepoStub{snapshots: []OpenAISchedulerHealthSnapshot{{
		AccountID: 35, Model: "gpt-5.6-sol", SuccessCount: 8, FailureCount: 2,
	}}}
	svc := &OpenAIGatewayService{usageLogRepo: repo}

	svc.RefreshOpenAISchedulerHealth(context.Background())
	svc.RefreshOpenAISchedulerHealth(context.Background())

	require.Equal(t, 1, repo.calls)
	require.NotNil(t, svc.openaiAccountStats)
	errorRate, _, _ := svc.openaiAccountStats.snapshotForRequest(35, "gpt-5.6-sol")
	require.InDelta(t, 0.2, errorRate, 1e-9)
}
