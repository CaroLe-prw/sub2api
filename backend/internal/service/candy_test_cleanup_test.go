package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"
)

func TestPruneCandyTestHistoryPreservesStateAndBoundary(t *testing.T) {
	cutoff := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"total":4,"passed":1,"failed":2,"errors":1,"last_verdict":"failed","consecutive_failures":2,"recent":[{"status":"failed","checked_at":"2026-11-23T11:59:59.999999999Z","response_text":"expired answer"},{"status":"failed","checked_at":"2026-11-23T20:00:00+08:00","response_text":"at boundary"},{"status":"error","checked_at":"2026-11-24T01:00:00Z"}],"schedules":{"7":{"model":"test-model","checked_at":"2026-11-24T01:00:00Z"}},"future_metadata":{"keep":true}}`)
	pruned, err := PruneCandyTestHistory(raw, cutoff)
	require.NoError(t, err)
	require.NotContains(t, string(pruned), "expired answer")
	require.Contains(t, string(pruned), "at boundary")
	var before, after map[string]any
	require.NoError(t, json.Unmarshal(raw, &before))
	require.NoError(t, json.Unmarshal(pruned, &after))
	for k, v := range before {
		if k != "recent" {
			require.Equal(t, v, after[k], k)
		}
	}
	snapshot, err := readCandyTestSnapshot(map[string]any{CandyTestExtraKey: after})
	require.NoError(t, err)
	require.Len(t, snapshot.Recent, 2)
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Schedulable: true, Extra: map[string]any{CandyTestExtraKey: after}}
	require.True(t, account.IsCandyTestBlocked())
	repeated, err := PruneCandyTestHistory(pruned, cutoff)
	require.NoError(t, err)
	require.Nil(t, repeated, "do not rewrite unchanged JSON every day")
}

func TestPruneCandyTestHistoryRetainsLegacyVerdictAndStreak(t *testing.T) {
	for _, tc := range []struct {
		name, statuses, verdict  string
		failures, passed, streak int
	}{
		{"failed", `[{"status":"failed"},{"status":"failed"}]`, "failed", 2, 0, 2},
		{"recovered", `[{"status":"failed"},{"status":"passed"}]`, "passed", 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"total": 2, "failed": tc.failures, "passed": tc.passed, "errors": 0, "recent": json.RawMessage(tc.statuses)})
			require.NoError(t, err)
			pruned, err := PruneCandyTestHistory(raw, time.Now())
			require.NoError(t, err)
			snapshot, err := readCandyTestSnapshot(map[string]any{CandyTestExtraKey: json.RawMessage(pruned)})
			require.NoError(t, err)
			require.NotNil(t, snapshot.Recent)
			require.Empty(t, snapshot.Recent)
			require.Equal(t, tc.verdict, snapshot.LastVerdict)
			require.Equal(t, tc.streak, snapshot.ConsecutiveFailures)
			require.Equal(t, 2, snapshot.Total)
		})
	}
}

func TestPruneCandyTestHistoryRejectsInvalidDataAndBoundsLegacyHistory(t *testing.T) {
	for _, raw := range []string{`broken`, `{"total":9,"passed":1}`, `{"total":0,"consecutive_failures":2}`, `{"recent":[{"checked_at":"invalid"}]}`} {
		_, err := PruneCandyTestHistory([]byte(raw), time.Now())
		require.Error(t, err)
	}
	snapshot := CandyTestSnapshot{Recent: make([]CandyTestSample, 20)}
	now := time.Now()
	for i := range snapshot.Recent {
		snapshot.Recent[i].CheckedAt = now.Add(time.Duration(i) * time.Second)
	}
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	pruned, err := PruneCandyTestHistory(raw, now)
	require.NoError(t, err)
	var got CandyTestSnapshot
	require.NoError(t, json.Unmarshal(pruned, &got))
	require.Len(t, got.Recent, candyTestHistoryLimit)
	require.True(t, snapshot.Recent[8].CheckedAt.Equal(got.Recent[0].CheckedAt))
}

type candyCleanupRepoStub struct {
	AccountRepository
	historyCalls, logCalls int
	cutoffs                []time.Time
	cursors                []int64
	limits                 []int
	historyErr, logErr     error
	onHistory              func(context.Context)
}

func (r *candyCleanupRepoStub) CleanupCandyTestHistory(ctx context.Context, before time.Time, afterID int64, limit int) (int64, int64, error) {
	r.historyCalls++
	r.cutoffs = append(r.cutoffs, before)
	r.cursors = append(r.cursors, afterID)
	r.limits = append(r.limits, limit)
	if r.onHistory != nil {
		r.onHistory(ctx)
	}
	if r.historyErr != nil {
		return afterID, 0, r.historyErr
	}
	if r.historyCalls < 3 {
		return afterID + int64(limit), 1, nil
	}
	return afterID, 0, nil
}
func (r *candyCleanupRepoStub) CleanupAccountTestLogs(_ context.Context, before time.Time, limit int) (int64, error) {
	r.logCalls++
	r.cutoffs = append(r.cutoffs, before)
	r.limits = append(r.limits, limit)
	if r.logErr != nil {
		return 0, r.logErr
	}
	if r.logCalls == 1 {
		return int64(limit), nil
	}
	return 1, nil
}

type candyCleanupGroupsStub struct{ GroupRepository }

func (*candyCleanupGroupsStub) ListCandyTestGroups(context.Context) ([]Group, error) { return nil, nil }

func TestCandyCleanupBatchesWithOneCutoffAndLeaderLock(t *testing.T) {
	repo := &candyCleanupRepoStub{}
	runner := NewCandyTestRunner(nil, repo, nil)
	defer runner.Stop()
	lock := &fakeLeaderLockCache{}
	runner.cleanupLockCache = lock
	now := time.Now()
	runner.cleanupOnce(now)
	require.Equal(t, []int64{0, 100, 200}, repo.cursors)
	require.Equal(t, 2, repo.logCalls)
	for _, cutoff := range repo.cutoffs {
		require.Equal(t, now.Add(-24*time.Hour), cutoff)
	}
	for _, limit := range repo.limits {
		require.Equal(t, candyTestCleanupBatchSize, limit)
	}
	require.Empty(t, lock.heldBy(candyTestCleanupLockKey))

	acquired, err := lock.TryAcquireLeaderLock(context.Background(), candyTestCleanupLockKey, "peer", time.Hour)
	require.NoError(t, err)
	require.True(t, acquired)
	runner.cleanupOnce(now)
	require.Equal(t, 3, repo.historyCalls, "a peer's cleanup must not be duplicated")
	require.Equal(t, "peer", lock.heldBy(candyTestCleanupLockKey))
}

func TestCandyCleanupFailureStillAttemptsLogsAndReleasesLock(t *testing.T) {
	repo := &candyCleanupRepoStub{historyErr: errors.New("read failed"), logErr: errors.New("delete failed")}
	runner := NewCandyTestRunner(nil, repo, nil)
	defer runner.Stop()
	runner.cleanupLockCache = &fakeLeaderLockCache{}
	runner.cleanupOnce(time.Now())
	require.Equal(t, 1, repo.historyCalls)
	require.Equal(t, 1, repo.logCalls)
	require.Empty(t, runner.cleanupLockCache.(*fakeLeaderLockCache).heldBy(candyTestCleanupLockKey))
}

func TestCandyCleanupStartsOnceAndStopsInFlightWork(t *testing.T) {
	entered := make(chan struct{})
	repo := &candyCleanupRepoStub{onHistory: func(ctx context.Context) { close(entered); <-ctx.Done() }}
	runner := NewCandyTestRunner(&candyCleanupGroupsStub{}, repo, nil)
	defer runner.Stop()
	runner.Start()
	runner.Start()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("startup cleanup did not run")
	}
	stopped := make(chan struct{})
	go func() { runner.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("cleanup was not canceled on shutdown")
	}
	runner.Stop()
	runner.cleanupOnce(time.Now())
	require.Equal(t, 1, repo.historyCalls)
	require.Zero(t, repo.logCalls)
}

func TestCandyCleanupDailyScheduleUsesApplicationTimezone(t *testing.T) {
	schedule, err := cron.ParseStandard(candyTestCleanupSchedule)
	require.NoError(t, err)
	location, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	now := time.Date(2026, 11, 24, 3, 29, 59, 0, location)
	first := schedule.Next(now)
	require.Equal(t, "2026-11-24T03:30:00+08:00", first.Format(time.RFC3339))
	require.Equal(t, "2026-11-25T03:30:00+08:00", schedule.Next(first).Format(time.RFC3339))
}
