import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

import GroupSelector from '../GroupSelector.vue'

const groups = [
  { id: 1, name: 'Plus', platform: 'openai', account_count: 9 },
  { id: 2, name: 'Pro', platform: 'openai', account_count: 8 }
] as any

describe('GroupSelector', () => {
  it('edits selected group priorities inline without rendering a duplicate list', async () => {
    const wrapper = mount(GroupSelector, {
      props: {
        modelValue: [1],
        priorities: { 1: 100 },
        groups
      },
      global: {
        stubs: {
          GroupBadge: {
            props: ['name'],
            template: '<span>{{ name }}</span>'
          },
          Icon: true
        }
      }
    })

    const priorityInput = wrapper.get('input[type="number"]')
    expect((priorityInput.element as HTMLInputElement).value).toBe('100')
    expect(wrapper.findAll('input[type="number"]')).toHaveLength(1)

    await priorityInput.setValue('110')
    expect(wrapper.emitted('update:priorities')?.at(-1)).toEqual([{ 1: 110 }])
  })

  it('uses the account priority when selecting another group', async () => {
    const wrapper = mount(GroupSelector, {
      props: {
        modelValue: [1],
        priorities: { 1: 37 },
        defaultPriority: 37,
        groups
      },
      global: {
        stubs: {
          GroupBadge: true,
          Icon: true
        }
      }
    })

    await wrapper.get('input[type="checkbox"][value="2"]').setValue(true)
    expect(wrapper.emitted('update:priorities')?.at(-1)).toEqual([{ 1: 37, 2: 37 }])
  })

  it('hides equal priorities until custom mode is enabled', async () => {
    const wrapper = mount(GroupSelector, {
      props: {
        modelValue: [1],
        priorities: { 1: 37 },
        defaultPriority: 37,
        groups
      },
      global: {
        stubs: {
          GroupBadge: true,
          Icon: true
        }
      }
    })

    expect(wrapper.find('input[type="number"]').exists()).toBe(false)

    await wrapper.get('[data-testid="custom-group-priority-toggle"]').trigger('click')
    expect((wrapper.get('input[type="number"]').element as HTMLInputElement).value).toBe('37')
  })

  it('resets custom values when custom mode is disabled', async () => {
    const wrapper = mount(GroupSelector, {
      props: {
        modelValue: [1],
        priorities: { 1: 100 },
        defaultPriority: 37,
        groups
      },
      global: {
        stubs: {
          GroupBadge: true,
          Icon: true
        }
      }
    })

    await wrapper.get('[data-testid="custom-group-priority-toggle"]').trigger('click')
    expect(wrapper.emitted('update:priorities')?.at(-1)).toEqual([{ 1: 37 }])
    expect(wrapper.find('input[type="number"]').exists()).toBe(false)
  })
})
