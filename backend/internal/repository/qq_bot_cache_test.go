package repository

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/qqbot"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestQQModerationDedupIsIndependentOfQueryCooldown(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	cache := NewQQBotCache(client)
	ctx := context.Background()
	ok, err := cache.Claim(ctx, "query", "group", false)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = cache.ClaimModeration(ctx, "ad-a")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = cache.ClaimModeration(ctx, "ad-b")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = cache.ClaimModeration(ctx, "ad-a")
	require.NoError(t, err)
	require.False(t, ok)
	for i := 0; i < 205; i++ {
		require.NoError(t, cache.RecordModeration(ctx, qqbot.ModerationRecord{MessageID: fmt.Sprint(i), Action: "recorded"}))
	}
	rows, err := cache.ModerationRecords(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 200)
	require.Equal(t, "204", rows[0].MessageID)
	mr.FastForward(8 * 24 * time.Hour)
	rows, err = cache.ModerationRecords(ctx)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestQQBotCacheLeaseOwnershipAndStatus(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	cache := NewQQBotCache(rdb)
	ctx := context.Background()
	owned, err := cache.AcquireOrRenewLease(ctx, "first")
	require.NoError(t, err)
	require.True(t, owned)
	mr.FastForward(15 * time.Second)
	owned, err = cache.AcquireOrRenewLease(ctx, "first")
	require.NoError(t, err)
	require.True(t, owned)
	mr.FastForward(10 * time.Second)
	owned, err = cache.AcquireOrRenewLease(ctx, "second")
	require.NoError(t, err)
	require.False(t, owned)
	require.NoError(t, cache.ReleaseLease(ctx, "second"))
	owned, err = cache.AcquireOrRenewLease(ctx, "second")
	require.NoError(t, err)
	require.False(t, owned)
	mr.FastForward(11 * time.Second)
	owned, err = cache.AcquireOrRenewLease(ctx, "second")
	require.NoError(t, err)
	require.True(t, owned)
	require.NoError(t, cache.ReleaseLease(ctx, "first"))
	owned, err = cache.AcquireOrRenewLease(ctx, "first")
	require.NoError(t, err)
	require.False(t, owned)
	require.NoError(t, cache.ReleaseLease(ctx, "second"))
	owned, err = cache.AcquireOrRenewLease(ctx, "first")
	require.NoError(t, err)
	require.True(t, owned)

	want := qqbot.Status{State: "online", Detail: "connected", UpdatedAt: time.Now().UTC()}
	require.NoError(t, cache.SetStatus(ctx, want))
	mr.FastForward(25 * time.Second)
	require.NoError(t, cache.RefreshStatus(ctx))
	mr.FastForward(10 * time.Second)
	got, err := cache.GetStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, want, got)
	mr.FastForward(21 * time.Second)
	_, err = cache.GetStatus(ctx)
	require.ErrorIs(t, err, redis.Nil)
}

func TestQQBotRedisDeduplicationAndCooldown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { require.NoError(t, rdb.Close()) })
	s := NewQQBotCache(rdb)
	ctx := context.Background()
	ok, err := s.Claim(ctx, "message-a", "group-a", true)
	require.NoError(t, err)
	require.True(t, ok)
	mr.FastForward(6 * time.Second)
	ok, err = s.Claim(ctx, "message-a", "group-a", false)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.Claim(ctx, "message-b", "group-a", true)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = s.Claim(ctx, "message-c", "group-a", false)
	require.NoError(t, err)
	require.True(t, ok)
	mr.FastForward(time.Minute)
	ok, err = s.Claim(ctx, "message-d", "group-a", true)
	require.NoError(t, err)
	require.True(t, ok)
}
