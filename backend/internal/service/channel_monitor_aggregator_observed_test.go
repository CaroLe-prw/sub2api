//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildStatusSummaryKeepsAutoDiscoveredHistoryModels(t *testing.T) {
	checkedAt := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	latency := 321
	avg := 300
	summary := buildStatusSummary(
		map[string]*ChannelMonitorLatest{
			"gpt-primary": {Model: "gpt-primary"},
			"gpt-auto": {
				Model: "gpt-auto", Status: MonitorStatusOperational,
				LatencyMs: &latency, CheckedAt: checkedAt,
			},
		},
		map[string]*ChannelMonitorAvailability{
			"gpt-auto": {Model: "gpt-auto", TotalChecks: 12, AvailabilityPct: 100, AvgLatencyMs: &avg},
		},
		"gpt-primary",
		nil,
	)

	require.Len(t, summary.ObservedModels, 2)
	require.Equal(t, "gpt-auto", summary.ObservedModels[0].Model)
	require.Equal(t, MonitorStatusOperational, summary.ObservedModels[0].Status)
	require.Equal(t, 12, summary.ObservedModels[0].TotalChecks7d)
	require.Equal(t, checkedAt, *summary.ObservedModels[0].CheckedAt)
}
