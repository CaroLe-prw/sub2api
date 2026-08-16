import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PoolProbeResult } from '@/api/admin/schedulerProbes'

import MonitorHeartbeatTimeline from './MonitorHeartbeatTimeline.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, number | string>) => {
        const messages: Record<string, string> = {
          'admin.channelMonitor.dataPanel.recentTrend': '近期探测趋势',
          'admin.channelMonitor.dataPanel.noHistory': '等待首次探测',
          'admin.channelMonitor.dataPanel.visibleSampleCount': '{n}/{limit} 条',
          'admin.channelMonitor.dataPanel.timelineStart': '起 {time}',
          'admin.channelMonitor.dataPanel.timelineLast': '上次 {time}',
          'admin.channelMonitor.dataPanel.relativeTime.now': '刚刚',
          'admin.channelMonitor.dataPanel.relativeTime.minutes': '{n}m前',
          'admin.channelMonitor.dataPanel.relativeTime.hours': '{n}h前',
          'admin.channelMonitor.dataPanel.relativeTime.days': '{n}d前',
        }
        return Object.entries(values ?? {}).reduce(
          (message, [name, value]) => message.replace(`{${name}}`, String(value)),
          messages[key] ?? key,
        )
      },
    }),
  }
})

function sample(id: number, finishedAt: string): PoolProbeResult {
  return {
    id,
    plan_id: 81,
    status: 'success',
    response_text: 'ok',
    error_message: '',
    ttft_ms: 200,
    latency_ms: 400,
    started_at: finishedAt,
    finished_at: finishedAt,
    created_at: finishedAt,
  }
}

describe('MonitorHeartbeatTimeline', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-13T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders only the latest 60 samples with compact range labels', () => {
    const start = Date.parse('2026-08-13T10:56:00Z')
    const samples = Array.from({ length: 65 }, (_, index) => (
      sample(index + 1, new Date(start + index * 60_000).toISOString())
    )).reverse()

    const wrapper = mount(MonitorHeartbeatTimeline, {
      props: { samples },
      global: {
        stubs: {
          MonitorHeartbeatTooltip: {
            props: ['sample'],
            template: '<button class="heartbeat" :data-sample-id="sample.id" />',
          },
        },
      },
    })

    const heartbeats = wrapper.findAll('.heartbeat')
    expect(heartbeats).toHaveLength(60)
    expect(heartbeats[0].attributes('data-sample-id')).toBe('6')
    expect(heartbeats.at(-1)?.attributes('data-sample-id')).toBe('65')
    expect(wrapper.text()).toContain('起 59m前')
    expect(wrapper.text()).toContain('60/60 条')
    expect(wrapper.text()).toContain('上次 刚刚')
  })

  it('orders heartbeats by persisted result time so the rightmost bar matches current status', () => {
    const olderPersistedFailure = {
      ...sample(1, '2026-08-13T11:59:58Z'),
      status: 'failed' as const,
      created_at: '2026-08-13T11:59:58Z',
    }
    const latestPersistedSuccess = {
      ...sample(2, '2026-08-13T11:59:57Z'),
      created_at: '2026-08-13T11:59:59Z',
    }

    const wrapper = mount(MonitorHeartbeatTimeline, {
      props: { samples: [latestPersistedSuccess, olderPersistedFailure] },
      global: {
        stubs: {
          MonitorHeartbeatTooltip: {
            props: ['sample'],
            template: '<button class="heartbeat" :data-sample-id="sample.id" />',
          },
        },
      },
    })

    const heartbeats = wrapper.findAll('.heartbeat')
    expect(heartbeats.map((item) => item.attributes('data-sample-id'))).toEqual(['1', '2'])
  })

  it('shows the empty state without range labels before the first probe', () => {
    const wrapper = mount(MonitorHeartbeatTimeline, { props: { samples: [] } })

    expect(wrapper.text()).toBe('等待首次探测')
    expect(wrapper.findAll('button')).toHaveLength(0)
  })
})
