/**
 * Admin scheduler-probe API.
 *
 * These probes feed scheduler health scoring and intentionally do not depend
 * on the separately controlled channel-monitor/public-status feature.
 */

import { apiClient } from '../client'

export type SchedulerProbeProvider = 'openai' | 'anthropic' | 'gemini' | 'grok'
export type SchedulerProbeMode = 'fixed' | 'adaptive'

export interface PoolProbeHeartbeat {
  id: number
  plan_id: number
  status: 'success' | 'failed'
  ttft_ms: number | null
  latency_ms: number
  started_at: string
  finished_at: string
  created_at: string
}

export interface SchedulerUserTrafficSummary {
  window_minutes: number
  success_count: number
  failure_count: number
  avg_ttft_ms: number | null
  last_success_at: string | null
  last_failure_at: string | null
}

export interface PoolMonitorModel {
  plan_id: number
  model: string
  enabled: boolean
  status: '' | 'success' | 'failed'
  latency_ms: number | null
  availability: number | null
  sample_count: number
  failure_count: number
  last_checked_at: string | null
  recent_results?: PoolProbeHeartbeat[]
  user_traffic?: SchedulerUserTrafficSummary
}

export interface PoolMonitorAccount {
  account_id: number
  name: string
  platform: string
  type: string
  status: string
  schedulable: boolean
  concurrency: number
  models: PoolMonitorModel[]
}

export interface PoolMonitorOverviewResponse {
  items: PoolMonitorAccount[]
}

export interface PoolAccountModelPolicy {
  account_id: number
  whitelist: string[]
  discovered_models: string[]
  effective_models: string[]
}

export interface PoolProbeResult extends PoolProbeHeartbeat {
  response_text: string
  error_message: string
}

export interface SchedulerProbePolicy {
  enabled: boolean
  mode: SchedulerProbeMode
  fixed_interval_minutes: number
  whitelist: string[]
  discovered_by_provider: Partial<Record<SchedulerProbeProvider, string[]>>
  eligible_by_provider: Partial<Record<SchedulerProbeProvider, string[]>>
}

export async function getPolicy(): Promise<SchedulerProbePolicy> {
  const { data } = await apiClient.get<SchedulerProbePolicy>('/admin/scheduler-observability/probes/policy')
  return data
}

export async function updatePolicy(
  input: Pick<SchedulerProbePolicy, 'enabled' | 'whitelist'> & Partial<Pick<SchedulerProbePolicy, 'mode' | 'fixed_interval_minutes'>>
): Promise<SchedulerProbePolicy> {
  const { data } = await apiClient.put<SchedulerProbePolicy>('/admin/scheduler-observability/probes/policy', input)
  return data
}

export async function listOverview(accountIds: number[] = []): Promise<PoolMonitorOverviewResponse> {
  const url = '/admin/scheduler-observability/probes/overview'
  const { data } = accountIds.length
    ? await apiClient.get<PoolMonitorOverviewResponse>(url, {
      params: { account_ids: accountIds.join(',') },
    })
    : await apiClient.get<PoolMonitorOverviewResponse>(url)
  return data
}

export async function getAccountModelPolicy(accountId: number): Promise<PoolAccountModelPolicy> {
  const { data } = await apiClient.get<PoolAccountModelPolicy>(`/admin/scheduler-observability/probes/accounts/${accountId}/model-policy`)
  return data
}

export async function updateAccountModelPolicy(accountId: number, whitelist: string[]): Promise<PoolAccountModelPolicy> {
  const { data } = await apiClient.put<PoolAccountModelPolicy>(`/admin/scheduler-observability/probes/accounts/${accountId}/model-policy`, { whitelist })
  return data
}

export async function listResults(planId: number, limit = 100): Promise<PoolProbeResult[]> {
  const { data } = await apiClient.get<PoolProbeResult[]>(`/admin/scheduler-observability/probes/plans/${planId}/results`, { params: { limit } })
  return data ?? []
}

export async function runNow(planId: number): Promise<PoolProbeResult> {
  const { data } = await apiClient.post<PoolProbeResult>(`/admin/scheduler-observability/probes/plans/${planId}/run`)
  return data
}

export const schedulerProbesAPI = {
  getPolicy,
  updatePolicy,
  listOverview,
  getAccountModelPolicy,
  updateAccountModelPolicy,
  listResults,
  runNow,
}

export default schedulerProbesAPI
