import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorPublishDialog from './MonitorPublishDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const monitor = { id: 7, name: 'internal-upstream' } as ChannelMonitor

describe('MonitorPublishDialog', () => {
  it('requires the exact monitor name before emitting publish confirmation', async () => {
    const wrapper = mount(MonitorPublishDialog, {
      props: { show: true, monitor, publishing: false },
      global: {
        stubs: {
          Teleport: true,
          Transition: false,
        },
      },
    })

    const confirm = wrapper.findAll('button').at(-1)!
    expect(confirm.attributes('disabled')).toBeDefined()

    await wrapper.get('input').setValue('wrong')
    expect(confirm.attributes('disabled')).toBeDefined()

    await wrapper.get('input').setValue('internal-upstream')
    expect(confirm.attributes('disabled')).toBeUndefined()
    await confirm.trigger('click')
    expect(wrapper.emitted('confirm')).toEqual([['internal-upstream']])
  })
})
