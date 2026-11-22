//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func automaticCandyAccount() *Account {
	account := candyAccount()
	account.Status = StatusActive
	account.Groups = []*Group{{ID: 7, Platform: PlatformOpenAI, Status: StatusActive, CandyTestIntervalSeconds: 60}}
	return account
}

func TestScheduledCandyTestPersistsDueTimeAndHonorsChanges(t *testing.T) {
	account := automaticCandyAccount()
	repo := &candyAccountRepo{account: account}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21"), candyResponse("21"), candyResponse("21")}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 1, result.Total)
	require.Equal(t, defaultCandyTestModel, result.Schedules[7].Model)
	require.False(t, result.Schedules[7].CheckedAt.IsZero())
	// A new service/process must respect the persisted clock, not start another check.
	restarted := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	result, err = restarted.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
	// Group model changes invalidate the previous model's clock.
	account.Groups[0].CandyTestModel = "gpt-6.1-sol"
	result, err = restarted.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 2, result.Total)
	require.Equal(t, "gpt-6.1-sol", result.Schedules[7].Model)
	// Interval changes take effect immediately using the saved completion time.
	snapshot, err := readCandyTestSnapshot(account.Extra)
	require.NoError(t, err)
	snapshot.Schedules[7] = CandyTestSchedule{Model: "gpt-6.1-sol", CheckedAt: time.Now().Add(-10 * time.Second)}
	account.Extra[CandyTestExtraKey] = snapshot
	account.Groups[0].CandyTestIntervalSeconds = 1
	result, err = restarted.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 3, result.Total)
}

func TestScheduledCandyTestRechecksEligibilityAndDisabledGroups(t *testing.T) {
	for _, scenario := range []string{"disabled", "inactive_group", "removed", "inactive_account", "expired", "shadow", "synthetic"} {
		t.Run(scenario, func(t *testing.T) {
			account := automaticCandyAccount()
			switch scenario {
			case "disabled":
				account.Groups[0].CandyTestIntervalSeconds = 0
			case "inactive_group":
				account.Groups[0].Status = "inactive"
			case "removed":
				account.Groups = nil
			case "inactive_account":
				account.Status = "inactive"
			case "expired":
				past := time.Now().Add(-time.Second)
				account.ExpiresAt = &past
			case "shadow":
				id := int64(1)
				account.ParentAccountID = &id
			case "synthetic":
				account.Extra["synthetic_ui_test"] = true
			}
			svc := &AccountTestService{accountRepo: &candyAccountRepo{account: account}}
			result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
			if scenario == "synthetic" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, result) // nil HTTP client would panic if this initiated an upstream check.
		})
	}
}

func TestScheduledCandyTestIndependentGroupsAndErrorIntervals(t *testing.T) {
	account := automaticCandyAccount()
	account.Groups = append(account.Groups, &Group{ID: 8, Platform: PlatformOpenAI, Status: StatusActive, CandyTestModel: "gpt-6.1-sol", CandyTestIntervalSeconds: 120})
	repo := &candyAccountRepo{account: account}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("34"), newJSONResponse(500, "error")}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	first, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Equal(t, 1, first.Failed)
	second, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 8)
	require.NoError(t, err)
	require.Equal(t, 1, second.Errors)
	require.Len(t, second.Schedules, 2)
	for _, groupID := range []int64{7, 8} {
		skipped, err := svc.RunScheduledCandyTest(context.Background(), account.ID, groupID)
		require.NoError(t, err)
		require.Nil(t, skipped)
	}
	require.Len(t, upstream.requests, 2)
	require.True(t, account.Schedulable)
}

func TestCandyTestManualSharesDistributedLockAndResetsGroupClock(t *testing.T) {
	account := automaticCandyAccount()
	repo := &candyAccountRepo{account: account}
	upstream := &queuedHTTPUpstream{responses: []*http.Response{candyResponse("21")}}
	cache := &fakeLeaderLockCache{}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	svc.SetCandyTestLeaderLock(cache, nil)
	_, err := cache.TryAcquireLeaderLock(context.Background(), "candy-test:account:42", "peer", 2*time.Minute)
	require.NoError(t, err)
	_, err = svc.RunCandyTest(context.Background(), account.ID, 7)
	require.Error(t, err)
	require.Empty(t, upstream.requests)
	require.NoError(t, cache.ReleaseLeaderLock(context.Background(), "candy-test:account:42", "peer"))
	_, err = svc.RunCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	result, err := svc.RunScheduledCandyTest(context.Background(), account.ID, 7)
	require.NoError(t, err)
	require.Nil(t, result)
	require.Len(t, upstream.requests, 1)
	require.Empty(t, cache.heldBy("candy-test:account:42"))
}

type candyRunnerGroups struct {
	GroupRepository
	groups []Group
}

func (r *candyRunnerGroups) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	return r.groups, nil
}

