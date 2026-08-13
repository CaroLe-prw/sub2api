package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountProbeConcurrencyLimiterCapsEachAccountAtTwo(t *testing.T) {
	limiter := newAccountProbeConcurrencyLimiter(channelMonitorMaxConcurrentModelsPerChannel)
	var active atomic.Int64
	var peak atomic.Int64
	start := make(chan struct{})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := limiter.acquire(context.Background(), 42)
			require.NoError(t, err)
			defer release()

			current := active.Add(1)
			for {
				previous := peak.Load()
				if current <= previous || peak.CompareAndSwap(previous, current) {
					break
				}
			}
			time.Sleep(15 * time.Millisecond)
			active.Add(-1)
		}()
	}

	close(start)
	wg.Wait()
	require.Equal(t, int64(2), peak.Load())

	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	require.Empty(t, limiter.accounts, "idle account limiters should be removed")
}

func TestAccountProbeConcurrencyLimiterIsIndependentPerAccount(t *testing.T) {
	limiter := newAccountProbeConcurrencyLimiter(channelMonitorMaxConcurrentModelsPerChannel)
	releases := make([]func(), 0, 4)
	for _, accountID := range []int64{10, 10, 20, 20} {
		release, err := limiter.acquire(context.Background(), accountID)
		require.NoError(t, err)
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}

func TestAccountProbeConcurrencyLimiterWaitHonorsCancellation(t *testing.T) {
	limiter := newAccountProbeConcurrencyLimiter(channelMonitorMaxConcurrentModelsPerChannel)
	first, err := limiter.acquire(context.Background(), 42)
	require.NoError(t, err)
	second, err := limiter.acquire(context.Background(), 42)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = limiter.acquire(ctx, 42)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	first()
	second()
}
