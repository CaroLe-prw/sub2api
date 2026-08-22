import type { PoolMonitorModel, PoolProbeHeartbeat } from '@/api/admin/schedulerProbes'
import { firstTokenSeverity } from '@/utils/latencyHealth'
import { resolveCurrentProbe } from './monitorCurrentProbeState'

export type CombinedMonitorHealthState = 'success' | 'degraded' | 'failed' | 'pending'

export interface CombinedMonitorHealth {
  state: CombinedMonitorHealthState
  available: boolean
  successCount: number
  failureCount: number
  userSampleCount: number
  probeSampleCount: number
  userWindowMinutes: number
}

export function summarizeCombinedHealth(
  model: PoolMonitorModel,
  samples: readonly PoolProbeHeartbeat[] = model.recent_results ?? [],
): CombinedMonitorHealth {
  const traffic = model.user_traffic
  const userSuccessCount = Math.max(0, traffic?.success_count ?? 0)
  const userFailureCount = Math.max(0, traffic?.failure_count ?? 0)
  const userSampleCount = userSuccessCount + userFailureCount
  const userSlow = userSampleCount > 0 && traffic?.avg_ttft_ms != null
    && firstTokenSeverity(traffic.avg_ttft_ms) !== 'good'

  const probe = resolveCurrentProbe(model, samples)
  const probeSampleCount = probe.state === 'pending' ? 0 : 1
  const probeSuccessCount = probe.state === 'success' || probe.state === 'degraded' ? 1 : 0
  const probeFailureCount = probe.state === 'failed' ? 1 : 0
  const successCount = userSuccessCount + probeSuccessCount
  const failureCount = userFailureCount + probeFailureCount
  const total = successCount + failureCount

  let state: CombinedMonitorHealthState = 'pending'
  if (total > 0 && successCount === 0) {
    state = 'failed'
  } else if (total > 0 && (failureCount > 0 || userSlow || probe.state === 'degraded')) {
    state = 'degraded'
  } else if (total > 0) {
    state = 'success'
  }

  return {
    state,
    available: state === 'success' || state === 'degraded',
    successCount,
    failureCount,
    userSampleCount,
    probeSampleCount,
    userWindowMinutes: traffic?.window_minutes ?? 30,
  }
}
