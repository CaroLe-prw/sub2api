import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupSchedulerPolicyField from '../GroupSchedulerPolicyField.vue'
import { createDefaultOpenAISchedulerConfig } from '../groupSchedulerPolicy'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: { value?: number }) =>
      params?.value === undefined ? key : `${key}:${params.value}`,
  }),
}))

const mountField = () =>
  mount(GroupSchedulerPolicyField, {
    props: {
      profile: 'inherit',
      config: createDefaultOpenAISchedulerConfig(),
    },
  })

describe('GroupSchedulerPolicyField', () => {
  it('keeps custom controls hidden for inherited scheduling', () => {
    const wrapper = mountField()

    expect(wrapper.findAll('input')).toHaveLength(0)
  })

  it('selects a custom profile and emits immutable weight updates', async () => {
    const wrapper = mountField()
    await wrapper.get('select').setValue('custom')

    expect(wrapper.emitted('update:profile')?.at(-1)).toEqual(['custom'])
    await wrapper.setProps({ profile: 'custom' })

    const inputs = wrapper.findAll('input[type="number"]')
    expect(inputs).toHaveLength(11)
    expect(inputs.map((input) => input.element.value)).toEqual(
      Array.from({ length: 11 }, () => ''),
    )
    expect(inputs.map((input) => input.attributes('placeholder'))).toEqual([
      'admin.groups.scheduler.defaultPlaceholder:7',
      'admin.groups.scheduler.defaultPlaceholder:1',
      'admin.groups.scheduler.defaultPlaceholder:1',
      'admin.groups.scheduler.defaultPlaceholder:0.7',
      'admin.groups.scheduler.defaultPlaceholder:0.8',
      'admin.groups.scheduler.defaultPlaceholder:0.5',
      'admin.groups.scheduler.defaultPlaceholder:0',
      'admin.groups.scheduler.defaultPlaceholder:0',
      'admin.groups.scheduler.defaultPlaceholder:0',
      'admin.groups.scheduler.defaultPlaceholder:5',
      'admin.groups.scheduler.defaultPlaceholder:3',
    ])
    await inputs[8].setValue('9')

    const emittedConfig = wrapper.emitted('update:config')?.at(-1)?.[0]
    expect(emittedConfig).toMatchObject({
      upstream_cost: 9,
      top_k: null,
    })

    await wrapper.setProps({
      config: {
        ...createDefaultOpenAISchedulerConfig(),
        upstream_cost: 9,
      },
    })
    await inputs[8].setValue('')
    expect(wrapper.emitted('update:config')?.at(-1)?.[0]).toMatchObject({
      upstream_cost: null,
    })
  })
})
