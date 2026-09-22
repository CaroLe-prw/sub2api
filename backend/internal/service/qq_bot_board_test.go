package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQQBoardMirrorsCardMetricsAndPreservesGaps(t *testing.T) {
	now := time.Now().Truncate(5 * time.Minute)
	start := now.Add(-90 * time.Minute)
	id, privateID := int64(1), int64(2)
	availability, score := 0.995, 90.0
	ttft := int64(3000)
	matrix := &ChannelMonitorV2Matrix{Coverage: ChannelMonitorV2Coverage{RequestedStart: start, RequestedEnd: now, DataThrough: now, BucketSeconds: 300}, Items: []ChannelMonitorV2MatrixRow{
		{GroupID: &id, GroupName: "公开渠道", Platform: "openai", Metrics: ChannelMonitorV2Metric{AvailabilityRate: &availability, CacheRate: 0.81, TTFT: ChannelMonitorV2Latency{P50Ms: &ttft}}, Health: ChannelMonitorV2Health{Overall: "healthy"}, Buckets: []ChannelMonitorV2TrendPoint{{BucketStart: start, Health: ChannelMonitorV2Health{Score: &score, Overall: "healthy"}}}},
		{GroupID: &privateID, GroupName: "不可公开", Platform: "openai"},
	}}
	board := qqBoardFromMatrix(matrix, []int64{id}, "", now)
	require.Len(t, board.Cards, 1)
	card := board.Cards[0]
	require.Equal(t, "99.5%", card.Availability)
	require.Equal(t, "81.0%", card.Cache)
	require.Equal(t, "3.0s", card.TTFT)
	require.Len(t, card.History, 18)
	require.Equal(t, "healthy", card.History[0])
	require.Equal(t, "unknown", card.History[1])
	matrix.Items[0].Metrics = ChannelMonitorV2Metric{}
	board = qqBoardFromMatrix(matrix, []int64{id}, "", now.Add(time.Hour))
	require.True(t, board.Stale)
	require.Equal(t, "-", board.Cards[0].Availability)
	require.Equal(t, "-", board.Cards[0].Cache)
	require.Equal(t, "-", board.Cards[0].TTFT)
	require.Empty(t, qqBoardFromMatrix(matrix, []int64{id}, "missing", now).Cards)
}
