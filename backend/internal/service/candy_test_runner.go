package service

import (
	"context"
	"database/sql"
	"log/slog"
	"sort"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const candyTestMaxWorkers = 4

type candyScheduledTester interface {
	RunScheduledCandyTest(context.Context, int64, int64) (*CandyTestSnapshot, error)
}

// The repository can provide a small enabled-only query without account counts.
type candyTestGroupLister interface {
	ListCandyTestGroups(context.Context) ([]Group, error)
}

type candyTestAccountLister interface {
	ListCandyTestAccounts(context.Context, int64) ([]Account, error)
}

type candyTestJob struct {
	accountID int64
	groupID   int64
	interval  int
	dueAt     time.Time
}

type CandyTestRunner struct {
	groups           GroupRepository
	accounts         AccountRepository
	tester           candyScheduledTester
	ctx              context.Context
	cancel           context.CancelFunc
	startOnce        sync.Once
	stopOnce         sync.Once
	wg               sync.WaitGroup
	slots            chan struct{}
	mu               sync.Mutex
	running          map[int64]bool
	retryAfter       map[[2]int64]time.Time
	cleanupLockCache LeaderLockCache
	cleanupLockDB    *sql.DB
}

func NewCandyTestRunner(groups GroupRepository, accounts AccountRepository, tester *AccountTestService) *CandyTestRunner {
	ctx, cancel := context.WithCancel(context.Background())
	runner := &CandyTestRunner{groups: groups, accounts: accounts, tester: tester, ctx: ctx, cancel: cancel,
		slots: make(chan struct{}, candyTestMaxWorkers), running: make(map[int64]bool), retryAfter: make(map[[2]int64]time.Time)}
	if tester != nil {
		runner.cleanupLockCache = tester.candyTestLockCache
		runner.cleanupLockDB = tester.candyTestLockDB
	}
	return runner
}

func (r *CandyTestRunner) Start() {
	r.startOnce.Do(func() {
		r.wg.Add(2)
		go func() {
			defer r.wg.Done()
			r.cleanupLoop()
		}()
		go func() {
			defer r.wg.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-r.ctx.Done():
					return
				case <-ticker.C:
					r.scan()
				}
			}
		}()
	})
}

func (r *CandyTestRunner) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(r.cancel)
	r.wg.Wait()
}

func candyAutomaticAccountEligible(account *Account, now time.Time) bool {
	// Honor the administrator's switch, not IsSchedulable: candy-blocked
	// accounts with the switch still on must be able to recover through retests.
	return supportsCandyTest(account) && account.Schedulable &&
		account.IsActive() && account.ParentAccountID == nil && !account.IsSyntheticUITest() &&
		(account.ExpiresAt == nil || account.ExpiresAt.After(now))
}

func candyScheduleDueAt(snapshot CandyTestSnapshot, group *Group) time.Time {
	models, err := normalizeCandyTestModel(PlatformOpenAI, group.CandyTestModel)
	if err != nil {
		return time.Time{}
	}
	if models == "" {
		models = defaultCandyTestModel
	}
	last := snapshot.Schedules[group.ID]
	// Legacy single-model schedules have no candidate setting.
	lastModels := firstNonEmptyString(last.ModelCandidates, last.Model)
	// Old schedules predate configurable effort and were checked with medium.
	effort, err := normalizeCandyTestReasoningEffort(group.Platform, group.CandyTestReasoningEffort)
	lastEffort, _ := normalizeCandyTestReasoningEffort(group.Platform, last.ReasoningEffort)
	if err != nil || lastModels != models || lastEffort != effort {
		return time.Time{}
	}
	return last.CheckedAt.Add(time.Duration(group.CandyTestIntervalSeconds) * time.Second)
}

func (r *CandyTestRunner) scan() {
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	defer cancel()
	var groups []Group
	var err error
	if repo, ok := r.groups.(candyTestGroupLister); ok {
		groups, err = repo.ListCandyTestGroups(ctx)
	} else {
		groups, err = r.groups.ListActiveByPlatform(ctx, PlatformOpenAI)
	}
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("[CandyTest] list groups failed", "error", err)
		}
		return
	}
	now := time.Now()
	jobs := make([]candyTestJob, 0)
	activeGroups := make(map[int64]bool)
	for i := range groups {
		group := &groups[i]
		if group.Platform != PlatformOpenAI || !group.IsActive() || group.CandyTestIntervalSeconds <= 0 {
			continue
		}
		activeGroups[group.ID] = true
		var accounts []Account
		var err error
		if repo, ok := r.accounts.(candyTestAccountLister); ok {
			accounts, err = repo.ListCandyTestAccounts(ctx, group.ID)
		} else {
			accounts, err = r.accounts.ListByGroup(ctx, group.ID)
		}
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("[CandyTest] list accounts failed", "group_id", group.ID, "error", err)
			}
			continue
		}
		for i := range accounts {
			account := &accounts[i]
			if !candyAutomaticAccountEligible(account, now) {
				continue
			}
			snapshot, err := readCandyTestSnapshot(account.Extra)
			if err != nil {
				continue
			}
			dueAt := candyScheduleDueAt(snapshot, group)
			if !dueAt.After(now) {
				jobs = append(jobs, candyTestJob{account.ID, group.ID, group.CandyTestIntervalSeconds, dueAt})
			}
		}
	}
	// Oldest due first prevents a short-interval group starving other groups or accounts.
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].dueAt.Before(jobs[j].dueAt) })
	r.mu.Lock()
	for key, until := range r.retryAfter {
		if !activeGroups[key[1]] || !until.After(now) {
			delete(r.retryAfter, key)
		}
	}
	r.mu.Unlock()
	for _, job := range jobs {
		if r.ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		key := [2]int64{job.accountID, job.groupID}
		if r.running[job.accountID] || r.retryAfter[key].After(now) {
			r.mu.Unlock()
			continue
		}
		select {
		case r.slots <- struct{}{}:
			r.running[job.accountID] = true
			r.wg.Add(1)
			r.mu.Unlock()
			go r.run(job)
		default:
			r.mu.Unlock()
			return // No unbounded goroutine queue; retry due work next tick.
		}
	}
}

func (r *CandyTestRunner) run(job candyTestJob) {
	defer r.wg.Done()
	defer func() {
		r.mu.Lock()
		delete(r.running, job.accountID)
		r.mu.Unlock()
		<-r.slots
	}()
	ctx, cancel := context.WithTimeout(r.ctx, 100*time.Second)
	defer cancel()
	_, err := r.tester.RunScheduledCandyTest(ctx, job.accountID, job.groupID)
	if err != nil && ctx.Err() == nil && !infraerrors.IsConflict(err) {
		// Infrastructure/persistence failures must not cause quota-consuming retries every second.
		delay := time.Duration(job.interval) * time.Second
		if delay < 30*time.Second {
			delay = 30 * time.Second
		}
		r.mu.Lock()
		r.retryAfter[[2]int64{job.accountID, job.groupID}] = time.Now().Add(delay)
		r.mu.Unlock()
		slog.Warn("[CandyTest] scheduled check failed", "account_id", job.accountID, "group_id", job.groupID, "error", err)
	}
}
