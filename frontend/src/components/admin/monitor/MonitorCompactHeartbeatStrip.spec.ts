import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { PoolProbeHeartbeat } from '@/api/admin/channelMonitor'

import MonitorCompactHeartbeatStrip from './MonitorCompactHeartbeatStrip.vue'
import { aggregateHeartbeatBuckets } from './monitorHeartbeatAggregation'
import type { HeartbeatSource } from './monitorHeartbeatAggregation'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function sample(id: number, planId: number, finishedAt: string, status: 'success' | 'failed', ttftMs: number, latencyMs: number): PoolProbeHeartbeat {
  return {
    id,
    plan_id: planId,
    status,
    ttft_ms: ttftMs,
    latency_ms: latencyMs,
    started_at: finishedAt,
    finished_at: finishedAt,
    created_at: finishedAt,
  }
}

const sources: HeartbeatSource[] = [
  {
    id: 'model-a',
    samples: [
      sample(1, 81, '2026-08-13T08:01:00Z', 'success', 200, 400),
      sample(2, 81, '2026-08-13T08:06:00Z', 'success', 250, 500),
      sample(3, 81, '2026-08-13T08:11:00Z', 'success', 300, 600),
    ],
  },
  {
    id: 'model-b',
    samples: [
      sample(4, 82, '2026-08-13T08:02:00Z', 'success', 350, 800),
      sample(5, 82, '2026-08-13T08:12:00Z', 'failed', 400, 900),
    ],
  },
  {
    id: 'model-c',
    samples: [
      sample(6, 83, '2026-08-13T08:02:30Z', 'success', 320, 700),
      sample(7, 83, '2026-08-13T08:12:30Z', 'failed', 450, 950),
    ],
  },
]

describe('MonitorCompactHeartbeatStrip', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-08-13T08:14:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders green, gray, and orange for healthy, incomplete, and partially degraded cycles', () => {
    const wrapper = mount(MonitorCompactHeartbeatStrip, {
      props: { sources, coverageUnit: 'model', limit: 3 },
      global: {
        stubs: {
          MonitorAggregateHeartbeatTooltip: {
            props: ['bucket', 'coverageUnit'],
            template: '<button class="heartbeat" :data-status="bucket.status" :data-observed="bucket.observedCount" :data-expected="bucket.expectedCount" />',
          },
        },
      },
    })

    const heartbeats = wrapper.findAll('.heartbeat')
    expect(heartbeats.map((heartbeat) => heartbeat.attributes('data-status'))).toEqual(['success', 'partial', 'degraded'])
    expect(heartbeats[0].attributes('data-observed')).toBe('3')
    expect(heartbeats[0].attributes('data-expected')).toBe('3')
    expect(heartbeats[1].attributes('data-observed')).toBe('1')
  })

  it('renders red only when every expected source failed', () => {
    const failedSources = sources.map((source, sourceIndex) => ({
      id: source.id,
      samples: [sample(20 + sourceIndex, 90 + sourceIndex, '2026-08-13T08:12:00Z', 'failed', 500, 1000)],
    }))

    const buckets = aggregateHeartbeatBuckets(failedSources, {
      limit: 1,
      nowMs: Date.parse('2026-08-13T08:14:00Z'),
    })

    expect(buckets[0].status).toBe('failed')
    expect(buckets[0].healthyCount).toBe(0)
    expect(buckets[0].failedCount).toBe(3)
  })

  it('reports the slowest timing values inside each cycle', () => {
    const [firstBucket] = aggregateHeartbeatBuckets(sources, {
      limit: 3,
      nowMs: Date.parse('2026-08-13T08:14:00Z'),
    })

    expect(firstBucket.slowestTtftMs).toBe(350)
    expect(firstBucket.slowestTotalDurationMs).toBe(800)
  })
})
