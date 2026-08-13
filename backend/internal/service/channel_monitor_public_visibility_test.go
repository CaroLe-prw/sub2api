//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type publicChannelMonitorRepoStub struct {
	ChannelMonitorRepository
	monitors        []*ChannelMonitor
	listPublicCalls int
	listAllCalls    int
}

func (r *publicChannelMonitorRepoStub) Update(_ context.Context, monitor *ChannelMonitor) error {
	for i, existing := range r.monitors {
		if existing.ID == monitor.ID {
			r.monitors[i] = monitor
			return nil
		}
	}
	return ErrChannelMonitorNotFound
}

func (r *publicChannelMonitorRepoStub) ListEnabled(_ context.Context) ([]*ChannelMonitor, error) {
	r.listAllCalls++
	return r.monitors, nil
}

func (r *publicChannelMonitorRepoStub) ListPublicEnabled(_ context.Context) ([]*ChannelMonitor, error) {
	r.listPublicCalls++
	visible := make([]*ChannelMonitor, 0, len(r.monitors))
	for _, monitor := range r.monitors {
		if monitor.Enabled && monitor.PublicVisible {
			visible = append(visible, monitor)
		}
	}
	return visible, nil
}

func (r *publicChannelMonitorRepoStub) GetByID(_ context.Context, id int64) (*ChannelMonitor, error) {
	for _, monitor := range r.monitors {
		if monitor.ID == id {
			return monitor, nil
		}
	}
	return nil, ErrChannelMonitorNotFound
}

func (r *publicChannelMonitorRepoStub) ListLatestForMonitorIDs(_ context.Context, _ []int64) (map[int64][]*ChannelMonitorLatest, error) {
	return map[int64][]*ChannelMonitorLatest{}, nil
}

func (r *publicChannelMonitorRepoStub) ComputeAvailabilityForMonitors(_ context.Context, _ []int64, _ int) (map[int64][]*ChannelMonitorAvailability, error) {
	return map[int64][]*ChannelMonitorAvailability{}, nil
}

func (r *publicChannelMonitorRepoStub) ListRecentHistoryForMonitors(_ context.Context, _ []int64, _ map[int64]string, _ int) (map[int64][]*ChannelMonitorHistoryEntry, error) {
	return map[int64][]*ChannelMonitorHistoryEntry{}, nil
}

func TestListUserViewUsesExplicitPublicAllowlist(t *testing.T) {
	repo := &publicChannelMonitorRepoStub{monitors: []*ChannelMonitor{
		{ID: 1, Name: "internal-upstream", Enabled: true, PublicVisible: false, PrimaryModel: "internal-model"},
		{ID: 2, Name: "public-status", Enabled: true, PublicVisible: true, PrimaryModel: "public-model"},
		{ID: 3, Name: "disabled-public", Enabled: false, PublicVisible: true, PrimaryModel: "disabled-model"},
	}}
	svc := NewChannelMonitorService(repo, nil)

	views, err := svc.ListUserView(context.Background())

	require.NoError(t, err)
	require.Len(t, views, 1)
	require.Equal(t, int64(2), views[0].ID)
	require.Equal(t, "public-status", views[0].Name)
	require.Equal(t, 1, repo.listPublicCalls)
	require.Zero(t, repo.listAllCalls, "user view must never read the scheduler/admin monitor set")
}

func TestGetUserDetailHidesInternalMonitorAsNotFound(t *testing.T) {
	repo := &publicChannelMonitorRepoStub{monitors: []*ChannelMonitor{
		{ID: 1, Name: "internal-upstream", Enabled: true, PublicVisible: false},
	}}
	svc := NewChannelMonitorService(repo, nil)

	detail, err := svc.GetUserDetail(context.Background(), 1)

	require.Nil(t, detail)
	require.ErrorIs(t, err, ErrChannelMonitorNotFound)
}

func TestPublishRequiresExactMonitorNameAndUnpublishDoesNot(t *testing.T) {
	repo := &publicChannelMonitorRepoStub{monitors: []*ChannelMonitor{{ID: 1, Name: "internal-upstream"}}}
	svc := NewChannelMonitorService(repo, nil)

	published, err := svc.SetPublicVisibility(context.Background(), 1, true, "wrong-name")
	require.Nil(t, published)
	require.ErrorIs(t, err, ErrChannelMonitorPublishNameMismatch)
	require.False(t, repo.monitors[0].PublicVisible)

	published, err = svc.SetPublicVisibility(context.Background(), 1, true, "internal-upstream")
	require.NoError(t, err)
	require.True(t, published.PublicVisible)

	unpublished, err := svc.SetPublicVisibility(context.Background(), 1, false, "")
	require.NoError(t, err)
	require.False(t, unpublished.PublicVisible)
}
