import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UpstreamBalanceAlertFields from '../UpstreamBalanceAlertFields.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('UpstreamBalanceAlertFields', () => {
  it('enables the threshold input and emits its numeric value', async () => {
    const wrapper = mount(UpstreamBalanceAlertFields, {
      props: {
        enabled: false,
        threshold: 10,
        'onUpdate:enabled': (value: boolean) => wrapper.setProps({ enabled: value }),
        'onUpdate:threshold': (value: number | null) => wrapper.setProps({ threshold: value })
      }
    })

    await wrapper.get('[data-testid="upstream-balance-alert-enabled"]').trigger('click')
    const input = wrapper.get('[data-testid="upstream-balance-alert-threshold"]')
    await input.setValue('25.5')

    expect(wrapper.props('enabled')).toBe(true)
    expect(wrapper.props('threshold')).toBe(25.5)
  })
})
