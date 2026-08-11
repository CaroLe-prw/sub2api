package service

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func parseOpenAISchedulerObservabilityRetentionDays(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < MinOpenAISchedulerObservabilityRetentionDays || parsed > MaxOpenAISchedulerObservabilityRetentionDays {
		return DefaultOpenAISchedulerObservabilityRetentionDays
	}
	return parsed
}

const (
	DefaultOpenAISchedulerObservabilityRetentionDays = 7
	MinOpenAISchedulerObservabilityRetentionDays     = 1
	MaxOpenAISchedulerObservabilityRetentionDays     = 30
	schedulerObservabilityFlushInterval              = 2 * time.Second
	schedulerObservabilityFlushTimeout               = 5 * time.Second
	schedulerObservabilityStateMax                   = 10000
)

type OpenAISchedulerObservabilityMetricDelta struct {
	BucketStart            time.Time
	GroupID                int64
	GroupName              string
	Requests               int64
	StickyDetectedRequests int64
	StickyRequests         int64
	SwitchedRequests       int64
	Switches               int64
	FailedRequests         int64
	CacheReadTokens        int64
	CacheEligibleTokens    int64
}

type OpenAISchedulerObservabilityTraceMutation struct {
	Trace    OpenAISchedulerObservabilityTrace
	Abnormal bool
}

type OpenAISchedulerObservabilityPersistentBatch struct {
	Metrics []OpenAISchedulerObservabilityMetricDelta
	Traces  []OpenAISchedulerObservabilityTraceMutation
}

type OpenAISchedulerObservabilityPersistentData struct {
	Metrics OpenAISchedulerObservabilityMetrics
	Groups  []OpenAISchedulerObservabilityGroup
	Traces  []OpenAISchedulerObservabilityTrace
}

type OpenAISchedulerObservabilityRepository interface {
	ApplyOpenAISchedulerObservabilityBatch(context.Context, OpenAISchedulerObservabilityPersistentBatch) error
	LoadOpenAISchedulerObservability(context.Context, time.Time, *int64) (OpenAISchedulerObservabilityPersistentData, error)
	DeleteOpenAISchedulerObservabilityBefore(context.Context, time.Time) error
}

type schedulerObservabilityContribution struct {
	bucketStart            time.Time
	groupID                int64
	groupName              string
	requests               int64
	stickyDetectedRequests int64
	stickyRequests         int64
	switchedRequests       int64
	switches               int64
	failedRequests         int64
	cacheReadTokens        int64
	cacheEligibleTokens    int64
	updatedAt              time.Time
}

type OpenAISchedulerObservabilityPersistence struct {
	repo OpenAISchedulerObservabilityRepository

	mu        sync.Mutex
	flushMu   sync.Mutex
	pending   map[string]OpenAISchedulerObservabilityTrace
	persisted map[string]schedulerObservabilityContribution
	wake      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	enabled   atomic.Bool
	retention atomic.Int64
	stopOnce  sync.Once
}

func NewOpenAISchedulerObservabilityPersistence(repo OpenAISchedulerObservabilityRepository) *OpenAISchedulerObservabilityPersistence {
	ctx, cancel := context.WithCancel(context.Background())
	p := &OpenAISchedulerObservabilityPersistence{
		repo: repo, pending: make(map[string]OpenAISchedulerObservabilityTrace),
		persisted: make(map[string]schedulerObservabilityContribution), wake: make(chan struct{}, 1),
		ctx: ctx, cancel: cancel,
	}
	p.enabled.Store(true)
	p.retention.Store(DefaultOpenAISchedulerObservabilityRetentionDays)
	if repo != nil {
		p.wg.Add(1)
		go p.run()
	}
	return p
}

func (p *OpenAISchedulerObservabilityPersistence) Configure(enabled bool, retentionDays int) {
	if p == nil {
		return
	}
	if retentionDays < MinOpenAISchedulerObservabilityRetentionDays || retentionDays > MaxOpenAISchedulerObservabilityRetentionDays {
		retentionDays = DefaultOpenAISchedulerObservabilityRetentionDays
	}
	p.enabled.Store(enabled)
	p.retention.Store(int64(retentionDays))
	if !enabled {
		p.flushMu.Lock()
		p.mu.Lock()
		p.pending = make(map[string]OpenAISchedulerObservabilityTrace)
		p.persisted = make(map[string]schedulerObservabilityContribution)
		p.mu.Unlock()
		p.flushMu.Unlock()
	}
}

func (p *OpenAISchedulerObservabilityPersistence) Enqueue(trace OpenAISchedulerObservabilityTrace) {
	if p == nil || p.repo == nil || !p.enabled.Load() || trace.RequestID == "" {
		return
	}
	p.mu.Lock()
	if _, exists := p.pending[trace.RequestID]; !exists && len(p.pending) >= schedulerObservabilityStateMax {
		for requestID := range p.pending {
			delete(p.pending, requestID)
			break
		}
	}
	p.pending[trace.RequestID] = trace
	p.mu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *OpenAISchedulerObservabilityPersistence) Load(ctx context.Context, cutoff time.Time, groupID *int64) (OpenAISchedulerObservabilityPersistentData, bool) {
	if p == nil || p.repo == nil || !p.enabled.Load() {
		return OpenAISchedulerObservabilityPersistentData{}, false
	}
	// The admin read path can afford to drain the tiny pending batch so displayed
	// aggregates include events from the last background flush interval.
	p.flush()
	data, err := p.repo.LoadOpenAISchedulerObservability(ctx, cutoff, groupID)
	if err != nil {
		log.Printf("[SchedulerObservability] load persisted data failed: %v", err)
		return OpenAISchedulerObservabilityPersistentData{}, false
	}
	return data, true
}

