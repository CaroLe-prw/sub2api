//go:build unit

package admin

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBuildListItemResponseIncludesPreviewDataForPrivateDisabledMonitor(t *testing.T) {
	latency := 3379
	ping := 741
	checkedAt := time.Date(2026, 8, 13, 12, 34, 56, 0, time.UTC)
	monitor := &service.ChannelMonitor{
		ID:            42,
		Name:          "internal-upstream",
		Provider:      service.MonitorProviderOpenAI,
		PrimaryModel:  "gpt-5.6-sol",
		Enabled:       false,
		PublicVisible: false,
		CreatedAt:     checkedAt,
		UpdatedAt:     checkedAt,
	}
	summary := service.MonitorStatusSummary{
		PrimaryStatus:    service.MonitorStatusOperational,
		PrimaryLatencyMs: &latency,
		Availability7d:   99.48,
		ObservedModels: []service.MonitorObservedModelStatus{
			{
				Model:         monitor.PrimaryModel,
				Status:        service.MonitorStatusOperational,
				LatencyMs:     &latency,
				PingLatencyMs: &ping,
				CheckedAt:     &checkedAt,
			},
		},
	}
	timeline := []service.UserMonitorTimelinePoint{
		{
			Status:        service.MonitorStatusOperational,
			LatencyMs:     &latency,
			PingLatencyMs: &ping,
			CheckedAt:     checkedAt,
		},
	}

	response := buildListItemResponse(monitor, summary, timeline)

	require.False(t, response.PublicVisible)
	require.False(t, response.Enabled)
	require.Equal(t, &ping, response.PrimaryPingLatencyMs)
	require.Equal(t, 99.48, response.Availability7d)
	require.Len(t, response.Timeline, 1)
	require.Equal(t, service.MonitorStatusOperational, response.Timeline[0].Status)
	require.Equal(t, checkedAt.Format(time.RFC3339), response.Timeline[0].CheckedAt)
}
