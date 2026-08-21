import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import type { PoolMonitorAccount, PoolMonitorModel } from '@/api/admin/schedulerProbes'
import AccountModelAvailabilityCell from './AccountModelAvailabilityCell.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const model = (planId: number, status: PoolMonitorModel['status']): PoolMonitorModel => ({
  plan_id: planId,
  model: `model-${planId}`,
  enabled: true,
  status,
  latency_ms: status ? 1200 : null,
  availability: status ? 100 : null,
  sample_count: status ? 1 : 0,
  failure_count: status === 'failed' ? 1 : 0,
  last_checked_at: status ? '2026-08-14T00:00:00Z' : null,
  recent_results: [],
})

const monitor = (models: PoolMonitorModel[]): PoolMonitorAccount => ({
  account_id: 35,
  name: 'sky-pro',
  platform: 'openai',
  type: 'apikey',
  status: 'active',
  schedulable: true,
  concurrency: 10,
  models,
})

function mountCell(value: PoolMonitorAccount | null) {
  return mount(AccountModelAvailabilityCell, {
    props: { monitor: value, loading: false },
    global: {
      mocks: {
        $t: (key: string) => key,
      },
      stubs: {
        Icon: true,
        MonitorCompactHeartbeatStrip: true,
      },
    },
  })
}

describe('AccountModelAvailabilityCell', () => {
  it('uses real user traffic together with the latest probe for the outer status', () => {
    const value = model(1, 'success')
    value.user_traffic = {
      window_minutes: 30,
      success_count: 18,
      failure_count: 2,
      avg_ttft_ms: 920,
      last_success_at: '2026-08-21T02:00:00Z',
      last_failure_at: '2026-08-21T01:59:00Z',
    }

    const wrapper = mountCell(monitor([value]))

    expect(wrapper.text()).toContain('1/1')
    expect(wrapper.text()).toContain('admin.accounts.modelAvailability.states.degraded')
    expect(wrapper.get('span.font-mono').classes()).toContain('text-amber-600')
  })

  it('uses the newest embedded heartbeat and shows a slow success as available but degraded', () => {
    const staleModel = model(1, 'failed')
    staleModel.recent_results = [
      {
        id: 1,
        plan_id: 1,
        status: 'failed',
        ttft_ms: null,
        latency_ms: 178,
        started_at: '2026-08-16T03:35:00Z',
        finished_at: '2026-08-16T03:35:00Z',
        created_at: '2026-08-16T03:35:00Z',
      },
      {
        id: 2,
        plan_id: 1,
        status: 'success',
        ttft_ms: 12_000,
        latency_ms: 12_500,
        started_at: '2026-08-16T03:36:00Z',
        finished_at: '2026-08-16T03:36:00Z',
        created_at: '2026-08-16T03:36:00Z',
      },
    ]

    const wrapper = mountCell(monitor([staleModel]))

    expect(wrapper.text()).toContain('1/1')
    expect(wrapper.text()).toContain('admin.accounts.modelAvailability.states.degraded')
    expect(wrapper.get('span.font-mono').classes()).toContain('text-amber-600')
  })

  it('shows an amber partial state and opens the monitor detail', async () => {
    const value = monitor([model(1, 'success'), model(2, 'failed')])
    const wrapper = mountCell(value)

    expect(wrapper.text()).toContain('1/2')
    expect(wrapper.get('span.font-mono').classes()).toContain('text-amber-600')

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('detail')).toEqual([[value]])
  })

  it('only shows healthy when every monitored model is currently available', () => {
    const wrapper = mountCell(monitor([model(1, 'success'), model(2, 'success')]))

    expect(wrapper.text()).toContain('2/2')
    expect(wrapper.get('span.font-mono').classes()).toContain('text-emerald-600')
  })
})
