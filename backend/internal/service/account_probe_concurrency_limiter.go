package service

import (
	"context"
	"sync"
)

// accountProbeConcurrencyLimiter bounds background probes independently for
// each upstream account. refs includes holders and waiters, allowing idle
// account entries to be removed without racing a queued acquisition.
type accountProbeConcurrencyLimiter struct {
	mu       sync.Mutex
	limit    int
	accounts map[int64]*accountProbeSlots
}

type accountProbeSlots struct {
	semaphore chan struct{}
	refs      int
}

func newAccountProbeConcurrencyLimiter(limit int) *accountProbeConcurrencyLimiter {
	if limit < 1 {
		limit = 1
	}
	return &accountProbeConcurrencyLimiter{
		limit:    limit,
		accounts: make(map[int64]*accountProbeSlots),
	}
}

func (l *accountProbeConcurrencyLimiter) acquire(ctx context.Context, accountID int64) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	l.mu.Lock()
	slots := l.accounts[accountID]
	if slots == nil {
		slots = &accountProbeSlots{semaphore: make(chan struct{}, l.limit)}
		l.accounts[accountID] = slots
	}
	slots.refs++
	l.mu.Unlock()

	select {
	case slots.semaphore <- struct{}{}:
		var once sync.Once
		return func() {
			once.Do(func() {
				<-slots.semaphore
				l.releaseReference(accountID, slots)
			})
		}, nil
	case <-ctx.Done():
		l.releaseReference(accountID, slots)
		return nil, ctx.Err()
	}
}

func (l *accountProbeConcurrencyLimiter) releaseReference(accountID int64, slots *accountProbeSlots) {
	l.mu.Lock()
	defer l.mu.Unlock()

	slots.refs--
	if slots.refs == 0 && l.accounts[accountID] == slots {
		delete(l.accounts, accountID)
	}
}
