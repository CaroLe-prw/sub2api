import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { AdminGroup } from '@/types'
import QuotaResetRunDialog from '../QuotaResetRunDialog.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('QuotaResetRunDialog', () => {
  it('submits only the independently selected one-off policy', async () => {
    const wrapper = mount(QuotaResetRunDialog, {
      props: {
        show: true,
        loading: false,
        groups: [{ id: 8, name: 'Weekly VIP' }] as AdminGroup[]
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>'
          },
          GroupSelector: {
            emits: ['update:modelValue'],
            template:
              '<button data-test="select-group" @click="$emit(\'update:modelValue\', [8])">group</button>'
          },
          QuotaResetPolicyFields: {
            emits: ['update:modelValue'],
            template:
              '<button data-test="select-policy" @click="$emit(\'update:modelValue\', { daily: false, weekly: true, monthly: true, window_start_mode: \'preserve\' })">policy</button>'
          }
        }
      }
    })

    expect(wrapper.get('[data-test="quota-reset-run-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="select-group"]').trigger('click')
    await wrapper.get('[data-test="select-policy"]').trigger('click')
    await wrapper.get('[data-test="quota-reset-keep-schedule"]').setValue()
    expect(wrapper.text()).toContain('Weekly VIP')

    await wrapper.get('#quota-reset-run-form').trigger('submit')

    expect(wrapper.emitted('submit')?.[0]?.[0]).toEqual({
      group_ids: [8],
      daily: false,
      weekly: true,
      monthly: true,
      window_start_mode: 'preserve',
      restart_schedule: false
    })
  })
})
