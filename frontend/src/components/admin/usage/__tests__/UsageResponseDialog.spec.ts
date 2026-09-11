import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UsageResponseDialog from '../UsageResponseDialog.vue'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/admin/usage', () => ({ getResponseDiagnostics: mocks.get }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const mountDialog = (usageId: number | null) => mount(UsageResponseDialog, {
  props: { usageId },
  global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } }
})
const record = (body: string) => ({
  summary: { upstream: 'unavailable', downstream: 'extra' },
  downstream: { body, bytes: body.length, status: 'extra', truncated: false, complete: true, json_documents: 1,
    issues: [{ kind: 'trailing_content', frame: 1, json: '{}', extra: '<script>alert(1)</script>' }] }
})
beforeEach(() => vi.clearAllMocks())
describe('UsageResponseDialog', () => {
  it('loads only when opened and renders raw content as text', async () => {
    mocks.get.mockResolvedValue(record('{}<script>alert(1)</script>'))
    const wrapper = mountDialog(null)
    expect(mocks.get).not.toHaveBeenCalled()
    await wrapper.setProps({ usageId: 12 })
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith(12, expect.any(AbortSignal))
    expect(wrapper.text()).toContain('{}<script>alert(1)</script>')
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.usage.response.status.extra')
    wrapper.unmount()
  })
  it('shows unrecorded historical data separately from request failure', async () => {
    mocks.get.mockResolvedValueOnce(null).mockRejectedValueOnce(new Error('failed'))
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.unavailable')
    await wrapper.setProps({ usageId: 2 })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.loadError')
    expect(wrapper.text()).not.toContain('admin.usage.response.unavailable')
    wrapper.unmount()
  })
  it('cancels the previous request and ignores stale results', async () => {
    let firstResolve!: (value: unknown) => void
    mocks.get.mockImplementationOnce(() => new Promise(resolve => { firstResolve = resolve }))
      .mockResolvedValueOnce(record('new response'))
    const wrapper = mountDialog(1)
    const firstSignal = mocks.get.mock.calls[0]?.[1] as AbortSignal
    await wrapper.setProps({ usageId: 2 })
    await flushPromises()
    firstResolve(record('old response'))
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    expect(wrapper.text()).toContain('new response')
    expect(wrapper.text()).not.toContain('old response')
    wrapper.unmount()
  })
  it('shows the truncation warning even when extra content was found', async () => {
    const value = record('{}tail')
    value.downstream.truncated = true
    mocks.get.mockResolvedValue(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.partial')
    expect(wrapper.text()).toContain('admin.usage.response.status.extra')
    wrapper.unmount()
  })
})
