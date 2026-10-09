package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAPIKeyHealthCacheTripsWithinRollingWindow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store, ok := NewTempUnschedCache(client).(service.OpenAIAPIKeyHealthCache)
	require.True(t, ok)

	ctx := context.Background()
	for attempt := 1; attempt <= 3; attempt++ {
		count, tripped, err := store.RecordOpenAIAPIKeyHealthFailure(ctx, 42, "gpt-6-astra", 1, 3)
		require.NoError(t, err)
		require.EqualValues(t, attempt, count)
		require.Equal(t, attempt == 3, tripped)
	}
}

func TestOpenAIAPIKeyHealthCacheDropsFailuresOutsideRollingWindow(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Unix(1_700_000_000, 0)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store, ok := NewTempUnschedCache(client).(service.OpenAIAPIKeyHealthCache)
	require.True(t, ok)

	ctx := context.Background()
	count, tripped, err := store.RecordOpenAIAPIKeyHealthFailure(ctx, 42, "gpt-6-astra", 1, 3)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.False(t, tripped)

	server.SetTime(now.Add(61 * time.Second))
	count, tripped, err = store.RecordOpenAIAPIKeyHealthFailure(ctx, 42, "gpt-6-astra", 1, 3)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.False(t, tripped)
}

func TestHealthAndTimeoutCountersSeparateAccountModels(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	health := NewTempUnschedCache(client).(service.OpenAIAPIKeyHealthCache)
	_, tripped, err := health.RecordOpenAIAPIKeyHealthFailure(ctx, 42, "gpt-6-astra", 1, 2)
	require.NoError(t, err)
	require.False(t, tripped)
	count, tripped, err := health.RecordOpenAIAPIKeyHealthFailure(ctx, 42, "gpt-5.6-sol", 1, 2)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.False(t, tripped)
	_, tripped, err = health.RecordOpenAIAPIKeyHealthFailure(ctx, 42, " GPT-6-ASTRA ", 1, 2)
	require.NoError(t, err)
	require.True(t, tripped)
	count, _, err = health.RecordOpenAIAPIKeyHealthFailure(ctx, 43, "gpt-6-astra", 1, 2)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	timeouts := NewTimeoutCounterCache(client)
	_, err = timeouts.IncrementTimeoutCount(ctx, 42, "gpt-6-astra", 1)
	require.NoError(t, err)
	_, err = timeouts.IncrementTimeoutCount(ctx, 42, "gpt-5.6-sol", 1)
	require.NoError(t, err)
	count, err = timeouts.IncrementTimeoutCount(ctx, 42, " GPT-6-ASTRA ", 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.NoError(t, timeouts.ResetTimeoutCount(ctx, 42, "gpt-6-astra"))
	count, err = timeouts.GetTimeoutCount(ctx, 42, "gpt-5.6-sol")
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