type candyRunnerAccounts struct {
	AccountRepository
	accounts []Account
	scans    int
}

func (r *candyRunnerAccounts) ListByGroup(context.Context, int64) ([]Account, error) {
	r.scans++
	return r.accounts, nil
}

type candyRunnerTester struct {
	mu      sync.Mutex
	calls   []candyTestJob
	started chan struct{}
	wait    bool
	err     error
}

func (t *candyRunnerTester) RunScheduledCandyTest(ctx context.Context, account, group int64) (*CandyTestSnapshot, error) {
	t.mu.Lock()
	t.calls = append(t.calls, candyTestJob{accountID: account, groupID: group})
	t.mu.Unlock()
	if t.started != nil {
		t.started <- struct{}{}
	}
	if t.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, t.err
}
func (t *candyRunnerTester) count() int { t.mu.Lock(); defer t.mu.Unlock(); return len(t.calls) }

func TestCandyRunnerLimitsWorkersAndStopsInFlightChecks(t *testing.T) {
	group := *automaticCandyAccount().Groups[0]
	groups := &candyRunnerGroups{groups: []Group{group}}
	accounts := &candyRunnerAccounts{}
	for i := 0; i < 10; i++ {
		a := *automaticCandyAccount()
		a.ID = int64(i + 1)
		accounts.accounts = append(accounts.accounts, a)
	}
	tester := &candyRunnerTester{started: make(chan struct{}, 10), wait: true}
	runner := NewCandyTestRunner(groups, accounts, nil)
	runner.tester = tester
	runner.scan()
	for i := 0; i < candyTestMaxWorkers; i++ {
		select {
		case <-tester.started:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
	}
	runner.scan()
	require.Equal(t, candyTestMaxWorkers, tester.count())
	runner.Stop()
	runner.Stop()
	require.Empty(t, runner.running)
	require.Empty(t, runner.slots)
}

func TestCandyRunnerFiltersDisabledUnsupportedAndNotDueAccounts(t *testing.T) {
	group := *automaticCandyAccount().Groups[0]
	groups := &candyRunnerGroups{groups: []Group{{ID: 8, Platform: PlatformOpenAI, Status: StatusActive}, {ID: 9, Platform: PlatformAnthropic, Status: StatusActive, CandyTestIntervalSeconds: 1}, group}}
	accounts := &candyRunnerAccounts{}
	for _, kind := range []string{"apikey", "anthropic", "inactive", "shadow", "expired", "not_due", "eligible"} {
		a := *automaticCandyAccount()
		a.ID = int64(len(accounts.accounts) + 1)
		switch kind {
		case "apikey":
			a.Type = AccountTypeAPIKey
		case "anthropic":
			a.Platform = PlatformAnthropic
		case "inactive":
			a.Status = "inactive"
		case "shadow":
			id := int64(30)
			a.ParentAccountID = &id
		case "expired":
			past := time.Now().Add(-time.Second)
			a.ExpiresAt = &past
		case "not_due":
			a.Extra[CandyTestExtraKey] = CandyTestSnapshot{Schedules: map[int64]CandyTestSchedule{7: {Model: defaultCandyTestModel, CheckedAt: time.Now()}}}
		}
		accounts.accounts = append(accounts.accounts, a)
	}
	tester := &candyRunnerTester{}
	runner := NewCandyTestRunner(groups, accounts, nil)
	runner.tester = tester
	runner.scan()
	runner.wg.Wait()
	require.Equal(t, 1, accounts.scans)
	require.Equal(t, 2, tester.count())
	require.ElementsMatch(t, []int64{1, 7}, []int64{tester.calls[0].accountID, tester.calls[1].accountID})
	runner.Stop()
}

func TestCandyRunnerPersistenceFailureBackoff(t *testing.T) {
	account := automaticCandyAccount()
	groups := &candyRunnerGroups{groups: []Group{*account.Groups[0]}}
	accounts := &candyRunnerAccounts{accounts: []Account{*account}}
	tester := &candyRunnerTester{err: errors.New("save failed")}
	runner := NewCandyTestRunner(groups, accounts, nil)
	runner.tester = tester
	runner.scan()
	runner.wg.Wait()
	runner.scan()
	runner.wg.Wait()
	require.Equal(t, 1, tester.count())
	runner.Stop()
}

func TestCandyScheduleDueBoundary(t *testing.T) {
	group := &Group{ID: 7, CandyTestIntervalSeconds: 60}
	now := time.Now()
	snapshot := CandyTestSnapshot{Schedules: map[int64]CandyTestSchedule{7: {Model: defaultCandyTestModel, CheckedAt: now}}}
	due := candyScheduleDueAt(snapshot, group)
	require.Equal(t, now.Add(time.Minute), due)
	require.True(t, due.After(now.Add(59*time.Second)))
	require.False(t, due.After(now.Add(time.Minute)))
}
