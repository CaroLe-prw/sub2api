package service

import (
	"math"
	"sync"
	"time"
)

// SchedulerTTFTHalfLife is shared with the persisted real-traffic aggregate.
// These signals affect placement only; health gates and binding escape rules
// continue to use their existing independent evidence.
const SchedulerTTFTHalfLife = 5 * time.Minute

const schedulerTTFTBucketCount = 30

type schedulerTTFTBucket struct {
	minute           int64
	samples          int64
	mean             float64
	effectiveSamples float64
	lastSampleAt     time.Time
}

// Minute buckets bound memory and sample counts even under sustained traffic.
// Within each bucket the decayed sums are exact. The oldest partial minute is
// dropped conservatively instead of retaining samples outside the window.
type schedulerTTFTWindow struct {
	mu      sync.Mutex
	buckets [schedulerTTFTBucketCount]schedulerTTFTBucket
}

func schedulerTTFTDecay(age time.Duration) float64 {
	return math.Exp2(-float64(age) / float64(SchedulerTTFTHalfLife))
}

func (w *schedulerTTFTWindow) record(ms int, at time.Time) {
	if ms <= 0 || at.Unix() <= 0 {
		return
	}
	minute := at.Unix() / 60
	w.mu.Lock()
	defer w.mu.Unlock()
	b := &w.buckets[minute%schedulerTTFTBucketCount]
	if b.samples > 0 && minute < b.minute {
		return
	}
	weight := 1.0
	if b.minute != minute || b.samples == 0 {
		*b = schedulerTTFTBucket{minute: minute}
	} else if at.Before(b.lastSampleAt) {
		// Concurrent completions can acquire the lock in reverse timestamp
		// order. Include the older sample at the bucket's current reference.
		weight = schedulerTTFTDecay(b.lastSampleAt.Sub(at))
	} else {
		b.effectiveSamples *= schedulerTTFTDecay(at.Sub(b.lastSampleAt))
	}
	b.mean += (float64(ms) - b.mean) * weight / (b.effectiveSamples + weight)
	b.effectiveSamples += weight
	b.samples++
	if at.After(b.lastSampleAt) {
		b.lastSampleAt = at
	}
}

type schedulerTTFTEvidence struct {
	mean             float64
	samples          int64
	effectiveSamples float64
	lastSampleAt     time.Time
	observedAt       time.Time
	generation       uint64
}

type schedulerTTFTSnapshot struct {
	ms           float64
	samples      int64
	lastSampleAt time.Time
	confidence   float64
	source       string
}

func (w *schedulerTTFTWindow) snapshot(now time.Time) schedulerTTFTEvidence {
	return w.snapshotWithClock(func() time.Time { return now })
}

func (w *schedulerTTFTWindow) snapshotWithClock(clock func() time.Time) schedulerTTFTEvidence {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Read the clock after acquiring the lock so a concurrent completion
	// cannot push the whole current bucket into this snapshot's future.
	now := clock()
	result := schedulerTTFTEvidence{observedAt: now}
	var weightedSum float64
	for _, b := range w.buckets {
		if b.samples == 0 || now.Before(b.lastSampleAt) ||
			now.Sub(time.Unix(b.minute*60, 0)) >= openAISchedulerHealthHistoryWindow {
			continue
		}
		weight := b.effectiveSamples * schedulerTTFTDecay(now.Sub(b.lastSampleAt))
		weightedSum += b.mean * weight
		result.effectiveSamples += weight
		result.samples += b.samples
		if b.lastSampleAt.After(result.lastSampleAt) {
			result.lastSampleAt = b.lastSampleAt
		}
	}
	if result.effectiveSamples > 0 {
		result.mean = weightedSum / result.effectiveSamples
	}
	return result
}

