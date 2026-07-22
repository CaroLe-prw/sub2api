import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import QuotaResetPolicyFields from '../QuotaResetPolicyFields.vue'
import type { QuotaResetPolicy } from '@/api/admin/quotaReset'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

describe('QuotaResetPolicyFields', () => {
  const policy: QuotaResetPolicy = {
    daily: true,
    weekly: false,
    monthly: false,
    window_start_mode: 'current'
  }

  it('emits independently selected quota windows', async () => {
    const wrapper = mount(QuotaResetPolicyFields, {
      props: { modelValue: policy }
    })

    await wrapper.findAll<HTMLInputElement>('input[type="checkbox"]')[1].setValue(true)

    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({
      ...policy,
      weekly: true
    })
  })

  it('emits the selected window-time strategy', async () => {
    const wrapper = mount(QuotaResetPolicyFields, {
      props: { modelValue: policy }
    })

    await wrapper.find<HTMLInputElement>('input[value="preserve"]').setValue()

    expect(wrapper.emitted('update:modelValue')?.[0]?.[0]).toEqual({
      ...policy,
      window_start_mode: 'preserve'
    })
  })
})
