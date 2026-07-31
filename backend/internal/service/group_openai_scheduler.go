package service

import (
	"errors"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	GroupOpenAISchedulerProfileInherit  = domain.GroupOpenAISchedulerProfileInherit
	GroupOpenAISchedulerProfileSLA      = domain.GroupOpenAISchedulerProfileSLA
	GroupOpenAISchedulerProfileBalanced = domain.GroupOpenAISchedulerProfileBalanced
	GroupOpenAISchedulerProfileCost     = domain.GroupOpenAISchedulerProfileCost
	GroupOpenAISchedulerProfileCustom   = domain.GroupOpenAISchedulerProfileCustom
)

func DefaultGroupOpenAISchedulerConfig() GroupOpenAISchedulerConfig {
	return GroupOpenAISchedulerConfig{
		StickyWeightedEnabled:       true,
		SubscriptionPriorityEnabled: false,
	}
}

type resolvedGroupOpenAISchedulerConfig struct {
	TopK                        int
	Priority                    float64
	Load                        float64
	Queue                       float64
	ErrorRate                   float64
	TTFT                        float64
	Reset                       float64
	QuotaHeadroom               float64
	UpstreamCost                float64
	PreviousResponse            float64
	SessionSticky               float64
	StickyWeightedEnabled       bool
	SubscriptionPriorityEnabled bool
}

func NormalizeGroupOpenAISchedulerProfile(profile string) string {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile == "" {
		return GroupOpenAISchedulerProfileInherit
	}
	return profile
}

func ValidateGroupOpenAISchedulerPolicy(profile string, custom GroupOpenAISchedulerConfig) error {
	switch NormalizeGroupOpenAISchedulerProfile(profile) {
	case GroupOpenAISchedulerProfileInherit,
		GroupOpenAISchedulerProfileSLA,
		GroupOpenAISchedulerProfileBalanced,
		GroupOpenAISchedulerProfileCost:
		return nil
	case GroupOpenAISchedulerProfileCustom:
		return validateGroupOpenAISchedulerConfig(custom)
	default:
		return errors.New("openai_scheduler_profile must be one of inherit, sla, balanced, cost, custom")
	}
}

func validateGroupOpenAISchedulerConfig(config GroupOpenAISchedulerConfig) error {
	if config.TopK != nil && *config.TopK <= 0 {
		return errors.New("openai_scheduler_config.top_k must be > 0")
	}
	weights := []*float64{
		config.Priority,
		config.Load,
		config.Queue,
		config.ErrorRate,
		config.TTFT,
		config.Reset,
		config.QuotaHeadroom,
		config.UpstreamCost,
		config.PreviousResponse,
		config.SessionSticky,
	}
	for _, weight := range weights {
		if weight != nil && (*weight < 0 || math.IsNaN(*weight) || math.IsInf(*weight, 0)) {
			return errors.New("openai_scheduler_config weights must be finite numbers >= 0")
		}
	}

	baseWeights := []*float64{
		config.Priority,
		config.Load,
		config.Queue,
		config.ErrorRate,
		config.TTFT,
		config.Reset,
		config.QuotaHeadroom,
		config.UpstreamCost,
	}
	baseSum := 0.0
	for _, weight := range baseWeights {
		if weight == nil {
			return nil
		}
		baseSum += *weight
	}
	if baseSum <= 0 {
		return errors.New("openai_scheduler_config base weights must not all be zero")
	}
	return nil
}

func resolveGroupOpenAISchedulerPreset(profile string) (resolvedGroupOpenAISchedulerConfig, bool) {
	switch NormalizeGroupOpenAISchedulerProfile(profile) {
	case GroupOpenAISchedulerProfileSLA:
		return resolvedGroupOpenAISchedulerConfig{
			TopK:                        2,
			Priority:                    0.5,
			Load:                        1.5,
			Queue:                       1.5,
			ErrorRate:                   2,
			TTFT:                        2.5,
			Reset:                       0,
			QuotaHeadroom:               0.5,
			UpstreamCost:                0,
			PreviousResponse:            1.5,
			SessionSticky:               0.75,
			StickyWeightedEnabled:       true,
			SubscriptionPriorityEnabled: false,
		}, true
	case GroupOpenAISchedulerProfileBalanced:
		return resolvedGroupOpenAISchedulerConfig{
			TopK:                        3,
			Priority:                    1,
			Load:                        1,
			Queue:                       0.8,
			ErrorRate:                   1,
			TTFT:                        1,
			Reset:                       0.3,
			QuotaHeadroom:               0.7,
			UpstreamCost:                1.5,
			PreviousResponse:            1,
			SessionSticky:               0.5,
			StickyWeightedEnabled:       true,
			SubscriptionPriorityEnabled: false,
		}, true
	case GroupOpenAISchedulerProfileCost:
		return resolvedGroupOpenAISchedulerConfig{
			TopK:                        2,
			Priority:                    0.3,
			Load:                        0.7,
			Queue:                       0.5,
			ErrorRate:                   0.8,
			TTFT:                        0.3,
			Reset:                       0.5,
			QuotaHeadroom:               1,
			UpstreamCost:                8,
			PreviousResponse:            0.5,
			SessionSticky:               0.25,
			StickyWeightedEnabled:       true,
			SubscriptionPriorityEnabled: false,
		}, true
	}
	return resolvedGroupOpenAISchedulerConfig{}, false
}

func applyCustomGroupOpenAISchedulerConfig(
	base resolvedGroupOpenAISchedulerConfig,
	custom GroupOpenAISchedulerConfig,
) (resolvedGroupOpenAISchedulerConfig, bool) {
	if validateGroupOpenAISchedulerConfig(custom) != nil {
		return resolvedGroupOpenAISchedulerConfig{}, false
	}
	if custom.TopK != nil {
		base.TopK = *custom.TopK
	}
	if custom.Priority != nil {
		base.Priority = *custom.Priority
	}
	if custom.Load != nil {
		base.Load = *custom.Load
	}
	if custom.Queue != nil {
		base.Queue = *custom.Queue
	}
	if custom.ErrorRate != nil {
		base.ErrorRate = *custom.ErrorRate
	}
	if custom.TTFT != nil {
		base.TTFT = *custom.TTFT
	}
	if custom.Reset != nil {
		base.Reset = *custom.Reset
	}
	if custom.QuotaHeadroom != nil {
		base.QuotaHeadroom = *custom.QuotaHeadroom
	}
	if custom.UpstreamCost != nil {
		base.UpstreamCost = *custom.UpstreamCost
	}
	if custom.PreviousResponse != nil {
		base.PreviousResponse = *custom.PreviousResponse
	}
	if custom.SessionSticky != nil {
		base.SessionSticky = *custom.SessionSticky
	}
	base.StickyWeightedEnabled = custom.StickyWeightedEnabled
	base.SubscriptionPriorityEnabled = custom.SubscriptionPriorityEnabled

	baseSum := base.Priority + base.Load + base.Queue + base.ErrorRate +
		base.TTFT + base.Reset + base.QuotaHeadroom + base.UpstreamCost
	if base.TopK <= 0 || baseSum <= 0 || math.IsNaN(baseSum) || math.IsInf(baseSum, 0) {
		return resolvedGroupOpenAISchedulerConfig{}, false
	}
	return base, true
}
