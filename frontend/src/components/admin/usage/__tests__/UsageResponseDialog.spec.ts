import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UsageResponseDialog from '../UsageResponseDialog.vue'
const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/admin/usage', () => ({ getResponseDiagnostics: mocks.get }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { limit?: string }) => params?.limit ? `${key}: ${params.limit}` : key }) }))
const mountDialog = (usageId: number | null) => mount(UsageResponseDialog, {
  props: { usageId },
  global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /></div>' } } }
})
const record = (body: string) => ({
  summary: { upstream: 'unavailable', downstream: 'extra_content' },
  downstream: { body, bytes: body.length, status: 'extra_content', truncated: false, complete: true, json_documents: 1,
    issues: [{ kind: 'trailing_content', frame: 1, json: '{}', extra: '<script>alert(1)</script>' }] }
})
beforeEach(() => vi.clearAllMocks())
describe('UsageResponseDialog', () => {
  it('loads only when opened and never renders historical raw content', async () => {
    mocks.get.mockResolvedValue(record('{}<script>alert(1)</script>'))
    const wrapper = mountDialog(null)
    expect(mocks.get).not.toHaveBeenCalled()
    await wrapper.setProps({ usageId: 12 })
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith(12, expect.any(AbortSignal))
    expect(wrapper.text()).not.toContain('<script>alert(1)</script>')
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.find('pre').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.usage.response.status.extra_content')
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
      .mockResolvedValueOnce({ ...record('new response'), downstream: { ...record('new response').downstream, status: 'ok' } })
    const wrapper = mountDialog(1)
    const firstSignal = mocks.get.mock.calls[0]?.[1] as AbortSignal
    await wrapper.setProps({ usageId: 2 })
    await flushPromises()
    firstResolve(record('old response'))
    await flushPromises()
    expect(firstSignal.aborted).toBe(true)
    expect(wrapper.text()).toContain('admin.usage.response.status.ok')
    expect(wrapper.text()).not.toContain('admin.usage.response.status.extra_content')
    expect(wrapper.text()).not.toContain('old response')
    wrapper.unmount()
  })
  it('shows the truncation warning even when extra content was found', async () => {
    const value = record('{}tail')
    value.downstream.truncated = true
    mocks.get.mockResolvedValue(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.truncatedHelp')
    expect(wrapper.text()).toContain('admin.usage.response.status.extra_content')
    wrapper.unmount()
  })
  it('renders capture truncation as an amber notice while keeping genuine trailing characters red', async () => {
    const value = record('{}unexpected text')
    value.downstream.truncated = true
    value.downstream.issues = [
      { kind: 'trailing_content', frame: 4, json: '{}', extra: 'unexpected text' },
      { kind: 'capture_truncated', frame: 225, json: '', extra: '' }
    ]
    mocks.get.mockResolvedValue(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    const notices = wrapper.findAll('p').filter(p => p.text().includes('admin.usage.response.issue.'))
    expect(notices).toHaveLength(2)
    expect(notices[0]?.classes()).toContain('text-red-500')
    expect(notices[1]?.classes()).toContain('text-amber-600')
    expect(notices[1]?.classes()).not.toContain('text-red-500')
    expect(wrapper.text()).not.toContain('admin.usage.response.issue.invalid_json')
    wrapper.unmount()
  })
  it('shows an incomplete read separately from a size limit', async () => {
    const value = record('{}')
    value.downstream.complete = false
    value.downstream.status = 'incomplete'
    value.downstream.issues = []
    mocks.get.mockResolvedValue(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.partial')
    expect(wrapper.text()).not.toContain('admin.usage.response.truncatedHelp')
    wrapper.unmount()
  })

  it('shows a trailing colon as a neutral notice without a content alert', async () => {
    const value = record('{}:')
    value.downstream.status = 'tail_symbols'
    value.downstream.issues = [{ kind: 'trailing_symbols', frame: 105, json: '{}', extra: ':' }]
    mocks.get.mockResolvedValue(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.status.tail_symbols')
    expect(wrapper.find('.text-red-500').exists()).toBe(false)
    expect(wrapper.find('.border-red-200').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.usage.response.status.extra_content')
    expect(wrapper.text()).toContain('admin.usage.response.issue.trailing_symbols')
    wrapper.unmount()
  })

  it('hides historical incoming and upstream request bodies', async () => {
    const value = {
      ...record('{}'),
      incoming_request: { method: 'POST', content_type: 'application/json', body: '{"model":"client-model","api_key":"[REDACTED]"}', bytes: 100, complete: true, truncated: false, redacted: true },
      upstream_request: { method: 'POST', content_type: 'application/json', body: '{"model":"mapped-model"}', bytes: 80, complete: true, truncated: false, redacted: true }
    }
    mocks.get.mockResolvedValue(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.usage.response.incomingRequest')
    expect(wrapper.text()).not.toContain('admin.usage.response.upstreamRequest')
    expect(wrapper.text()).not.toContain('client-model')
    expect(wrapper.text()).not.toContain('mapped-model')
    expect(wrapper.text()).not.toContain('[REDACTED]')
    expect(wrapper.text()).toContain('admin.usage.response.downstream')
    wrapper.unmount()
  })
  it('renders metadata without body viewing controls', async () => {
    mocks.get.mockResolvedValue({
      bodies_omitted: true,
      summary: { upstream: 'unavailable', downstream: 'extra_content' },
      downstream: { status: 'extra_content', bytes: 1234, json_documents: 2, complete: true, truncated: false,
        issues: [{ kind: 'trailing_content', frame: 2 }] }
    })
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('1,234 bytes')
    expect(wrapper.text()).toContain('admin.usage.response.status.extra_content')
    expect(wrapper.text()).toContain('admin.usage.response.issue.trailing_content')
    expect(wrapper.find('details').exists()).toBe(false)
    expect(wrapper.find('pre').exists()).toBe(false)
    wrapper.unmount()
  })

  it('uses each capture limit and preserves the historical 256 KiB fallback', async () => {
    const value = record('{}')
    value.downstream.truncated = true
    mocks.get.mockResolvedValueOnce({ ...value, downstream: { ...value.downstream, limit_bytes: 1048576 } }).mockResolvedValueOnce(value)
    const wrapper = mountDialog(1)
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.truncatedHelp: 1 MiB')
    await wrapper.setProps({ usageId: 2 })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.usage.response.truncatedHelp: 256 KiB')
    wrapper.unmount()
  })

})
