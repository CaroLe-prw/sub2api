package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type schedulerObservabilityRepositoryStub struct {
	batches []OpenAISchedulerObservabilityPersistentBatch
}

func (s *schedulerObservabilityRepositoryStub) ApplyOpenAISchedulerObservabilityBatch(_ context.Context, batch OpenAISchedulerObservabilityPersistentBatch) error {
	s.batches = append(s.batches, batch)
	return nil
}

func (s *schedulerObservabilityRepositoryStub) LoadOpenAISchedulerObservability(context.Context, time.Time, *int64) (OpenAISchedulerObservabilityPersistentData, error) {
	return OpenAISchedulerObservabilityPersistentData{}, nil
}

func (s *schedulerObservabilityRepositoryStub) DeleteOpenAISchedulerObservabilityBefore(context.Context, time.Time) error {
	return nil
}

func TestOpenAISchedulerObservabilityPersistenceCoalescesAndCorrectsTraceState(t *testing.T) {
	repo := &schedulerObservabilityRepositoryStub{}
	persistence := &OpenAISchedulerObservabilityPersistence{
		repo: repo, pending: make(map[string]OpenAISchedulerObservabilityTrace),
		persisted: make(map[string]schedulerObservabilityContribution), wake: make(chan struct{}, 1),
	}
	persistence.enabled.Store(true)
	trace := OpenAISchedulerObservabilityTrace{
		ID: "request-1", RequestID: "request-1", CreatedAt: "2026-08-10T12:01:10Z", Status: "failed",
		AccountPath: []OpenAISchedulerObservabilityAccount{}, Attempts: []OpenAISchedulerObservabilityAttempt{}, Candidates: []OpenAISchedulerObservabilityCandidate{},
	}
	persistence.Enqueue(trace)
	persistence.flush()

	require.Len(t, repo.batches, 1)
	require.Equal(t, int64(1), repo.batches[0].Metrics[0].Requests)
	require.Equal(t, int64(1), repo.batches[0].Metrics[0].FailedRequests)
	require.True(t, repo.batches[0].Traces[0].Abnormal)

	trace.Status = "success"
	trace.CacheReadTokens = 80
	trace.CacheEligibleTokens = 100
	persistence.Enqueue(trace)
	persistence.flush()

	require.Len(t, repo.batches, 2)
	require.Zero(t, repo.batches[1].Metrics[0].Requests)
	require.Equal(t, int64(-1), repo.batches[1].Metrics[0].FailedRequests)
	require.Equal(t, int64(80), repo.batches[1].Metrics[0].CacheReadTokens)
	require.False(t, repo.batches[1].Traces[0].Abnormal)
}