func (p *OpenAISchedulerObservabilityPersistence) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		p.cancel()
		p.wg.Wait()
		p.flush()
	})
}

func (p *OpenAISchedulerObservabilityPersistence) run() {
	defer p.wg.Done()
	flushTicker := time.NewTicker(schedulerObservabilityFlushInterval)
	cleanupTicker := time.NewTicker(time.Hour)
	defer flushTicker.Stop()
	defer cleanupTicker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.wake:
			// Coalesce rapid retry/failover mutations until the regular batch tick.
		case <-flushTicker.C:
			p.flush()
		case <-cleanupTicker.C:
			p.cleanup()
		}
	}
}

func (p *OpenAISchedulerObservabilityPersistence) flush() {
	if p == nil || p.repo == nil || !p.enabled.Load() {
		return
	}
	p.flushMu.Lock()
	defer p.flushMu.Unlock()
	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return
	}
	items := p.pending
	p.pending = make(map[string]OpenAISchedulerObservabilityTrace)
	p.mu.Unlock()

	batch := OpenAISchedulerObservabilityPersistentBatch{
		Metrics: make([]OpenAISchedulerObservabilityMetricDelta, 0, len(items)),
		Traces:  make([]OpenAISchedulerObservabilityTraceMutation, 0, len(items)),
	}
	contributions := make(map[string]schedulerObservabilityContribution, len(items))
	for requestID, trace := range items {
		current := schedulerObservabilityContributionFromTrace(trace)
		previous := p.persisted[requestID]
		batch.Metrics = append(batch.Metrics, schedulerObservabilityContributionDelta(current, previous))
		batch.Traces = append(batch.Traces, OpenAISchedulerObservabilityTraceMutation{
			Trace: trace, Abnormal: trace.SwitchCount > 0 || trace.Status == "failed",
		})
		contributions[requestID] = current
	}
	ctx, cancel := context.WithTimeout(context.Background(), schedulerObservabilityFlushTimeout)
	err := p.repo.ApplyOpenAISchedulerObservabilityBatch(ctx, batch)
	cancel()
	if err != nil {
		log.Printf("[SchedulerObservability] persist batch failed: %v", err)
		p.mu.Lock()
		for requestID, trace := range items {
			if _, newer := p.pending[requestID]; !newer {
				p.pending[requestID] = trace
			}
		}
		p.mu.Unlock()
		return
	}
	for requestID, contribution := range contributions {
		p.persisted[requestID] = contribution
	}
	for len(p.persisted) > schedulerObservabilityStateMax {
		var oldestID string
		var oldestAt time.Time
		for requestID, contribution := range p.persisted {
			if oldestID == "" || contribution.updatedAt.Before(oldestAt) {
				oldestID, oldestAt = requestID, contribution.updatedAt
			}
		}
		delete(p.persisted, oldestID)
	}
}

func (p *OpenAISchedulerObservabilityPersistence) cleanup() {
	if p == nil || p.repo == nil {
		return
	}
	days := p.retention.Load()
	if days <= 0 {
		days = DefaultOpenAISchedulerObservabilityRetentionDays
	}
	ctx, cancel := context.WithTimeout(context.Background(), schedulerObservabilityFlushTimeout)
	err := p.repo.DeleteOpenAISchedulerObservabilityBefore(ctx, time.Now().Add(-time.Duration(days)*24*time.Hour))
	cancel()
	if err != nil {
		log.Printf("[SchedulerObservability] cleanup failed: %v", err)
	}
}

func schedulerObservabilityContributionFromTrace(trace OpenAISchedulerObservabilityTrace) schedulerObservabilityContribution {
	createdAt := parseSchedulerTraceTime(trace.CreatedAt).UTC()
	updatedAt := trace.updatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	return schedulerObservabilityContribution{
		bucketStart: createdAt.Truncate(time.Minute), groupID: trace.GroupID, groupName: trace.GroupName,
		requests: 1, stickyDetectedRequests: boolInt64(trace.StickyDetected), stickyRequests: boolInt64(trace.StickyHit), switchedRequests: boolInt64(trace.SwitchCount > 0),
		switches: int64(trace.SwitchCount), failedRequests: boolInt64(trace.Status == "failed"),
		cacheReadTokens: trace.CacheReadTokens, cacheEligibleTokens: trace.CacheEligibleTokens,
		updatedAt: updatedAt,
	}
}

func schedulerObservabilityContributionDelta(current, previous schedulerObservabilityContribution) OpenAISchedulerObservabilityMetricDelta {
	return OpenAISchedulerObservabilityMetricDelta{
		BucketStart: current.bucketStart, GroupID: current.groupID, GroupName: current.groupName,
		Requests:               current.requests - previous.requests,
		StickyDetectedRequests: current.stickyDetectedRequests - previous.stickyDetectedRequests,
		StickyRequests:         current.stickyRequests - previous.stickyRequests,
		SwitchedRequests:       current.switchedRequests - previous.switchedRequests, Switches: current.switches - previous.switches,
		FailedRequests:      current.failedRequests - previous.failedRequests,
		CacheReadTokens:     current.cacheReadTokens - previous.cacheReadTokens,
		CacheEligibleTokens: current.cacheEligibleTokens - previous.cacheEligibleTokens,
	}
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
