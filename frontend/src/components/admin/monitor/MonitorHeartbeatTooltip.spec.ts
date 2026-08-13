import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import MonitorHeartbeatTooltip from './MonitorHeartbeatTooltip.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => ({
      'admin.channelMonitor.dataPanel.probeStatus.success': '在线',
      'admin.channelMonitor.dataPanel.probeStatus.failed': '异常',
      'admin.channelMonitor.dataPanel.firstToken': '首 T',
      'admin.channelMonitor.dataPanel.totalDuration': '总时长',
      'admin.channelMonitor.dataPanel.unavailable': '暂无数据',
    })[key] ?? key }),
  }
})

const sample = {
  id: 1,
  plan_id: 81,
  status: 'success' as const,
  response_text: 'ok',
  error_message: '',
  ttft_ms: 347,
  latency_ms: 2425,
  started_at: '2026-08-13T08:00:00Z',
  finished_at: '2026-08-13T08:00:02Z',
  created_at: '2026-08-13T08:00:02Z',
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('MonitorHeartbeatTooltip', () => {
  it('shows first-token and total duration for a heartbeat on hover', async () => {
    const wrapper = mount(MonitorHeartbeatTooltip, { props: { sample }, attachTo: document.body })
    const heartbeat = wrapper.get('button')
    vi.spyOn(heartbeat.element, 'getBoundingClientRect').mockReturnValue({
      x: 100,
      y: 200,
      top: 200,
      right: 120,
      bottom: 212,
      left: 100,
      width: 20,
      height: 12,
      toJSON: () => ({}),
    })

    await heartbeat.trigger('mouseenter')

    expect(document.body.textContent).toContain('2026')
    expect(document.body.textContent).toContain('在线')
    expect(document.body.textContent).toContain('首 T')
    expect(document.body.textContent).toContain('347ms')
    expect(document.body.textContent).toContain('总时长')
    expect(document.body.textContent).toContain('2425ms')
    expect(heartbeat.attributes('aria-label')).toContain('首 T: 347ms')
  })

  it('marks TTFT unavailable for historical samples without the new metric', async () => {
    const wrapper = mount(MonitorHeartbeatTooltip, {
      props: { sample: { ...sample, ttft_ms: null } },
      attachTo: document.body,
    })

    await wrapper.get('button').trigger('focus')

    expect(document.body.textContent).toContain('暂无数据')
    expect(document.body.textContent).toContain('2425ms')
  })
})
