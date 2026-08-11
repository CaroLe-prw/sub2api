import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import VaptchaWidget from '../VaptchaWidget.vue'

describe('VaptchaWidget', () => {
  let config: Record<string, unknown> | null
  let validationResult: {
    token?: string
    knock?: string
    dfu?: string
    ip?: string
  } | null

  const instance = {
    validate: vi.fn(async () => validationResult),
    reset: vi.fn()
  }

  beforeEach(() => {
    config = null
    validationResult = {
      token: '1700000000.token-id.signature',
      knock: 'knock-1',
      dfu: 'dfu-1',
      ip: '203.0.113.8'
    }
    vi.clearAllMocks()
    window.vaptcha = vi.fn(async (nextConfig: Record<string, unknown>) => {
      config = nextConfig
      return instance
    })
  })

  afterEach(() => {
    delete window.vaptcha
    vi.restoreAllMocks()
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

  it('从官方 V4 地址加载 SDK', async () => {
    delete window.vaptcha
    const appendSpy = vi.spyOn(document.head, 'appendChild').mockImplementation((node) => {
      const script = node as HTMLScriptElement
      window.vaptcha = vi.fn(async (nextConfig: Record<string, unknown>) => {
        config = nextConfig
        return instance
      })
      queueMicrotask(() => script.onload?.(new Event('load')))
      return node
    })
    const wrapper = mountWidget()

    await expect(wrapper.vm.verify()).resolves.toContain('token-id')

    expect(appendSpy).toHaveBeenCalledOnce()
    expect((appendSpy.mock.calls[0]?.[0] as HTMLScriptElement).src).toBe(
      'https://c4.vaptcha.com/src/v4.js'
    )
    wrapper.unmount()
  })

  it('点击业务提交后按 V4 协议初始化并返回完整验签参数', async () => {
    const wrapper = mountWidget()
    const verification = wrapper.vm.verify()
    await flushPromises()

    expect(config).toMatchObject({
      vid: 'vid-1',
      lang: 'auto',
      area: 'auto'
    })
    expect(config).not.toHaveProperty('mode')
    expect(config).not.toHaveProperty('scene')
    expect(config).not.toHaveProperty('container')
    expect(instance.validate).toHaveBeenCalledOnce()

    await expect(verification).resolves.toBe(
      JSON.stringify({
        token: '1700000000.token-id.signature',
        knock: 'knock-1',
        dfu: 'dfu-1',
        ip: '203.0.113.8'
      })
    )
    expect(wrapper.emitted('verify')).toEqual([
      [
        JSON.stringify({
          token: '1700000000.token-id.signature',
          knock: 'knock-1',
          dfu: 'dfu-1',
          ip: '203.0.113.8'
        })
      ]
    ])

    wrapper.unmount()
  })

  it('用户关闭弹框时返回 null，不继续业务请求', async () => {
    validationResult = null
    const wrapper = mountWidget()
    await expect(wrapper.vm.verify()).resolves.toBeNull()
    wrapper.unmount()
  })

  it('并发调用只打开一次验证弹框', async () => {
    const wrapper = mountWidget()
    const first = wrapper.vm.verify()
    const second = wrapper.vm.verify()
    await expect(first).resolves.toContain('token-id')
    await expect(second).resolves.toContain('token-id')
    expect(window.vaptcha).toHaveBeenCalledOnce()
    expect(instance.validate).toHaveBeenCalledOnce()

    wrapper.unmount()
  })

  it('连续验证会复用同一个 V4 实例', async () => {
    const wrapper = mountWidget()

    await expect(wrapper.vm.verify()).resolves.toContain('token-id')
    wrapper.vm.reset()
    await expect(wrapper.vm.verify()).resolves.toContain('token-id')
    wrapper.vm.reset()

    expect(window.vaptcha).toHaveBeenCalledOnce()
    expect(instance.validate).toHaveBeenCalledTimes(2)
    expect(instance.reset).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('重置时关闭当前验证并结束等待', async () => {
    let resolveValidation: (result: typeof validationResult) => void = () => {}
    instance.validate.mockImplementationOnce(
      () => new Promise((resolve) => {
        resolveValidation = resolve
      })
    )
    const wrapper = mountWidget()
    const verification = wrapper.vm.verify()
    await flushPromises()

    wrapper.vm.reset()
    resolveValidation(validationResult)

    await expect(verification).resolves.toBeNull()
    expect(instance.reset).toHaveBeenCalled()
    wrapper.unmount()
  })
})
