import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import type { PoolProbeHeartbeat } from '@/api/admin/channelMonitor'

import MonitorCompactHeartbeatStrip from './MonitorCompactHeartbeatStrip.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function sample(id: number): PoolProbeHeartbeat {
  const finishedAt = new Date(Date.parse('2026-08-13T08:00:00Z') + id * 60_000).toISOString()
  return {
    id,
    plan_id: 81,
    status: id % 3 === 0 ? 'failed' : 'success',
    ttft_ms: 200,
    latency_ms: 400,
    started_at: finishedAt,
    finished_at: finishedAt,
    created_at: finishedAt,
  }
}

describe('MonitorCompactHeartbeatStrip', () => {
  it('keeps a compact latest-sample window for overview cards', () => {
    const wrapper = mount(MonitorCompactHeartbeatStrip, {
      props: { samples: Array.from({ length: 20 }, (_, index) => sample(index + 1)).reverse() },
      global: {
        stubs: {
          MonitorHeartbeatTooltip: {
            props: ['sample', 'compact'],
            template: '<button class="heartbeat" :data-id="sample.id" />',
          },
        },
      },
    })

    const heartbeats = wrapper.findAll('.heartbeat')
    expect(heartbeats).toHaveLength(18)
    expect(heartbeats[0].attributes('data-id')).toBe('3')
    expect(heartbeats.at(-1)?.attributes('data-id')).toBe('20')
  })
})
