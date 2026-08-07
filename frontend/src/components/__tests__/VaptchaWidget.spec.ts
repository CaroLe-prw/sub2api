import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VaptchaWidget from '../VaptchaWidget.vue'

describe('VaptchaWidget', () => {
  let config: Record<string, unknown> | null
  let listeners: Record<string, () => void>
  let token: string
  let server: string

  const instance = {
    listen: vi.fn((event: string, callback: () => void) => {
      listeners[event] = callback
    }),
    render: vi.fn(),
    validate: vi.fn(),
    reset: vi.fn(),
    getServerToken: vi.fn(() => ({ token, server }))
  }

  beforeEach(() => {
    config = null
    listeners = {}
    token = 'token-1'
    server = 'https://0.vaptcha.com/verify'
    vi.clearAllMocks()
    window.vaptcha = vi.fn(async (nextConfig: Record<string, unknown>) => {
      config = nextConfig
      return instance
    })
  })

  afterEach(() => {
    delete window.vaptcha
  })

  function mountWidget() {
    return mount(VaptchaWidget, { props: { vid: 'vid-1', scene: 0 } })
  }

  it('与腾讯验证码相同，不渲染可见按钮且不会在挂载时初始化 SDK', () => {
    const wrapper = mountWidget()

    expect(wrapper.find('button').exists()).toBe(false)
    expect(window.vaptcha).not.toHaveBeenCalled()

    wrapper.unmount()
  })

  it('点击业务提交后才初始化隐藏式 VAPTCHA 并等待令牌', async () => {
    const wrapper = mountWidget()
    const verification = wrapper.vm.verify()
    await flushPromises()

    expect(config).toMatchObject({
      vid: 'vid-1',
      mode: 'invisible',
      scene: 0
    })
    expect(config).not.toHaveProperty('container')
    expect(instance.render).toHaveBeenCalledOnce()
    expect(instance.validate).toHaveBeenCalledOnce()

    listeners.pass()

    await expect(verification).resolves.toBe(
      JSON.stringify({ token: 'token-1', server: 'https://0.vaptcha.com/verify' })
    )
    expect(wrapper.emitted('verify')).toEqual([
      [JSON.stringify({ token: 'token-1', server: 'https://0.vaptcha.com/verify' })]
    ])

    wrapper.unmount()
  })

  it('用户关闭弹框时返回 null，不继续业务请求', async () => {
    const wrapper = mountWidget()
    const verification = wrapper.vm.verify()
    await flushPromises()

    listeners.close()

    await expect(verification).resolves.toBeNull()
    wrapper.unmount()
  })

  it('并发调用只打开一次验证弹框', async () => {
    const wrapper = mountWidget()
    const first = wrapper.vm.verify()
    const second = wrapper.vm.verify()
    await flushPromises()

    listeners.pass()

    await expect(first).resolves.toContain('token-1')
    await expect(second).resolves.toContain('token-1')
    expect(window.vaptcha).toHaveBeenCalledOnce()
    expect(instance.validate).toHaveBeenCalledOnce()

    wrapper.unmount()
  })

  it('重置时关闭当前验证并结束等待', async () => {
    const wrapper = mountWidget()
    const verification = wrapper.vm.verify()
    await flushPromises()

    wrapper.vm.reset()

    await expect(verification).resolves.toBeNull()
    expect(instance.reset).toHaveBeenCalled()
    wrapper.unmount()
  })
})
