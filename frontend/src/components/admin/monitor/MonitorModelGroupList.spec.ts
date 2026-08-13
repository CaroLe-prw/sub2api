import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { PoolMonitorAccount } from '@/api/admin/channelMonitor'

import MonitorModelGroupList from './MonitorModelGroupList.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key }),
  }
})

function account(accountId: number, name: string, models: Array<{ planId: number; model: string; status: 'success' | 'failed' }>): PoolMonitorAccount {
  return {
    account_id: accountId,
    name,
    platform: 'openai',
    type: 'apikey',
    status: 'active',
    schedulable: true,
    concurrency: 50,
    models: models.map((item) => ({
      plan_id: item.planId,
      model: item.model,
      enabled: true,
      status: item.status,
      latency_ms: 300,
      availability: item.status === 'success' ? 100 : 90,
      sample_count: 10,
      failure_count: item.status === 'success' ? 0 : 1,
      last_checked_at: '2026-08-13T08:00:00Z',
    })),
  }
}

describe('MonitorModelGroupList', () => {
  it('groups channels under their model instead of rendering a flat model table', async () => {
    const sky = account(35, 'sky-pro', [
      { planId: 81, model: 'gpt-5.6-sol', status: 'success' },
      { planId: 82, model: 'gpt-5.6-terra', status: 'success' },
    ])
    const coder = account(324, 'coder', [
      { planId: 91, model: 'gpt-5.6-sol', status: 'failed' },
    ])
    const wrapper = mount(MonitorModelGroupList, {
      props: { accounts: [sky, coder] },
      global: { stubs: { Icon: true } },
    })

    const groups = wrapper.findAll('[data-testid="model-group"]')
    expect(groups).toHaveLength(2)

    const solGroup = wrapper.get('[data-model="gpt-5.6-sol"]')
    expect(solGroup.find('table').exists()).toBe(false)
    await solGroup.get('button[aria-expanded]').trigger('click')
    expect(solGroup.text()).toContain('sky-pro')
    expect(solGroup.text()).toContain('coder')
    expect(solGroup.findAll('tbody tr')).toHaveLength(2)
    expect(solGroup.text()).toContain('"healthy":1,"total":2')

    await solGroup.get('tbody button').trigger('click')
    expect(wrapper.emitted('detail')?.[0]).toEqual([coder])
  })

  it('shows only ten channels per expanded model page', async () => {
    const accounts = Array.from({ length: 23 }, (_, index) => account(
      index + 1,
      `channel-${index + 1}`,
      [{ planId: index + 100, model: 'gpt-5.6-sol', status: 'success' }],
    ))
    const wrapper = mount(MonitorModelGroupList, {
      props: { accounts },
      global: { stubs: { Icon: true } },
    })
    const group = wrapper.get('[data-model="gpt-5.6-sol"]')
    await group.get('button[aria-expanded]').trigger('click')

    expect(group.findAll('tbody tr')).toHaveLength(10)
    expect(group.text()).not.toContain('channel-23')

    const next = group.findAll('button').find((button) => button.attributes('aria-label') === 'pagination.next')
    expect(next).toBeDefined()
    await next!.trigger('click')

    expect(group.findAll('tbody tr')).toHaveLength(10)
  })
})
