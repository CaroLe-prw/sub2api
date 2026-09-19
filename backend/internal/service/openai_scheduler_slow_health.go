package service

import (
	"context"
	"sort"
	"sync"
	"time"
)

const (
	openAISlowFirstOutputSamples = 3
	openAISlowRecoveryInterval   = time.Minute
	openAISlowFirstOutputReason  = "consecutive_slow_first_output"
)

type openAIFirstOutputSample struct {
	ms int
	at time.Time
}

// This signal is model scoped and uses individual real requests, not the EWMA
// or synthetic probes. A mutex keeps concurrent completions/history hydration
// from manufacturing a streak or overwriting a newer recovery.
type openAISlowFirstOutputHealth struct {
	mu          sync.Mutex
	samples     [openAISlowFirstOutputSamples]openAIFirstOutputSample
	count       int
	updatedAt   time.Time
	nextProbeAt time.Time
}

func (h *openAISlowFirstOutputHealth) record(ms int, at time.Time) {
	if ms <= 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if at.Before(h.updatedAt) {
		return
	}
	copy(h.samples[:], h.samples[1:])
	h.samples[len(h.samples)-1] = openAIFirstOutputSample{ms: ms, at: at}
	if h.count < len(h.samples) {
		h.count++
	}
	h.updatedAt = at
	h.nextProbeAt = at.Add(openAISlowRecoveryInterval)
}

func (h *openAISlowFirstOutputHealth) restore(samples []openAIFirstOutputSample) {
	if len(samples) == 0 {
		return
	}
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].at.Before(samples[j].at) })
	latest := samples[len(samples)-1].at
	h.mu.Lock()
	defer h.mu.Unlock()
	if !latest.After(h.updatedAt) {
		return
	}
	if len(samples) > len(h.samples) {
		samples = samples[len(samples)-len(h.samples):]
	}
	h.samples = [openAISlowFirstOutputSamples]openAIFirstOutputSample{}
	copy(h.samples[len(h.samples)-len(samples):], samples)
	h.count = len(samples)
	h.updatedAt = latest
	if next := latest.Add(openAISlowRecoveryInterval); next.After(h.nextProbeAt) {
		h.nextProbeAt = next
	}
}

func (h *openAISlowFirstOutputHealth) slowLocked(threshold time.Duration, now time.Time) bool {
	if threshold <= 0 || h.count < len(h.samples) {
		return false
	}
	for _, sample := range h.samples {
		if sample.ms <= 0 || time.Duration(sample.ms)*time.Millisecond <= threshold || now.Sub(sample.at) > openAISchedulerHealthHistoryWindow {
			return false
		}
	}
	return true
}

func (h *openAISlowFirstOutputHealth) status(threshold time.Duration, now time.Time) (slow, probeDue bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	slow = h.slowLocked(threshold, now)
	return slow, slow && !now.Before(h.nextProbeAt)
}

func (h *openAISlowFirstOutputHealth) claimProbe(threshold time.Duration, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.slowLocked(threshold, now) {
		return true
	}
	if now.Before(h.nextProbeAt) {
		return false
	}
	h.nextProbeAt = now.Add(openAISlowRecoveryInterval)
	return true
}

func (h *openAISlowFirstOutputHealth) recover(ms int, threshold time.Duration, now time.Time) {
	if ms <= 0 || threshold <= 0 || time.Duration(ms)*time.Millisecond > threshold {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Before(h.updatedAt) {
		return
	}
	h.count = 0
	h.samples = [openAISlowFirstOutputSamples]openAIFirstOutputSample{}
	h.updatedAt = now // Older persisted calls must not undo this successful probe.
}

func (s *OpenAIGatewayService) openAISlowAccountThreshold() time.Duration {
	if s == nil || s.cfg == nil {
		return 15 * time.Second
	}
	return s.openAIFirstOutputSlowThreshold("")
}

func (s *defaultOpenAIAccountScheduler) slowAccountHealth(account *Account, req OpenAIAccountScheduleRequest) *openAISlowFirstOutputHealth {
	if s == nil || s.stats == nil || account == nil || account.Platform != PlatformOpenAI {
		return nil
	}
	// Use exactly the model to which ReportOpenAIAccountScheduleResult attributes
	// the call. Do not let another model's latency poison this model's routing.
	model := ResolveOpenAIAccountUpstreamModelForRequest(account, req.RequestedModel, req.RequireCompact)
	key, ok := openAIAccountModelTransientKey(account.ID, model)
	if !ok {
		return nil
	}
	value, ok := s.stats.models.Load(key)
	if !ok {
		return nil
	}
	stat, _ := value.(*openAIAccountRuntimeStat)
	if stat == nil {
		return nil
	}
	return &stat.slowFirstOutput
}

func (s *defaultOpenAIAccountScheduler) slowAccountStatus(account *Account, req OpenAIAccountScheduleRequest) (bool, bool) {
	h := s.slowAccountHealth(account, req)
	if h == nil {
		return false, false
	}
	return h.status(s.service.openAISlowAccountThreshold(), time.Now())
}

// Persisted usage events already have the same real-user and model attribution
// filters as scheduler health aggregates. Hydration is periodic, never a query
// per scheduling decision. Only successful measured calls count toward a streak.
type openAISchedulerTrafficEventsRepository interface {
	GetSchedulerFirstOutputEvents(context.Context, time.Time) ([]ChannelMonitorUserTrafficEvent, error)
}

func (s *openAIAccountRuntimeStats) restoreSlowFirstOutputHistory(events []ChannelMonitorUserTrafficEvent) {
	byModel := make(map[openAIAccountModelKey][]openAIFirstOutputSample)
	for _, event := range events {
		if event.Status != "success" || event.TTFTMs == nil || *event.TTFTMs <= 0 {
			continue
		}
		key, ok := openAIAccountModelTransientKey(event.AccountID, event.Model)
		if !ok {
			continue
		}
		byModel[key] = append(byModel[key], openAIFirstOutputSample{ms: int(*event.TTFTMs), at: event.CreatedAt})
	}
	for key, samples := range byModel {
		if stat := s.loadOrCreateModel(key.AccountID, key.Model); stat != nil {
			stat.slowFirstOutput.restore(samples)
		}
	}
}
