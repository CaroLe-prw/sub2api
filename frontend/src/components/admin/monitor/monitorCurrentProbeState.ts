import type { PoolMonitorAccount, PoolMonitorModel, PoolProbeHeartbeat } from '@/api/admin/schedulerProbes'
import { firstTokenSeverity } from '@/utils/latencyHealth'

export type CurrentProbeState = 'success' | 'degraded' | 'failed' | 'pending'

export interface CurrentProbeSnapshot {
  state: CurrentProbeState
  latencyMs: number | null
  latest: PoolProbeHeartbeat | null
}

export function latestProbeResult(samples: readonly PoolProbeHeartbeat[]): PoolProbeHeartbeat | null {
  return samples.reduce<PoolProbeHeartbeat | null>((latest, item) => {
    if (!latest) return item
    const itemCreatedAt = Date.parse(item.created_at)
    const latestCreatedAt = Date.parse(latest.created_at)
    if (itemCreatedAt !== latestCreatedAt) return itemCreatedAt > latestCreatedAt ? item : latest
    return item.id > latest.id ? item : latest
  }, null)
}

export function resolveCurrentProbe(
  model: Pick<PoolMonitorModel, 'status' | 'latency_ms'>,
  samples: readonly PoolProbeHeartbeat[],
): CurrentProbeSnapshot {
  const latest = latestProbeResult(samples)
  const status = latest?.status ?? model.status
  let state: CurrentProbeState = 'pending'
  if (status === 'failed') {
    state = 'failed'
  } else if (status === 'success') {
    state = latest?.ttft_ms != null && firstTokenSeverity(latest.ttft_ms) !== 'good'
      ? 'degraded'
      : 'success'
  }
  return {
    state,
    latencyMs: latest?.latency_ms ?? model.latency_ms,
    latest,
  }
}

type ProbeHistories = Record<number, readonly PoolProbeHeartbeat[] | null | undefined>

function isNewerThanOverview(
  candidate: PoolProbeHeartbeat,
  model: PoolMonitorModel,
): boolean {
  const embeddedLatest = latestProbeResult(model.recent_results ?? [])
  if (embeddedLatest) {
    const candidateCreatedAt = Date.parse(candidate.created_at)
    const embeddedCreatedAt = Date.parse(embeddedLatest.created_at)
    if (!Number.isNaN(candidateCreatedAt) && !Number.isNaN(embeddedCreatedAt)) {
      if (candidateCreatedAt !== embeddedCreatedAt) return candidateCreatedAt > embeddedCreatedAt
      return candidate.id >= embeddedLatest.id
    }
    return candidate.id >= embeddedLatest.id
  }

  if (!model.last_checked_at) return true
  const candidateFinishedAt = Date.parse(candidate.finished_at)
  const overviewCheckedAt = Date.parse(model.last_checked_at)
  if (Number.isNaN(candidateFinishedAt) || Number.isNaN(overviewCheckedAt)) return true
  return candidateFinishedAt >= overviewCheckedAt
}

/**
 * Bring a list overview account forward to the newest snapshot already loaded
 * by the detail dialog. This prevents the dialog and its originating cell from
 * reporting different current states when a probe finishes between requests.
 */
export function mergeMonitorHistories(
  account: PoolMonitorAccount,
  histories: ProbeHistories,
): PoolMonitorAccount {
  let changed = false
  const models = account.models.map((model) => {
    const samples = histories[model.plan_id]
    if (!Array.isArray(samples) || samples.length === 0) return model

    const latest = latestProbeResult(samples)
    if (!latest || !isNewerThanOverview(latest, model)) return model

    changed = true
    return {
      ...model,
      status: latest.status,
      latency_ms: latest.latency_ms,
      last_checked_at: latest.finished_at,
      recent_results: [...samples],
    }
  })

  return changed ? { ...account, models } : account
}
