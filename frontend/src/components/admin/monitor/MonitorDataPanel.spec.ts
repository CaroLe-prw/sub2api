import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorDataPanel from './MonitorDataPanel.vue'
import MonitorAccountWhitelistDialog from './MonitorAccountWhitelistDialog.vue'

const { listPoolOverview, listProbeResults, runProbeNow, getPoolAccountModelPolicy, updatePoolAccountModelPolicy, showError, showSuccess } = vi.hoisted(() => ({
  listPoolOverview: vi.fn(),
  listProbeResults: vi.fn(),
  runProbeNow: vi.fn(),
  getPoolAccountModelPolicy: vi.fn(),
  updatePoolAccountModelPolicy: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    schedulerProbes: {
      listOverview: listPoolOverview,
      listResults: listProbeResults,
      runNow: runProbeNow,
      getAccountModelPolicy: getPoolAccountModelPolicy,
      updateAccountModelPolicy: updatePoolAccountModelPolicy,
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
    listProbeResults.mockReset()
    runProbeNow.mockReset()
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
          recent_results: [{
            id: 1,
            plan_id: 81,
            status: 'success',
            ttft_ms: 200,
            latency_ms: 234,
            started_at: '2026-08-13T07:59:59Z',
            finished_at: '2026-08-13T08:00:00Z',
            created_at: '2026-08-13T08:00:00Z',
          }],
        }],
      }],
    })
    listProbeResults.mockResolvedValue([])
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
    expect(wrapper.findAll('[data-testid="compact-heartbeat-strip"]')).toHaveLength(1)

    const tabs = wrapper.findAll('button.tab')
    await tabs[1].trigger('click')
    const group = wrapper.get('[data-testid="model-group"]')
    await group.get('button[aria-expanded]').trigger('click')
    expect(wrapper.find('table').exists()).toBe(true)
    expect(wrapper.text()).toContain('gpt-5.6-sol')
    expect(wrapper.text()).toContain('#35 · openai · apikey')
    expect(wrapper.findAll('[data-testid="compact-heartbeat-strip"]')).toHaveLength(1)
  })

  it('shows twelve compact model cards per page', async () => {
    const base = (await listPoolOverview()).items[0]
    listPoolOverview.mockResolvedValue({
      items: [{
        ...base,
        models: Array.from({ length: 13 }, (_, index) => ({
          ...base.models[0],
          plan_id: 100 + index,
          model: `model-${String(index + 1).padStart(2, '0')}`,
        })),
      }],
    })
    const wrapper = mount(MonitorDataPanel, {
      global: { stubs: { Icon: true, MonitorModelHistoryDialog: true } },
    })
    await flushPromises()

    await wrapper.findAll('button.tab')[1].trigger('click')
    expect(wrapper.findAll('[data-testid="model-group"]')).toHaveLength(12)
    expect(wrapper.text()).toContain('model-01')
    expect(wrapper.text()).not.toContain('model-13')

    const next = wrapper.findAll('button').find((button) => button.attributes('aria-label') === 'pagination.next')
    expect(next).toBeDefined()
    await next!.trigger('click')

    expect(wrapper.findAll('[data-testid="model-group"]')).toHaveLength(1)
    expect(wrapper.text()).toContain('model-13')
  })

  it('paginates the channel card view instead of rendering every channel', async () => {
    const base = (await listPoolOverview()).items[0]
    listPoolOverview.mockResolvedValue({
      items: Array.from({ length: 13 }, (_, index) => ({
        ...base,
        account_id: index + 1,
        name: `channel-${index + 1}`,
        models: base.models.map((model: { plan_id: number }) => ({ ...model, plan_id: model.plan_id + index })),
      })),
    })
    const wrapper = mount(MonitorDataPanel, {
      global: { stubs: { Icon: true, MonitorModelHistoryDialog: true } },
    })
    await flushPromises()

    expect(wrapper.findAll('article')).toHaveLength(12)
    expect(wrapper.text()).toContain('channel-1')
    expect(wrapper.text()).not.toContain('channel-13')

    const next = wrapper.findAll('button').find((button) => button.attributes('aria-label') === 'pagination.next')
    expect(next).toBeDefined()
    await next!.trigger('click')

    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(wrapper.text()).toContain('channel-13')
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

  it('synchronizes the selected overview snapshot with newer detail history', async () => {
    const overview = (await listPoolOverview()).items[0]
    listPoolOverview.mockResolvedValue({
      items: [{
        ...overview,
        models: [{
          ...overview.models[0],
          status: 'failed',
          last_checked_at: '2026-08-13T08:00:00Z',
        }],
      }],
    })
    listProbeResults.mockResolvedValue([{
      id: 2,
      plan_id: 81,
      status: 'success',
      ttft_ms: 200,
      latency_ms: 253,
      started_at: '2026-08-13T08:04:59Z',
      finished_at: '2026-08-13T08:05:00Z',
      created_at: '2026-08-13T08:05:00Z',
      response_text: 'ok',
      error_message: '',
    }])

    const wrapper = mount(MonitorDataPanel, {
      global: {
        stubs: {
          Icon: true,
          MonitorModelHistoryDialog: {
            props: ['show', 'account', 'histories', 'loading', 'runningPlanId'],
            template: '<div data-testid="history-dialog" />',
          },
        },
      },
    })
    await flushPromises()

    const detailButton = wrapper.findAll('button').find((button) => button.text().includes('detail'))
    expect(detailButton).toBeDefined()
    await detailButton!.trigger('click')
    await flushPromises()

    const dialog = wrapper.getComponent('[data-testid="history-dialog"]')
    expect(dialog.props('account').models[0].status).toBe('success')
    expect(dialog.props('account').models[0].latency_ms).toBe(253)
  })

  it('shows traffic-only models without requesting a nonexistent probe history', async () => {
    const overview = (await listPoolOverview()).items[0]
    listPoolOverview.mockResolvedValue({
      items: [{
        ...overview,
        models: [
          { ...overview.models[0], has_probe: true },
          {
            plan_id: 0,
            has_probe: false,
            model: 'gpt-5.6-sol',
            enabled: false,
            status: '',
            latency_ms: null,
            availability: null,
            sample_count: 0,
            failure_count: 0,
            last_checked_at: null,
            recent_results: [],
            user_traffic: {
              window_minutes: 30,
              success_count: 4,
              failure_count: 0,
              avg_ttft_ms: 430,
              last_success_at: '2026-08-13T08:06:00Z',
              last_failure_at: null,
            },
          },
        ],
      }],
    })

    const wrapper = mount(MonitorDataPanel, {
      global: {
        stubs: {
          Icon: true,
          MonitorModelHistoryDialog: {
            props: ['show', 'account', 'histories', 'loading', 'runningPlanId'],
            template: '<div data-testid="history-dialog" />',
          },
        },
      },
    })
    await flushPromises()

    const detailButton = wrapper.findAll('button').find((button) => button.text().includes('detail'))
    expect(detailButton).toBeDefined()
    await detailButton!.trigger('click')
    await flushPromises()

    expect(listProbeResults).toHaveBeenCalledTimes(1)
    expect(listProbeResults).toHaveBeenCalledWith(81, 100)
    expect(listProbeResults).not.toHaveBeenCalledWith(0, 100)
    expect(wrapper.getComponent('[data-testid="history-dialog"]').props('account').models).toHaveLength(2)
  })
})
