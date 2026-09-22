package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/qqbot"
)

func (s *QQBotService) PreviewBoard(ctx context.Context) ([][]byte, error) {
	cfg, err := s.stored(ctx)
	if err != nil {
		return nil, err
	}
	board, err := s.StatusBoard(ctx, cfg.Config, "")
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, fmt.Errorf("请先开启 V2 渠道监控并保存允许展示的分组")
	}
	return qqbot.RenderBoard(*board)
}

// StatusBoard uses the same platform/group matrix and metric rules as the
// public MonitorChannelCards view, but always restricts the configured groups.
func (s *QQBotService) StatusBoard(ctx context.Context, cfg qqbot.Config, query string) (*qqbot.Board, error) {
	runtime := s.settings.GetChannelMonitorRuntime(ctx)
	if !runtime.Enabled || runtime.Mode != ChannelMonitorModeV2 || len(cfg.GroupIDs) == 0 {
		return nil, nil
	}
	filter, err := s.v2.ParseFilter("90m", nil, nil, cfg.GroupIDs)
	if err != nil {
		return nil, err
	}
	matrix, err := s.v2.Matrix(ctx, filter, ChannelMonitorV2GroupByPlatformGroup, true)
	if err != nil {
		return nil, err
	}
	if matrix == nil {
		return nil, fmt.Errorf("channel matrix unavailable")
	}
	board := qqBoardFromMatrix(matrix, cfg.GroupIDs, query, time.Now())
	board.Title = s.settings.GetSiteName(ctx)
	return &board, nil
}

func qqBoardFromMatrix(matrix *ChannelMonitorV2Matrix, allowed []int64, query string, now time.Time) qqbot.Board {
	board := qqbot.Board{Subtitle: "近 90 分钟 · 5 分钟 / 格 · 最近监控结果，非即时实测", Through: matrix.Coverage.DataThrough, Stale: matrix.Coverage.DataThrough.IsZero() || now.Sub(matrix.Coverage.DataThrough) > 15*time.Minute}
	for _, row := range matrix.Items {
		if row.GroupID == nil || !qqContainsID(allowed, *row.GroupID) || !qqMatches(query, row.GroupName, row.Platform, row.Model) {
			continue
		}
		card := qqbot.Card{Platform: row.Platform, Name: row.GroupName, Model: row.Model, State: row.Health.Overall, Cache: "-", Availability: "-", TTFT: "-", AvailabilityState: row.Health.ErrorRate, TTFTState: row.Health.TTFT}
		metric := row.Metrics
		if metric.AvailabilityRate != nil {
			card.Availability = qqPercent(*metric.AvailabilityRate)
		} else if metric.RequestCount > 0 {
			card.Availability = qqPercent(1 - metric.ErrorRate)
		}
		if metric.RequestCount > 0 || metric.AvailabilitySource == "traffic" || metric.AvailabilitySource == "mixed" || metric.CacheRate > 0 || metric.TTFT.P50Ms != nil || metric.Duration.P50Ms != nil {
			card.Cache = qqPercent(metric.CacheRate)
		}
		if metric.TTFT.P50Ms != nil {
			ms := *metric.TTFT.P50Ms
			if ms >= 1000 {
				card.TTFT = fmt.Sprintf("%.1fs", float64(ms)/1000)
			} else {
				card.TTFT = fmt.Sprintf("%dms", ms)
			}
		} else {
			card.TTFTState = "unknown"
		}
		step := matrix.Coverage.BucketSeconds
		if step < 60 {
			step = 300
		}
		start, end := matrix.Coverage.RequestedStart, matrix.Coverage.RequestedEnd
		if end.IsZero() {
			end = matrix.Coverage.DataThrough
		}
		if start.IsZero() || !start.Before(end) {
			start = end.Add(-90 * time.Minute)
		}
		buckets := map[int64]ChannelMonitorV2TrendPoint{}
		for _, bucket := range row.Buckets {
			buckets[bucket.BucketStart.Unix()] = bucket
		}
		// Missing buckets are preserved as grey slots, including partial backfill.
		for at := start.Truncate(time.Duration(step) * time.Second); at.Before(end) && len(card.History) < 72; at = at.Add(time.Duration(step) * time.Second) {
			state := "unknown"
			if bucket, ok := buckets[at.Unix()]; ok {
				if bucket.Health.Score != nil && !math.IsNaN(*bucket.Health.Score) {
					band := math.Round(*bucket.Health.Score / 10)
					switch {
					case band >= 8:
						state = "healthy"
					case band >= 5:
						state = "warning"
					default:
						state = "critical"
					}
				} else if bucket.Metrics.RequestCount > 0 {
					state = bucket.Health.Overall
				}
			}
			card.History = append(card.History, state)
		}
		board.Cards = append(board.Cards, card)
	}
	return board
}

func qqPercent(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "-"
	}
	if value < 0.01 {
		return fmt.Sprintf("%.2f%%", value*100)
	}
	return fmt.Sprintf("%.1f%%", value*100)
}
