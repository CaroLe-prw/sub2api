package service

import "time"

const (
	schedulerCacheMinimumSamples = 5
	schedulerCacheMinimumTokens  = 4096
	schedulerCacheNeutralFactor  = 0.5
)

// Cache evidence is hydrated from real usage alongside health, never from
// synthetic probes. Keeping one immutable snapshot avoids torn token counters.
type schedulerCacheEvidence struct {
	readTokens     int64
	eligibleTokens int64
	samples        int64
	lastSampleAt   time.Time
	refreshedAt    time.Time
	generation     uint64
}

// A cache advantage elsewhere must not itself move a healthy conversation.
// Preserve existing weighted-sticky policy; enable cache placement again when
// the bound account is absent, excluded, degraded or at capacity.
func schedulerCachePlacementWeight(weight float64, candidates []openAIAccountCandidateScore, stickyIDs ...int64) float64 {
	for _, candidate := range candidates {
		if candidate.account == nil || candidate.excluded || candidate.slowDeprioritized ||
			candidate.stickyTTFTFallback || candidate.errorRate > 0.5 ||
			(candidate.loadInfo != nil && candidate.loadInfo.LoadRate >= 100) {
			continue
		}
		for _, id := range stickyIDs {
			if id > 0 && candidate.account.ID == id {
				return 0
			}
		}
	}
	return weight
}

func (s *openAIAccountRuntimeStats) cacheHitRateForRequest(accountID int64, model string, now time.Time) (rate *float64, samples int64, factor float64) {
	factor = schedulerCacheNeutralFactor
	stat := s.modelStat(accountID, model)
	if stat == nil {
		return
	}
	evidence := stat.cacheEvidence.Load()
	if evidence == nil || evidence.generation != s.historyGen.Load() ||
		now.Before(evidence.refreshedAt) || now.Sub(evidence.refreshedAt) >= 2*openAISchedulerHealthRefreshInterval ||
		evidence.lastSampleAt.IsZero() || now.Before(evidence.lastSampleAt) || now.Sub(evidence.lastSampleAt) >= openAISchedulerHealthHistoryWindow {
		return
	}
	samples = evidence.samples
	if samples < schedulerCacheMinimumSamples || evidence.eligibleTokens < schedulerCacheMinimumTokens ||
		evidence.readTokens < 0 || evidence.readTokens > evidence.eligibleTokens {
		return
	}
	value := float64(evidence.readTokens) / float64(evidence.eligibleTokens)
	rate = &value
	// Shrink small samples toward neutral; a handful of cached calls must not
	// dominate placement. Unmeasured models never inherit another model's rate.
	confidence := float64(samples) / (float64(samples) + schedulerCacheMinimumSamples)
	factor += (value - schedulerCacheNeutralFactor) * confidence
	return
}

// CacheHitRate exposes the raw token-weighted rate for monitoring, without the
// scheduler's sample floor or smoothing. Zero hits and unknown usage differ.
func (s OpenAISchedulerHealthSnapshot) CacheHitRate() *float64 {
	if s.CacheEligibleTokens <= 0 || s.CacheReadTokens < 0 || s.CacheReadTokens > s.CacheEligibleTokens {
		return nil
	}
	rate := float64(s.CacheReadTokens) / float64(s.CacheEligibleTokens)
	return &rate
}
