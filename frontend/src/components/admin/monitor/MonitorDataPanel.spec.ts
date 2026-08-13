import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorDataPanel from './MonitorDataPanel.vue'
import MonitorAccountWhitelistDialog from './MonitorAccountWhitelistDialog.vue'

const { listPoolOverview, getPoolAccountModelPolicy, updatePoolAccountModelPolicy, showError, showSuccess } = vi.hoisted(() => ({
  listPoolOverview: vi.fn(),
  getPoolAccountModelPolicy: vi.fn(),
  updatePoolAccountModelPolicy: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      listPoolOverview,
      listPoolProbeResults: vi.fn(),
      runPoolProbe: vi.fn(),
      getPoolAccountModelPolicy,
      updatePoolAccountModelPolicy,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
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
    showSuccess.mockReset()
    getPoolAccountModelPolicy.mockReset()
    updatePoolAccountModelPolicy.mockReset()
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
    getPoolAccountModelPolicy.mockResolvedValue({
      account_id: 35,
      whitelist: ['gpt-5.6-sol'],
      discovered_models: ['gpt-5.6-sol'],
      effective_models: ['gpt-5.6-sol'],
    })
    updatePoolAccountModelPolicy.mockResolvedValue({
      account_id: 35,
      whitelist: ['gpt-5.6-sol'],
      discovered_models: ['gpt-5.6-sol'],
      effective_models: ['gpt-5.6-sol'],
    })
  })

  it('loads existing pool accounts and switches to model groups', async () => {
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
    expect(wrapper.find('[data-testid="model-group"]').exists()).toBe(true)
    expect(wrapper.find('table').exists()).toBe(true)
    expect(wrapper.text()).toContain('gpt-5.6-sol')
    expect(wrapper.text()).toContain('#35 · openai · apikey')
  })

  it('loads and saves a per-channel model allowlist', async () => {
    const wrapper = mount(MonitorDataPanel, {
      global: { stubs: { Icon: true, MonitorModelHistoryDialog: true } },
    })
    await flushPromises()

    const manageButton = wrapper.findAll('button').find((button) => button.text().includes('manageModels'))
    expect(manageButton).toBeDefined()
    await manageButton!.trigger('click')
    await flushPromises()

    expect(getPoolAccountModelPolicy).toHaveBeenCalledWith(35)
    const dialog = wrapper.findComponent(MonitorAccountWhitelistDialog)
    dialog.vm.$emit('save', ['gpt-5.6-sol'])
    await flushPromises()

    expect(updatePoolAccountModelPolicy).toHaveBeenCalledWith(35, ['gpt-5.6-sol'])
    expect(showSuccess).toHaveBeenCalled()
  })
})
