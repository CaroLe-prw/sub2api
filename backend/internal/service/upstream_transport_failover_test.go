//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamTransportFailoverErrorRetriesWhenInboundRequestIsAlive(t *testing.T) {
	err := upstreamTransportFailoverError(context.Background(), context.Canceled)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.True(t, failoverErr.RetryableOnSameAccount)
	require.True(t, failoverErr.RequestScopedTransient)
	require.True(t, failoverErr.ShouldRetryNextAccount())
}

func TestUpstreamTransportFailoverErrorStopsWhenClientCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	original := errors.New("transport stopped")

	err := upstreamTransportFailoverError(ctx, original)

	require.ErrorIs(t, err, original)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
}
