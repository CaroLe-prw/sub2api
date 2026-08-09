import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import GroupRateGuardFields from '../GroupRateGuardFields.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      key === 'keys.currentGroupRate' ? `Current: ${params?.rate}x` : key,
  }),
}))

describe('GroupRateGuardFields', () => {
  it('is off by default and initializes the threshold from the current rate', async () => {
    const wrapper = mount(GroupRateGuardFields, {
      props: {
        currentRate: 0.08,
        enabled: false,
        threshold: null,
        'onUpdate:enabled': (value: boolean) => wrapper.setProps({ enabled: value }),
      },
    })

    expect(wrapper.find('input[type="number"]').exists()).toBe(false)
    await wrapper.get('button[role="switch"]').trigger('click')

    expect(wrapper.props('enabled')).toBe(true)
    expect(wrapper.emitted('update:threshold')).toEqual([[0.08]])
    await wrapper.setProps({ threshold: 0.08 })
    expect(wrapper.get('input[type="number"]').element.value).toBe('0.08')
  })

  it('warns when the current rate is above the configured threshold', () => {
    const wrapper = mount(GroupRateGuardFields, {
      props: {
        currentRate: 0.09,
        enabled: true,
        threshold: 0.08,
      },
    })

    expect(wrapper.text()).toContain('keys.groupRateGuardCurrentlyBlocked')
    expect(wrapper.get('input[type="number"]').classes()).toContain('border-red-500')
  })

  it('accepts common decimal thresholds without a native step mismatch', async () => {
    const wrapper = mount(GroupRateGuardFields, {
      props: {
        currentRate: 0.065,
        enabled: true,
        threshold: 0.05,
      },
    })
    const input = wrapper.get('input[type="number"]')

    expect((input.element as HTMLInputElement).checkValidity()).toBe(true)

    await input.setValue('0')
    expect((input.element as HTMLInputElement).checkValidity()).toBe(false)
  })
})
