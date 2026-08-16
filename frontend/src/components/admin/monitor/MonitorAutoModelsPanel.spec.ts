import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorAutoModelsPanel from './MonitorAutoModelsPanel.vue'

const { getPolicy, updatePolicy, showSuccess, showError } = vi.hoisted(() => ({
  getPolicy: vi.fn(),
  updatePolicy: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    schedulerProbes: {
      getPolicy,
      updatePolicy,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const policy = {
  enabled: true,
  whitelist: ['gpt-5.*'],
  discovered_by_provider: { openai: ['gpt-5.6', 'gpt-4o-mini'] },
  eligible_by_provider: { openai: ['gpt-5.6'] },
}

describe('MonitorAutoModelsPanel', () => {
  beforeEach(() => {
    for (const fn of [getPolicy, updatePolicy, showSuccess, showError]) fn.mockReset()
    getPolicy.mockResolvedValue(policy)
    updatePolicy.mockImplementation(async (input) => ({ ...policy, ...input }))
  })

  it('loads inventory and saves a normalized newline/comma allowlist', async () => {
    const wrapper = mount(MonitorAutoModelsPanel, {
      global: { stubs: { Toggle: true } },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('gpt-5.6')
    await wrapper.get('textarea').setValue('gpt-5.*\nclaude-* , gemini-3-*')
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(updatePolicy).toHaveBeenCalledWith({
      enabled: true,
      mode: 'fixed',
      fixed_interval_minutes: 5,
      whitelist: ['gpt-5.*', 'claude-*', 'gemini-3-*'],
    })
    expect(showSuccess).toHaveBeenCalled()
  })
})
