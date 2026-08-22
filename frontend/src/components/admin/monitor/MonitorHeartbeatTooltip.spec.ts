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
      'admin.channelMonitor.dataPanel.sourceUser': '真实用户调用',
      'admin.channelMonitor.dataPanel.sourceProbe': '主动探测',
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

  it('uses a shorter bar in compact overview strips', () => {
    const wrapper = mount(MonitorHeartbeatTooltip, { props: { sample, compact: true } })

    expect(wrapper.get('button').classes()).toContain('h-1.5')
    expect(wrapper.get('button').classes()).toContain('min-w-1')
  })

  it('uses the usage-record latency colors when a successful first token is slow', async () => {
    const wrapper = mount(MonitorHeartbeatTooltip, {
      props: { sample: { ...sample, ttft_ms: 10_000 } },
      attachTo: document.body,
    })

    expect(wrapper.get('button').classes()).toContain('bg-amber-400')

    await wrapper.get('button').trigger('mouseenter')

    const ttftValue = [...document.body.querySelectorAll('dd')]
      .find((element) => element.textContent === '10000ms')
    expect(ttftValue?.classList.contains('text-amber-600')).toBe(true)
  })

  it('renders user-call success as green and identifies its source', async () => {
    const wrapper = mount(MonitorHeartbeatTooltip, {
      props: { sample: { ...sample, id: 'usage:9', source: 'user', ttft_ms: 10_000 } },
      attachTo: document.body,
    })

    expect(wrapper.get('button').classes()).toContain('bg-emerald-500')
    expect(wrapper.get('button').classes()).not.toContain('bg-amber-400')

    await wrapper.get('button').trigger('mouseenter')

    expect(document.body.textContent).toContain('真实用户调用')
    expect(wrapper.get('button').attributes('aria-label')).toContain('真实用户调用')
  })
})
