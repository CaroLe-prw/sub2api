import { describe, expect, it } from 'vitest'

import type { PoolMonitorAccount, PoolProbeResult } from '@/api/admin/schedulerProbes'
import { mergeMonitorHistories, resolveCurrentProbe } from './monitorCurrentProbeState'

const account: PoolMonitorAccount = {
  account_id: 35,
  name: 'sky-pro',
  platform: 'openai',
  type: 'apikey',
  status: 'active',
  schedulable: true,
  concurrency: 10,
  models: [{
    plan_id: 81,
    model: 'gpt-5.6-terra',
    enabled: true,
    status: 'success',
    latency_ms: 1_200,
    availability: 100,
    sample_count: 1,
    failure_count: 0,
    last_checked_at: '2026-08-21T01:00:00Z',
    recent_results: [],
  }],
}

function result(id: number, status: PoolProbeResult['status'], createdAt: string): PoolProbeResult {
  return {
    id,
    plan_id: 81,
    status,
    response_text: status === 'success' ? 'ok' : '',
    error_message: status === 'failed' ? 'upstream error' : '',
    ttft_ms: null,
    latency_ms: 178,
    started_at: createdAt,
    finished_at: createdAt,
    created_at: createdAt,
  }
}

describe('mergeMonitorHistories', () => {
  it('updates the overview snapshot when detail history contains a newer failed probe', () => {
    const latestFailure = result(2, 'failed', '2026-08-21T01:01:00Z')

    const merged = mergeMonitorHistories(account, { 81: [latestFailure] })
    const model = merged.models[0]

    expect(model.status).toBe('failed')
    expect(model.last_checked_at).toBe(latestFailure.finished_at)
    expect(resolveCurrentProbe(model, model.recent_results ?? []).state).toBe('failed')
  })

  it('does not roll a newer overview snapshot back to stale detail history', () => {
    const overviewFailure = result(3, 'failed', '2026-08-21T01:02:00Z')
    const currentAccount: PoolMonitorAccount = {
      ...account,
      models: [{
        ...account.models[0],
        status: 'failed',
        last_checked_at: overviewFailure.finished_at,
        recent_results: [overviewFailure],
      }],
    }
    const staleSuccess = result(2, 'success', '2026-08-21T01:01:00Z')

    expect(mergeMonitorHistories(currentAccount, { 81: [staleSuccess] })).toBe(currentAccount)
  })
})
