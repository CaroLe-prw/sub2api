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
