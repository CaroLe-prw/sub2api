import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ModelPlazaView from '../ModelPlazaView.vue'
import ModelPlazaContent from '@/components/modelPlaza/ModelPlazaContent.vue'
import type { ModelPlazaResponse } from '@/api/modelPlaza'

const mocks = vi.hoisted(() => ({
  getModelPlaza: vi.fn(),
  fetchPublicSettings: vi.fn(),
  route: { query: {} as Record<string, string> },
  auth: { isAuthenticated: false }
}))
vi.mock('@/api/modelPlaza', () => ({ getModelPlaza: mocks.getModelPlaza }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ fetchPublicSettings: mocks.fetchPublicSettings }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
vi.mock('vue-router', () => ({ useRoute: () => mocks.route }))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))

const response: ModelPlazaResponse = { description: '', groups: [] }
function mountView() {
  return mount(ModelPlazaView, {
    global: { stubs: { ModelPlazaContent: true, PlazaNavBar: true } }
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.getModelPlaza.mockResolvedValue(response)
  mocks.fetchPublicSettings.mockResolvedValue(null)
  mocks.route.query = {}
  mocks.auth.isAuthenticated = false
})

describe('ModelPlazaView', () => {
  it('uses the shared background and loads the public catalog', async () => {
    let resolveRequest!: (value: ModelPlazaResponse) => void
    mocks.getModelPlaza.mockReturnValueOnce(new Promise<ModelPlazaResponse>((resolve) => { resolveRequest = resolve }))
    const wrapper = mountView()
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.bg-mesh-gradient').exists()).toBe(true)
    expect(wrapper.findComponent(ModelPlazaContent).props('loading')).toBe(true)
    resolveRequest(response)
    await flushPromises()
    expect(mocks.getModelPlaza).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(ModelPlazaContent).props()).toMatchObject({ response, loading: false, error: false })
    wrapper.unmount()
  })

  it('retries a failed load through the catalog refresh action', async () => {
    mocks.getModelPlaza.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()
    const content = wrapper.findComponent(ModelPlazaContent)
    expect(content.props('error')).toBe(true)
    content.vm.$emit('refresh')
    await flushPromises()
    expect(mocks.getModelPlaza).toHaveBeenCalledTimes(2)
    expect(content.props()).toMatchObject({ response, loading: false, error: false })
    wrapper.unmount()
  })

  it('ignores repeated refresh clicks while a load is pending', async () => {
    let resolveRequest!: (value: ModelPlazaResponse) => void
    mocks.getModelPlaza.mockReturnValueOnce(new Promise<ModelPlazaResponse>((resolve) => { resolveRequest = resolve }))
    const wrapper = mountView()
    wrapper.findComponent(ModelPlazaContent).vm.$emit('refresh')
    expect(mocks.getModelPlaza).toHaveBeenCalledTimes(1)
    resolveRequest(response)
    await flushPromises()
    expect(wrapper.findComponent(ModelPlazaContent).props('loading')).toBe(false)
    wrapper.unmount()
  })

  it('keeps authenticated embedded mode and anonymous standalone fallback', async () => {
    mocks.route.query = { embedded: '1' }
    mocks.auth.isAuthenticated = true
    const embedded = mountView()
    await flushPromises()
    expect(embedded.findComponent(ModelPlazaContent).props('embedded')).toBe(true)
    expect(embedded.find('plaza-nav-bar-stub').exists()).toBe(false)
    embedded.unmount()
    mocks.auth.isAuthenticated = false
    const standalone = mountView()
    await flushPromises()
    expect(standalone.findComponent(ModelPlazaContent).props('embedded')).toBe(false)
    expect(standalone.find('plaza-nav-bar-stub').exists()).toBe(true)
    standalone.unmount()
  })
})