func (e schedulerTTFTEvidence) snapshot(now time.Time, source string) schedulerTTFTSnapshot {
	if e.samples <= 0 || e.mean <= 0 || math.IsNaN(e.mean) || math.IsInf(e.mean, 0) ||
		e.effectiveSamples <= 0 || math.IsNaN(e.effectiveSamples) || math.IsInf(e.effectiveSamples, 0) ||
		e.lastSampleAt.IsZero() || e.observedAt.IsZero() || e.lastSampleAt.After(e.observedAt) ||
		now.Before(e.observedAt) || now.Sub(e.lastSampleAt) >= openAISchedulerHealthHistoryWindow {
		return schedulerTTFTSnapshot{}
	}
	effective := e.effectiveSamples * schedulerTTFTDecay(now.Sub(e.observedAt))
	// An idle high-volume account must lose confidence too, even if its old
	// effective sample count was very large.
	confidence := effective / (effective + 5) * schedulerTTFTDecay(now.Sub(e.lastSampleAt))
	if source == "probe" {
		confidence *= 0.25
	}
	return schedulerTTFTSnapshot{ms: e.mean, samples: e.samples, lastSampleAt: e.lastSampleAt, confidence: confidence, source: source}
}

func (s *openAIAccountRuntimeStats) ttftForRequest(accountID int64, model string, now time.Time) schedulerTTFTSnapshot {
	return s.ttftForRequestWithClock(accountID, model, func() time.Time { return now })
}

func (s *openAIAccountRuntimeStats) ttftForRequestNow(accountID int64, model string) schedulerTTFTSnapshot {
	return s.ttftForRequestWithClock(accountID, model, time.Now)
}

func (s *openAIAccountRuntimeStats) ttftForRequestWithClock(accountID int64, model string, clock func() time.Time) schedulerTTFTSnapshot {
	stat := s.modelStat(accountID, model)
	if stat == nil {
		return schedulerTTFTSnapshot{}
	}
	trafficEvidence := stat.trafficTTFT.snapshotWithClock(clock)
	now := clock()
	traffic := trafficEvidence.snapshot(now, "traffic")
	if history := stat.historyTTFT.Load(); history != nil && history.generation == s.historyGen.Load() {
		historical := history.snapshot(now, "traffic")
		// Live and persisted requests overlap. Use the newest measured source,
		// never add their counts or claim independent corroboration.
		if historical.lastSampleAt.After(traffic.lastSampleAt) {
			traffic = historical
		}
	}
	if traffic.source != "" {
		return traffic
	}
	probeEvidence := stat.probeTTFT.snapshotWithClock(clock)
	return probeEvidence.snapshot(clock(), "probe")
}

func (c *openAIAccountCandidateScore) setTTFT(evidence schedulerTTFTSnapshot) {
	c.ttftEvidence = evidence
	c.ttft = evidence.ms
	c.hasTTFT = evidence.source != ""
}

// Relative, bounded latency differences replace min/max stretching. A 1%
// speed difference remains small even in a two-account pool. Unknown evidence
// is neutral and low-confidence evidence stays close to neutral.
func openAITTFTFactors(candidates []openAIAccountCandidateScore) []float64 {
	factors := make([]float64, len(candidates))
	measured := 0
	var logSum, confidenceSum float64
	for i, candidate := range candidates {
		factors[i] = 0.5
		if !candidate.excluded && validOpenAICandidateTTFT(candidate) && candidate.ttftEvidence.confidence > 0 {
			confidence := clamp01(candidate.ttftEvidence.confidence)
			logSum += math.Log(candidate.ttft) * confidence
			confidenceSum += confidence
			measured++
		}
	}
	if measured < 2 || confidenceSum <= 0 {
		return factors
	}
	// Weight the reference by confidence too: a stale outlier must not shift
	// every other account's score while its own factor is nearly neutral.
	baseline := math.Exp(logSum / confidenceSum)
	for i, candidate := range candidates {
		if !validOpenAICandidateTTFT(candidate) {
			continue
		}
		relative := (baseline - candidate.ttft) / math.Max(baseline, candidate.ttft)
		factors[i] += 0.5 * clamp01(candidate.ttftEvidence.confidence) * relative
	}
	return factors
}

func (e schedulerTTFTSnapshot) observedMS() *float64 {
	if e.source == "" {
		return nil
	}
	return &e.ms
}

func (e schedulerTTFTSnapshot) observedAt() *time.Time {
	if e.source == "" {
		return nil
	}
	at := e.lastSampleAt.UTC()
	return &at
}
