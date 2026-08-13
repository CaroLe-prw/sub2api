/**
 * Admin Channel Monitor API endpoints
 * Handles channel monitor (uptime/health) management for administrators
 */

import { apiClient } from '../client'

export type Provider = 'openai' | 'anthropic' | 'gemini' | 'grok'
export type MonitorStatus = 'operational' | 'degraded' | 'failed' | 'error'
export type BodyOverrideMode = 'off' | 'merge' | 'replace'
export type APIMode = 'chat_completions' | 'responses'

export interface ChannelMonitor {
  id: number
  name: string
  provider: Provider
  api_mode: APIMode
  endpoint: string
  api_key_masked: string
  /**
   * True when the stored encrypted API key cannot be decrypted (e.g. the
   * encryption key has changed). Admin must re-edit the monitor to provide
   * a fresh key. Backend skips checks for these monitors.
   */
  api_key_decrypt_failed?: boolean
  primary_model: string
  extra_models: string[]
  group_name: string
  enabled: boolean
  public_visible: boolean
  streaming: boolean
  interval_seconds: number
  /** 每次调度在 interval 基础上 ± [0, jitter] 的随机偏移（秒），0 = 固定间隔 */
  jitter_seconds: number
  last_checked_at: string | null
  created_by: number
  created_at: string
  updated_at: string
  /** Latest status of the primary model (empty when no history yet) */
  primary_status: MonitorStatus | ''
  /** Latest latency of the primary model in ms (null when no history yet) */
  primary_latency_ms: number | null
  /** Primary model 7-day availability percentage (0-100) */
  availability_7d: number
  /** Latest status per extra model (used for hover tooltip) */
  extra_models_status: ExtraModelStatus[]
  /** All models with admin-side probe data, including runtime auto-discovered models. */
  observed_models?: ObservedModelStatus[]
  /** 请求自定义快照字段（高级设置） */
  template_id: number | null
  extra_headers: Record<string, string>
  body_override_mode: BodyOverrideMode
  body_override: Record<string, unknown> | null
}

export interface ExtraModelStatus {
  model: string
  status: MonitorStatus | ''
  latency_ms: number | null
}

export type ObservedModelSource = 'primary' | 'manual' | 'auto'

export interface ObservedModelStatus {
  model: string
  source: ObservedModelSource
  status: MonitorStatus | ''
  latency_ms: number | null
  ping_latency_ms: number | null
  checked_at: string | null
  availability_7d: number | null
  avg_latency_7d_ms: number | null
  total_checks_7d: number
}

export interface ListParams {
  page?: number
  page_size?: number
  provider?: Provider
  enabled?: boolean
  search?: string
}

