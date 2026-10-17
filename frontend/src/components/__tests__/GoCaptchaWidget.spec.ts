import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GoCaptchaWidget from '@/components/GoCaptchaWidget.vue'
import { createGoCaptchaChallenge, checkGoCaptchaChallenge } from '@/api/goCaptcha'

vi.mock('@/api/goCaptcha', () => ({ createGoCaptchaChallenge: vi.fn(), checkGoCaptchaChallenge: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('go-captcha-vue', () => ({
  Slide: { name: 'Slide', props: ['config', 'data', 'events'], template: '<div />' }
}))

const Dialog = defineComponent({
  props: ['show', 'title'], emits: ['close'],
  template: '<div v-if="show"><button data-testid="close" @click="$emit(\'close\')">Close</button><slot /></div>'
})
const challenge = { id: 'first', image: 'image', thumb: 'thumb', width: 300, height: 220, thumb_width: 60, thumb_height: 60, thumb_x: 5, thumb_y: 40 }

function mountWidget() {
  return mount(GoCaptchaWidget, { global: { stubs: { BaseDialog: Dialog } } })
}

describe('GoCaptchaWidget', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    vi.mocked(createGoCaptchaChallenge).mockResolvedValue(challenge)
    vi.mocked(checkGoCaptchaChallenge).mockResolvedValue('proof')
  })

  it('loads only on demand and returns the server proof', async () => {
    const wrapper = mountWidget()
    expect(createGoCaptchaChallenge).not.toHaveBeenCalled()
    const pending = wrapper.vm.verify()
    expect(wrapper.vm.verify()).toBe(pending)
    await flushPromises()
    await wrapper.findComponent({ name: 'Slide' }).props('events').confirm({ x: 150, y: 40 })
    expect(await pending).toBe('proof')
    expect(checkGoCaptchaChallenge).toHaveBeenCalledWith('first', { x: 150, y: 40 }, expect.any(AbortSignal))
    expect(wrapper.findComponent(Dialog).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('loads a new challenge after rejection while keeping the action pending', async () => {
    vi.mocked(checkGoCaptchaChallenge).mockRejectedValueOnce(new Error('incorrect'))
    vi.mocked(createGoCaptchaChallenge).mockResolvedValueOnce(challenge).mockResolvedValueOnce({ ...challenge, id: 'second' })
    const wrapper = mountWidget()
    const pending = wrapper.vm.verify()
    await flushPromises()
    await wrapper.findComponent({ name: 'Slide' }).props('events').confirm({ x: 50, y: 40 })
    await flushPromises()
    expect(wrapper.text()).toContain('auth.goCaptcha.failed')
    expect(createGoCaptchaChallenge).toHaveBeenCalledTimes(2)
    await wrapper.findComponent({ name: 'Slide' }).props('events').confirm({ x: 150, y: 40 })
    expect(await pending).toBe('proof')
    expect(checkGoCaptchaChallenge).toHaveBeenLastCalledWith('second', { x: 150, y: 40 }, expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('cancels on close and ignores a late response from the closed dialog', async () => {
    let complete!: (value: typeof challenge) => void
    vi.mocked(createGoCaptchaChallenge).mockReturnValueOnce(new Promise(resolve => { complete = resolve }))
    const wrapper = mountWidget()
    const pending = wrapper.vm.verify()
    await flushPromises()
    await wrapper.get('[data-testid="close"]').trigger('click')
    expect(await pending).toBeNull()
    expect(vi.mocked(createGoCaptchaChallenge).mock.calls[0][0]?.aborted).toBe(true)
    const next = wrapper.vm.verify()
    await flushPromises()
    complete({ ...challenge, id: 'stale' })
    await flushPromises()
    await wrapper.findComponent({ name: 'Slide' }).props('events').confirm({ x: 150, y: 40 })
    expect(await next).toBe('proof')
    expect(checkGoCaptchaChallenge).toHaveBeenCalledWith('first', expect.any(Object), expect.any(AbortSignal))
    wrapper.unmount()
  })

  it('allows retry after loading fails and resolves on unmount', async () => {
    vi.mocked(createGoCaptchaChallenge).mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountWidget()
    const pending = wrapper.vm.verify()
    await flushPromises()
    expect(wrapper.text()).toContain('auth.goCaptcha.unavailable')
    await wrapper.findAll('button').find(button => button.text() === 'auth.goCaptcha.retry')!.trigger('click')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'Slide' }).exists()).toBe(true)
    wrapper.unmount()
    expect(await pending).toBeNull()
  })
})
