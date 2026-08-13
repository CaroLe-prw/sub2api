import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

import MonitorAggregateHeartbeatTooltip from './MonitorAggregateHeartbeatTooltip.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, number>) => {
        const messages: Record<string, string> = {
          'admin.channelMonitor.dataPanel.aggregateStatus.success': '全部正常',
          'admin.channelMonitor.dataPanel.aggregateStatus.degraded': '部分异常',
          'admin.channelMonitor.dataPanel.aggregateBreakdown': '正常 {healthy} · 异常 {failed}',
          'admin.channelMonitor.dataPanel.modelCoverage': '覆盖 {observed}/{expected} 个模型',
          'admin.channelMonitor.dataPanel.slowestFirstToken': '最慢首 T',
          'admin.channelMonitor.dataPanel.slowestTotalDuration': '最慢总时长',
          'admin.channelMonitor.dataPanel.unavailable': '暂无数据',
        }
        return Object.entries(values ?? {}).reduce(
          (message, [name, value]) => message.replace(`{${name}}`, String(value)),
          messages[key] ?? key,
        )
      },
    }),
  }
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('MonitorAggregateHeartbeatTooltip', () => {
  it('explains aggregate coverage and slowest timings on hover', async () => {
    const wrapper = mount(MonitorAggregateHeartbeatTooltip, {
      props: {
        coverageUnit: 'model',
        bucket: {
          startedAt: '2026-08-13T08:00:00Z',
          finishedAt: '2026-08-13T08:05:00Z',
          status: 'degraded',
          observedCount: 3,
          expectedCount: 3,
          healthyCount: 2,
          failedCount: 1,
          slowestTtftMs: 420,
          slowestTotalDurationMs: 1380,
        },
      },
      attachTo: document.body,
    })

    await wrapper.get('button').trigger('mouseenter')

    expect(wrapper.get('button').classes()).toContain('bg-amber-400')
    expect(document.body.textContent).toContain('部分异常')
    expect(document.body.textContent).toContain('覆盖 3/3 个模型')
    expect(document.body.textContent).toContain('正常 2 · 异常 1')
    expect(document.body.textContent).toContain('最慢首 T')
    expect(document.body.textContent).toContain('420ms')
    expect(document.body.textContent).toContain('最慢总时长')
    expect(document.body.textContent).toContain('1380ms')
    expect(wrapper.get('button').attributes('aria-label')).toContain('覆盖 3/3 个模型')
  })
})
