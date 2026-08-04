package domain

const (
	GroupOpenAISchedulerProfileInherit  = "inherit"
	GroupOpenAISchedulerProfileSLA      = "sla"
	GroupOpenAISchedulerProfileBalanced = "balanced"
	GroupOpenAISchedulerProfileCost     = "cost"
	GroupOpenAISchedulerProfileCustom   = "custom"
)

// GroupOpenAISchedulerConfig stores a group's custom scheduler policy.
// The legacy OpenAI name is retained for database/API compatibility. Preset
// profiles ignore this value so switching profiles keeps custom tuning.
type GroupOpenAISchedulerConfig struct {
	TopK                        *int     `json:"top_k"`
	Priority                    *float64 `json:"priority"`
	Load                        *float64 `json:"load"`
	Queue                       *float64 `json:"queue"`
	ErrorRate                   *float64 `json:"error_rate"`
	TTFT                        *float64 `json:"ttft"`
	Reset                       *float64 `json:"reset"`
	QuotaHeadroom               *float64 `json:"quota_headroom"`
	UpstreamCost                *float64 `json:"upstream_cost"`
	PreviousResponse            *float64 `json:"previous_response"`
	SessionSticky               *float64 `json:"session_sticky"`
	StickyWeightedEnabled       bool     `json:"sticky_weighted_enabled"`
	SubscriptionPriorityEnabled bool     `json:"subscription_priority_enabled"`
}
