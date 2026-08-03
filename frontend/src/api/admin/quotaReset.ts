import { apiClient } from '../client'

export const QUOTA_RESET_RUN_TIMEOUT_MS = 31 * 60 * 1000

export type QuotaResetWindowStartMode = 'current' | 'natural_day' | 'preserve'

export interface QuotaResetPolicy {
  daily: boolean
  weekly: boolean
  monthly: boolean
  window_start_mode: QuotaResetWindowStartMode
}

export interface SubscriptionQuotaResetConfig extends QuotaResetPolicy {
  enabled: boolean
  interval_hours: number
  group_ids: number[]
}

export interface SubscriptionQuotaResetRunRequest extends QuotaResetPolicy {
  group_ids: number[]
  restart_schedule: boolean
}

export type SubscriptionQuotaResetStatus =
  | 'idle'
  | 'running'
  | 'success'
  | 'partial_failed'
  | 'failed'

export interface SubscriptionQuotaResetState {
  status: SubscriptionQuotaResetStatus
  last_started_at: string | null
  last_finished_at: string | null
  last_scheduled_finished_at: string | null
  last_success_at: string | null
  next_run_at: string | null
  matched_count: number
  reset_count: number
  skipped_count: number
  failed_count: number
  last_error: string
}

export interface SubscriptionQuotaResetResponse {
  config: SubscriptionQuotaResetConfig
  state: SubscriptionQuotaResetState
}

export async function get(): Promise<SubscriptionQuotaResetResponse> {
  const { data } = await apiClient.get<SubscriptionQuotaResetResponse>(
    '/admin/subscription-quota-reset'
  )
  return data
}

export async function update(
  config: SubscriptionQuotaResetConfig
): Promise<SubscriptionQuotaResetResponse> {
  const { data } = await apiClient.put<SubscriptionQuotaResetResponse>(
    '/admin/subscription-quota-reset',
    config
  )
  return data
}

export async function run(
  request: SubscriptionQuotaResetRunRequest
): Promise<SubscriptionQuotaResetResponse> {
  const { data } = await apiClient.post<SubscriptionQuotaResetResponse>(
    '/admin/subscription-quota-reset/run',
    request,
    { timeout: QUOTA_RESET_RUN_TIMEOUT_MS }
  )
  return data
}

export default { get, update, run }
