package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const (
	candyTestCleanupSchedule  = "30 3 * * *"
	candyTestCleanupBatchSize = 100
	candyTestCleanupTimeout   = 10 * time.Minute
	candyTestCleanupLockKey   = "candy-test:cleanup"
)

// Optional repository capability keeps retention independent of enabled groups
// and of the unrelated ops-log retention switch.
type candyTestCleanupRepository interface {
	CleanupCandyTestHistory(context.Context, time.Time, int64, int) (int64, int64, error)
	CleanupAccountTestLogs(context.Context, time.Time, int) (int64, error)
}

func pruneCandyTestSamples(snapshot *CandyTestSnapshot, before time.Time) bool {
	recent := make([]CandyTestSample, 0, len(snapshot.Recent))
	for _, sample := range snapshot.Recent {
		if !sample.CheckedAt.Before(before) {
			recent = append(recent, sample)
		}
	}
	if len(recent) > candyTestHistoryLimit {
		recent = recent[len(recent)-candyTestHistoryLimit:]
	}
	if len(recent) == len(snapshot.Recent) {
		return false
	}
	snapshot.Recent = recent
	return true
}

// PruneCandyTestHistory returns nil if no write is needed. It retains lifetime
// counters, schedule timestamps and unknown fields, and materializes legacy
// verdict/streak metadata BEFORE dropping the answers used to infer them.
// Removing diagnostic history must never unblock an unhealthy account.
func PruneCandyTestHistory(raw []byte, before time.Time) ([]byte, error) {
	snapshot, err := readCandyTestSnapshot(map[string]any{CandyTestExtraKey: json.RawMessage(raw)})
	if err != nil {
		return nil, err
	}
	if !pruneCandyTestSamples(&snapshot, before) {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["recent"], err = json.Marshal(snapshot.Recent)
	if err != nil {
		return nil, err
	}
	fields["last_verdict"], _ = json.Marshal(snapshot.LastVerdict)
	fields["consecutive_failures"], _ = json.Marshal(snapshot.ConsecutiveFailures)
	return json.Marshal(fields)
}

func (r *CandyTestRunner) cleanupLoop() {
	if _, ok := r.accounts.(candyTestCleanupRepository); !ok {
		return
	}
	// The constant spec is valid; the timezone is the application's configured
	// timezone rather than the machine's UTC offset.
	schedule, err := cron.ParseStandard(candyTestCleanupSchedule)
	if err != nil {
		slog.Error("[CandyTest] invalid cleanup schedule", "error", err)
		return
	}
	slog.Info("[CandyTest] cleanup scheduled", "schedule", candyTestCleanupSchedule,
		"timezone", timezone.Location().String(), "retention", candyTestHistoryRetention)
	// Catch up on existing data after an upgrade or downtime.
	r.cleanupOnce(time.Now())
	for {
		now := time.Now().In(timezone.Location())
		timer := time.NewTimer(schedule.Next(now).Sub(now))
		select {
		case <-r.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			r.cleanupOnce(time.Now())
		}
	}
}

func (r *CandyTestRunner) cleanupOnce(now time.Time) {
	repo, ok := r.accounts.(candyTestCleanupRepository)
	if !ok || r.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.ctx, candyTestCleanupTimeout)
	defer cancel()
	lockCtx, cancelLock := context.WithTimeout(ctx, 2*time.Second)
	release, acquired := tryAcquireSingletonLeaderLock(lockCtx, r.cleanupLockCache, r.cleanupLockDB,
		candyTestCleanupLockKey, uuid.NewString(), candyTestCleanupTimeout+time.Minute)
	cancelLock()
	if !acquired {
		return
	}
	defer release()

	cutoff := now.Add(-candyTestHistoryRetention)
	var afterID, accounts, logs int64
	for ctx.Err() == nil {
		nextID, cleaned, err := repo.CleanupCandyTestHistory(ctx, cutoff, afterID, candyTestCleanupBatchSize)
		accounts += cleaned
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("[CandyTest] history cleanup failed", "error", err)
			}
			break // Still attempt log cleanup when one history batch fails.
		}
		if nextID <= afterID {
			break
		}
		afterID = nextID
	}
	for ctx.Err() == nil {
		deleted, err := repo.CleanupAccountTestLogs(ctx, cutoff, candyTestCleanupBatchSize)
		logs += deleted
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("[CandyTest] test log cleanup failed", "error", err)
			}
			break
		}
		if deleted < candyTestCleanupBatchSize {
			break
		}
	}
	if ctx.Err() != nil && r.ctx.Err() == nil {
		slog.Warn("[CandyTest] cleanup timed out", "error", ctx.Err())
	}
	if accounts > 0 || logs > 0 {
		slog.Info("[CandyTest] expired diagnostics cleaned", "accounts", accounts, "test_logs", logs, "before", cutoff)
	}
}