export interface ListResponse {
  items: ChannelMonitor[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface CreateParams {
  name: string
  provider: Provider
  api_mode?: APIMode
  endpoint: string
  api_key: string
  primary_model: string
  extra_models?: string[]
  group_name?: string
  enabled?: boolean
  streaming?: boolean
  interval_seconds: number
  jitter_seconds?: number
  template_id?: number | null
  extra_headers?: Record<string, string>
  body_override_mode?: BodyOverrideMode
  body_override?: Record<string, unknown> | null
}

// Update request: api_key 空串 = 不修改；clear_template=true 时把 template_id 置空
export type UpdateParams = Partial<CreateParams> & {
  clear_template?: boolean
}

export interface CheckResult {
  model: string
  status: MonitorStatus
  latency_ms: number | null
  ping_latency_ms: number | null
  message: string
  checked_at: string
}

export interface RunNowResponse {
  results: CheckResult[]
}

export interface HistoryItem {
  id: number
  model: string
  status: MonitorStatus
  latency_ms: number | null
  ping_latency_ms: number | null
  message: string
  checked_at: string
}

export interface HistoryParams {
  model?: string
  limit?: number
}

export interface HistoryResponse {
  items: HistoryItem[]
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

export interface PoolProbeResult {
  id: number
  plan_id: number
  status: 'success' | 'failed'
  response_text: string
  error_message: string
  latency_ms: number
  started_at: string
  finished_at: string
  created_at: string
}

export interface AutoModelPolicy {
  enabled: boolean
  whitelist: string[]
  discovered_by_provider: Partial<Record<Provider, string[]>>
  eligible_by_provider: Partial<Record<Provider, string[]>>
}

/**
 * List channel monitors with pagination and filters
 */
export async function list(
  params: ListParams = {},
  options?: { signal?: AbortSignal }
): Promise<ListResponse> {
  const { data } = await apiClient.get<ListResponse>('/admin/channel-monitors', {
    params,
    signal: options?.signal,
  })
  return data
}

/**
 * Get a channel monitor by ID
 */
export async function get(id: number): Promise<ChannelMonitor> {
  const { data } = await apiClient.get<ChannelMonitor>(`/admin/channel-monitors/${id}`)
  return data
}

/**
 * Create a new channel monitor
 */
export async function create(params: CreateParams): Promise<ChannelMonitor> {
  const { data } = await apiClient.post<ChannelMonitor>('/admin/channel-monitors', params)
  return data
}

/**
 * Duplicate a monitor without exposing its stored API key to the browser.
 * Keep the operation key after ambiguous failures so a retry replays the
 * original server-side operation instead of creating another monitor.
 */
const duplicateOperationKeys = new Map<string, string>()

interface DuplicateOperationScope {
  adminID: string
  key: string
}

function getCurrentAdminID(): string | null {
  try {
    const rawUser = globalThis.localStorage?.getItem('auth_user')
    if (!rawUser) return null

    const user: unknown = JSON.parse(rawUser)
    if (typeof user !== 'object' || user === null) return null

    const id = (user as { id?: unknown }).id
    if (typeof id !== 'number' || !Number.isSafeInteger(id) || id <= 0) return null
    return String(id)
  } catch {
    return null
  }
}

function duplicateOperationScope(id: number): DuplicateOperationScope | null {
  const adminID = getCurrentAdminID()
  if (!adminID) return null

  return {
    adminID,
    key: `sub2api:admin:channel-monitor-duplicate:${adminID}:${id}`,
  }
}

function getStoredDuplicateOperationKey(storageKey: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(storageKey) ?? null
  } catch {
    return null
  }
}

function storeDuplicateOperationKey(storageKey: string, key: string | null): void {
  try {
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // In-memory retry protection still works when browser storage is unavailable.
  }
}

export async function duplicate(id: number): Promise<ChannelMonitor> {
  const scope = duplicateOperationScope(id)
  let idempotencyKey = scope
    ? duplicateOperationKeys.get(scope.key) ?? getStoredDuplicateOperationKey(scope.key)
    : null
  if (!idempotencyKey) {
    const requestID = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`
    idempotencyKey = `channel-monitor-duplicate-${scope?.adminID ?? 'unknown-admin'}-${id}-${requestID}`
  }
  if (scope) {
    duplicateOperationKeys.set(scope.key, idempotencyKey)
    storeDuplicateOperationKey(scope.key, idempotencyKey)
  }

  const { data } = await apiClient.post<ChannelMonitor>(
    `/admin/channel-monitors/${id}/duplicate`,
    undefined,
    { headers: { 'Idempotency-Key': idempotencyKey } }
  )

  if (scope) {
    duplicateOperationKeys.delete(scope.key)
    storeDuplicateOperationKey(scope.key, null)
  }
  return data
}

/**
 * Update an existing channel monitor.
 * api_key field: empty string means "do not modify".
 */
export async function update(id: number, params: UpdateParams): Promise<ChannelMonitor> {
  const { data } = await apiClient.put<ChannelMonitor>(`/admin/channel-monitors/${id}`, params)
  return data
}

export async function publish(id: number, confirmName: string): Promise<ChannelMonitor> {
  const { data } = await apiClient.post<ChannelMonitor>(`/admin/channel-monitors/${id}/publish`, {
    confirm_name: confirmName,
  })
  return data
}

export async function unpublish(id: number): Promise<ChannelMonitor> {
  const { data } = await apiClient.post<ChannelMonitor>(`/admin/channel-monitors/${id}/unpublish`)
  return data
}

/**
 * Delete a channel monitor
 */
export async function del(id: number): Promise<void> {
  await apiClient.delete(`/admin/channel-monitors/${id}`)
}

/**
 * Trigger an immediate manual check for a channel monitor.
 * Returns the latest check results for primary + extra models.
 */
export async function runNow(id: number): Promise<RunNowResponse> {
  const { data } = await apiClient.post<RunNowResponse>(`/admin/channel-monitors/${id}/run`)
  return data
}

/**
 * List historical check results for a monitor.
 */
export async function listHistory(
  id: number,
  params: HistoryParams = {}
): Promise<HistoryResponse> {
  const { data } = await apiClient.get<HistoryResponse>(
    `/admin/channel-monitors/${id}/history`,
    { params }
  )
  return data
}

export async function getAutoModelPolicy(): Promise<AutoModelPolicy> {
  const { data } = await apiClient.get<AutoModelPolicy>('/admin/channel-monitors/auto-model-policy')
  return data
}

export async function updateAutoModelPolicy(input: Pick<AutoModelPolicy, 'enabled' | 'whitelist'>): Promise<AutoModelPolicy> {
  const { data } = await apiClient.put<AutoModelPolicy>('/admin/channel-monitors/auto-model-policy', input)
  return data
}

export async function listPoolOverview(): Promise<PoolMonitorOverviewResponse> {
  const { data } = await apiClient.get<PoolMonitorOverviewResponse>('/admin/channel-monitors/pool-overview')
  return data
}

export async function listPoolProbeResults(planId: number, limit = 100): Promise<PoolProbeResult[]> {
  const { data } = await apiClient.get<PoolProbeResult[]>(`/admin/scheduled-test-plans/${planId}/results`, { params: { limit } })
  return data
}

export async function runPoolProbe(planId: number): Promise<PoolProbeResult> {
  const { data } = await apiClient.post<PoolProbeResult>(`/admin/scheduled-test-plans/${planId}/run`)
  return data
}

export const channelMonitorAPI = {
  list,
  get,
  create,
  duplicate,
  update,
  publish,
  unpublish,
  del,
  runNow,
  listHistory,
  getAutoModelPolicy,
  updateAutoModelPolicy,
  listPoolOverview,
  listPoolProbeResults,
  runPoolProbe,
}

export default channelMonitorAPI
