import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupAccountCostLimitField from '../GroupAccountCostLimitField.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const mountField = (modelValue: number | null = null) =>
  mount(GroupAccountCostLimitField, {
    props: {
      modelValue,
      billingRateMultiplier: 1,
    },
  })

describe('GroupAccountCostLimitField', () => {
  it('emits an explicit scheduling cost ceiling', async () => {
    const wrapper = mountField()
    await wrapper.get('input').setValue('0.05')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([0.05])
  })

  it('emits null when the field is cleared', async () => {
    const wrapper = mountField(0.4)
    await wrapper.get('input').setValue('')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])
  })
})
