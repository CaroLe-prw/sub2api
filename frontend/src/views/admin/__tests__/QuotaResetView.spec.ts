import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import QuotaResetView from '../QuotaResetView.vue'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  update: vi.fn(),
  run: vi.fn(),
  getGroups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    quotaReset: {
      get: mocks.get,
      update: mocks.update,
      run: mocks.run
    },
    groups: {
      getAllIncludingInactive: mocks.getGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: mocks.showError,
    showSuccess: mocks.showSuccess
  })
}))

const response = {
  config: {
    enabled: true,
    interval_hours: 12,
    group_ids: [7],
    daily: true,
    weekly: false,
    monthly: true,
    window_start_mode: 'preserve' as const
  },
  state: {
    status: 'idle' as const,
    last_started_at: null,
    last_finished_at: null,
    last_scheduled_finished_at: null,
    last_success_at: null,
    next_run_at: null,
    matched_count: 0,
    reset_count: 0,
    skipped_count: 0,
    failed_count: 0,
    last_error: ''
  }
}

describe('QuotaResetView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue(response)
    mocks.update.mockResolvedValue(response)
    mocks.run.mockResolvedValue(response)
    mocks.getGroups.mockResolvedValue([])
  })

  it('runs a one-off policy without saving or replacing the schedule form', async () => {
    const runRequest = {
      group_ids: [9],
      daily: false,
      weekly: true,
      monthly: true,
      window_start_mode: 'natural_day',
      restart_schedule: false
    }
    const wrapper = mount(QuotaResetView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          GroupSelector: true,
          QuotaResetPolicyFields: true,
          QuotaResetRunDialog: {
            props: ['show'],
            emits: ['submit', 'close'],
            data: () => ({ runRequest }),
            template:
              '<button v-if="show" data-test="manual-run" @click="$emit(\'submit\', runRequest)">run</button>'
          }
        }
      }
    })
    await flushPromises()

    await wrapper.get('#quota-reset-interval').setValue('99')
    await wrapper.get('[data-test="quota-reset-run-now"]').trigger('click')
    await wrapper.get('[data-test="manual-run"]').trigger('click')
    await flushPromises()

    expect(mocks.update).not.toHaveBeenCalled()
    expect(mocks.run).toHaveBeenCalledWith(runRequest)
    expect((wrapper.get('#quota-reset-interval').element as HTMLInputElement).value).toBe('99')
  })

  it('allows a disabled schedule to be saved without groups', async () => {
    const disabledResponse = {
      ...response,
      config: {
        ...response.config,
        enabled: false,
        group_ids: []
      }
    }
    mocks.get.mockResolvedValue(disabledResponse)
    mocks.update.mockResolvedValue(disabledResponse)

    const wrapper = mount(QuotaResetView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          GroupSelector: true,
          QuotaResetPolicyFields: true,
          QuotaResetRunDialog: true
        }
      }
    })
    await flushPromises()

    await wrapper.get('[data-test="quota-reset-save"]').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(disabledResponse.config)
  })
})
