import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorDataPanel from './MonitorDataPanel.vue'

const { listPoolOverview, showError } = vi.hoisted(() => ({
  listPoolOverview: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      listPoolOverview,
      listPoolProbeResults: vi.fn(),
      runPoolProbe: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess: vi.fn() }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

describe('MonitorDataPanel', () => {
  beforeEach(() => {
    listPoolOverview.mockReset()
    showError.mockReset()
    listPoolOverview.mockResolvedValue({
      items: [{
        account_id: 35,
        name: 'sky-pro',
        platform: 'openai',
        type: 'apikey',
        status: 'active',
        schedulable: true,
        concurrency: 50,
        models: [{
          plan_id: 81,
          model: 'gpt-5.6-sol',
          enabled: true,
          status: 'success',
          latency_ms: 234,
          availability: 100,
          sample_count: 52,
          failure_count: 0,
          last_checked_at: '2026-08-13T08:00:00Z',
        }],
      }],
    })
  })

  it('loads existing pool accounts and switches between channel and model views', async () => {
    const wrapper = mount(MonitorDataPanel, {
      global: {
        stubs: {
          Icon: true,
          MonitorModelHistoryDialog: true,
        },
      },
    })
    await flushPromises()

    expect(listPoolOverview).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('sky-pro')

    const tabs = wrapper.findAll('button.tab')
    await tabs[1].trigger('click')
    expect(wrapper.find('table').exists()).toBe(true)
    expect(wrapper.text()).toContain('gpt-5.6-sol')
    expect(wrapper.text()).toContain('#35 · openai · apikey')
  })
})
