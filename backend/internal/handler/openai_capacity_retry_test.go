package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHandleOpenAIOAuthCapacitySelectionExhaustedReleasesLastAccount(t *testing.T) {
	failedAccountIDs := map[int64]struct{}{42: {}}
	retryCounts := make(map[int64]int)
	failoverErr := &service.UpstreamFailoverError{
		StatusCode:                             400,
		RetryableOnSameAccountIfNoOtherAccount: true,
	}

	action := handleOpenAIOAuthCapacitySelectionExhausted(
		context.Background(), failedAccountIDs, failoverErr, 42, retryCounts, nil,
	)

	require.Equal(t, FailoverContinue, action)
	require.NotContains(t, failedAccountIDs, int64(42))
	require.Equal(t, 1, retryCounts[42])
}

func TestHandleOpenAIOAuthCapacitySelectionExhaustedStopsAtRetryLimit(t *testing.T) {
	failedAccountIDs := map[int64]struct{}{42: {}}
	retryCounts := map[int64]int{42: maxSameAccountRetries}
	failoverErr := &service.UpstreamFailoverError{
		RetryableOnSameAccountIfNoOtherAccount: true,
	}

	action := handleOpenAIOAuthCapacitySelectionExhausted(
		context.Background(), failedAccountIDs, failoverErr, 42, retryCounts, nil,
	)

	require.Equal(t, FailoverExhausted, action)
	require.Contains(t, failedAccountIDs, int64(42))
}

func TestHandleOpenAIOAuthCapacitySelectionExhaustedIgnoresOtherErrors(t *testing.T) {
	failedAccountIDs := map[int64]struct{}{42: {}}
	retryCounts := make(map[int64]int)

	action := handleOpenAIOAuthCapacitySelectionExhausted(
		context.Background(), failedAccountIDs, &service.UpstreamFailoverError{}, 42, retryCounts, nil,
	)

	require.Equal(t, FailoverExhausted, action)
	require.Contains(t, failedAccountIDs, int64(42))
	require.Empty(t, retryCounts)
}
