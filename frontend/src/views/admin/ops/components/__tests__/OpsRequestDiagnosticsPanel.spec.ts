import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import OpsRequestDiagnosticsPanel from '../OpsRequestDiagnosticsPanel.vue'

const copy = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: copy }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('OpsRequestDiagnosticsPanel', () => {
  it('separates original and transformed requests and the matching upstream response', async () => {
    const client = { body: '{"input":[{"content":{"type":"input_text","text":"hello"}}]}', bytes: 70 }
    const request = { body: '{"input":[{"content":[{"type":"input_text","text":"hello"}]}]}', bytes: 72 }
    const response = { body: '{"error":{"code":"invalid_type"}}', bytes: 33, truncated: true }
    const wrapper = mount(OpsRequestDiagnosticsPanel, { props: { diagnostics: JSON.stringify({ client_request: client, upstream_attempts: [{ account_id: 42, status_code: 400, request_id: 'rid-test', request, response }], dropped_attempts: 1 }) } })
    expect(wrapper.get('[data-testid="client-request"]').text()).toContain('"content": {')
    expect(wrapper.get('[data-testid="upstream-request-0"]').text()).toContain('"content": [')
    expect(wrapper.get('[data-testid="upstream-response-0"]').text()).toContain('invalid_type')
    expect(wrapper.text()).toContain('rid-test')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.records.truncated')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.records.dropped')
    await wrapper.get('[data-testid="client-request"] button').trigger('click')
    expect(copy).toHaveBeenCalledWith(JSON.stringify(JSON.parse(client.body), null, 2))
  })

  it.each([undefined, '', 'invalid JSON', '{}', 'null'])('explains unavailable historical snapshots: %s', (diagnostics) => {
    const wrapper = mount(OpsRequestDiagnosticsPanel, { props: { diagnostics } })
    expect(wrapper.text()).toContain('admin.ops.errorDetail.records.notCollected')
    expect(wrapper.findAll('pre')).toHaveLength(0)
  })

  it('renders omission reasons and treats payloads as text', () => {
    const wrapper = mount(OpsRequestDiagnosticsPanel, { props: { diagnostics: JSON.stringify({ client_request: { body: '<script>bad()</script>', bytes: 22 }, upstream_attempts: [{ account_id: 1, status_code: 502, request: { omitted_reason: 'too_large', truncated: true }, response: { omitted_reason: 'not_captured' } }] }) } })
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>bad()</script>')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.records.omitted.too_large')
    expect(wrapper.text()).toContain('admin.ops.errorDetail.records.omitted.not_captured')
  })
})
