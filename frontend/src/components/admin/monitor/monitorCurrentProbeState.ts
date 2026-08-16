import type { PoolMonitorModel, PoolProbeHeartbeat } from '@/api/admin/schedulerProbes'
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
