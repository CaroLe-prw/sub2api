import { describe, expect, it } from 'vitest'

import type { PoolMonitorModel, PoolProbeHeartbeat } from '@/api/admin/schedulerProbes'
import { summarizeCombinedHealth } from './monitorCombinedHealth'

function heartbeat(status: PoolProbeHeartbeat['status']): PoolProbeHeartbeat {
  return {
    id: 2,
    plan_id: 81,
    status,
    ttft_ms: status === 'success' ? 900 : null,
    latency_ms: 1_200,
    started_at: '2026-08-21T02:00:00Z',
    finished_at: '2026-08-21T02:00:01Z',
    created_at: '2026-08-21T02:00:01Z',
  }
}

function model(probeStatus: PoolMonitorModel['status']): PoolMonitorModel {
  return {
    plan_id: 81,
    model: 'gpt-5.6-sol',
    enabled: true,
    status: probeStatus,
    latency_ms: 1_200,
    availability: 100,
    sample_count: 1,
    failure_count: probeStatus === 'failed' ? 1 : 0,
    last_checked_at: '2026-08-21T02:00:01Z',
    recent_results: [heartbeat(probeStatus || 'success')],
    user_traffic: {
      window_minutes: 30,
      success_count: 18,
      failure_count: 2,
      avg_ttft_ms: 920,
      last_success_at: '2026-08-21T02:00:00Z',
      last_failure_at: '2026-08-21T01:59:00Z',
    },
  }
}

describe('summarizeCombinedHealth', () => {
  it('marks mixed real traffic and a successful probe as available with issues', () => {
    const summary = summarizeCombinedHealth(model('success'))

    expect(summary.state).toBe('degraded')
    expect(summary.available).toBe(true)
    expect(summary.userSampleCount).toBe(20)
    expect(summary.probeSampleCount).toBe(1)
    expect(summary.successCount).toBe(19)
    expect(summary.failureCount).toBe(2)
  })

  it('lets successful real traffic keep a failed probe from reporting all failed', () => {
    const summary = summarizeCombinedHealth(model('failed'))

    expect(summary.state).toBe('degraded')
    expect(summary.available).toBe(true)
    expect(summary.failureCount).toBe(3)
  })

  it('falls back to the active probe when no user has called the model', () => {
    const value = model('success')
    value.user_traffic = {
      window_minutes: 30,
      success_count: 0,
      failure_count: 0,
      avg_ttft_ms: null,
      last_success_at: null,
      last_failure_at: null,
    }

    const summary = summarizeCombinedHealth(value)

    expect(summary.state).toBe('success')
    expect(summary.userSampleCount).toBe(0)
    expect(summary.probeSampleCount).toBe(1)
  })
})
