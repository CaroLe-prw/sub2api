//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorV2MaxChunkForDepth(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	// Within last day → tightest ceiling (2h).
	require.Equal(t, channelMonitorV2MaxChunkNear1d, channelMonitorV2MaxChunkForDepth(now, now.Add(-2*time.Hour)))
	// Between 1d and 7d → 4h.
	require.Equal(t, channelMonitorV2MaxChunkNear7d, channelMonitorV2MaxChunkForDepth(now, now.Add(-2*24*time.Hour)))
	// Older than 7d → 6h (never 24h default).
	require.Equal(t, channelMonitorV2MaxChunkFar, channelMonitorV2MaxChunkForDepth(now, now.Add(-10*24*time.Hour)))
	require.Less(t, channelMonitorV2MaxChunkFar, 24*time.Hour)
	require.Equal(t, time.Hour, channelMonitorV2BackfillChunkInit)
	require.Equal(t, 15*time.Minute, channelMonitorV2MinBackfillChunk)
}

type channelMonitorV2HealthRepairRepoStub struct {
	*channelMonitorV2RepoStub
	repairErr               error
	repairCalls             int
	liveRefreshBeforeRepair bool
}

func (r *channelMonitorV2HealthRepairRepoStub) RepairNextInboundBodyReadHealth(context.Context) (bool, error) {
	r.repairCalls++
	r.liveRefreshBeforeRepair = len(r.recomputeCalls) == r.repairCalls
	return r.repairErr == nil, r.repairErr
}

func TestChannelMonitorV2AggregatorRefreshesLiveTrafficBeforeHistoricalHealthRepair(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "retry failure"}[failure], func(t *testing.T) {
			now := time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)
			repo := &channelMonitorV2HealthRepairRepoStub{channelMonitorV2RepoStub: &channelMonitorV2RepoStub{
				watermark: &ChannelMonitorV2AggregationWatermark{
					HasData: true, DataThrough: now, BackfillCursor: now.Add(-30 * 24 * time.Hour),
				},
			}}
			if failure {
				repo.repairErr = errors.New("repair unavailable")
			}
			aggregator := NewChannelMonitorV2Aggregator(repo, nil, nil)
			aggregator.now = func() time.Time { return now }
			for range 2 {
				aggregator.runOnce()
				require.True(t, repo.liveRefreshBeforeRepair)
				require.Equal(t, now, aggregator.dataThrough)
			}
			require.Equal(t, 2, repo.repairCalls)
			require.Equal(t, [][2]time.Time{
				{now.Add(-channelMonitorV2RecentOverlap), now},
				{now.Add(-channelMonitorV2RecentOverlap), now},
			}, repo.recomputeCalls)
		})
	}
}

func TestChannelMonitorV2AggregatorAdaptiveChunk(t *testing.T) {
	s := NewChannelMonitorV2Aggregator(nil, nil, nil)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cursor := now.Add(-3 * time.Hour)

	// Failure shrinks chunk and sets backoff floor.
	s.backfillChunk = 2 * time.Hour
	s.recordBackfillFailure(now, cursor)
	require.Equal(t, time.Hour, s.backfillChunk)
	require.Equal(t, time.Minute, s.nextWaitFloor)
	require.Equal(t, 1, s.backfillFailures)

	// Repeated failure halves again and raises floor.
	s.recordBackfillFailure(now, cursor)
	require.Equal(t, 30*time.Minute, s.backfillChunk)
	require.Equal(t, 2*time.Minute, s.nextWaitFloor)

	// Fast success grows within depth ceiling and clears backoff.
	s.recordBackfillSuccess(cursor.Add(-30*time.Minute), 5*time.Second, now)
	require.Equal(t, 0, s.backfillFailures)
	require.Equal(t, time.Duration(0), s.nextWaitFloor)
	require.Greater(t, s.backfillChunk, 30*time.Minute)
	require.LessOrEqual(t, s.backfillChunk, channelMonitorV2MaxChunkForDepth(now, cursor.Add(-30*time.Minute)))
}

func TestChannelMonitorV2AggregatorRepairsForwardGapBeforeNormalRefresh(t *testing.T) {
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	dataThrough := now.Add(-6 * time.Hour)
	repo := &channelMonitorV2RepoStub{watermark: &ChannelMonitorV2AggregationWatermark{
		HasData:        true,
		DataThrough:    dataThrough,
		BackfillCursor: now.Add(-30 * 24 * time.Hour),
	}}
	aggregator := NewChannelMonitorV2Aggregator(repo, nil, nil)
	aggregator.now = func() time.Time { return now }

	aggregator.runOnce()
	require.Equal(t, [][2]time.Time{{dataThrough, dataThrough.Add(time.Hour)}}, repo.recomputeCalls)

	aggregator.runOnce()
	require.Equal(t, [][2]time.Time{
		{dataThrough, dataThrough.Add(time.Hour)},
		{dataThrough.Add(time.Hour), dataThrough.Add(2 * time.Hour)},
	}, repo.recomputeCalls)
}
