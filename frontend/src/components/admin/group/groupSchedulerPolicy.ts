import type {
  OpenAISchedulerConfig,
  OpenAISchedulerProfile,
} from '@/types'

export const OPENAI_SCHEDULER_PROFILES: OpenAISchedulerProfile[] = [
  'inherit',
  'sla',
  'balanced',
  'cost',
  'custom',
]

export function createDefaultOpenAISchedulerConfig(): OpenAISchedulerConfig {
  return {
    top_k: null,
    priority: null,
    load: null,
    queue: null,
    error_rate: null,
    ttft: null,
    reset: null,
    quota_headroom: null,
    upstream_cost: null,
    previous_response: null,
    session_sticky: null,
    sticky_weighted_enabled: true,
    subscription_priority_enabled: false,
  }
}

export function normalizeOpenAISchedulerConfig(
  value?: Partial<OpenAISchedulerConfig> | null,
): OpenAISchedulerConfig {
  return {
    ...createDefaultOpenAISchedulerConfig(),
    ...(value ?? {}),
  }
}
