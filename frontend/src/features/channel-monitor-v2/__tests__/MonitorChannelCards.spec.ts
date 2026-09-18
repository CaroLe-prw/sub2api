import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import MonitorChannelCards from '../MonitorChannelCards.vue'
import { monitorAvailability, monitorCacheRate, monitorCardSlots } from '../monitorCards'
import zh from '@/i18n/locales/zh/channelMonitorV2'
import type { MonitorCoverage, MonitorMatrixRow, MonitorMetric } from '@/api/channelMonitorV2'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    locale: { value: 'zh' },
    t: (key: string, params: Record<string, unknown> = {}) => {
      const value = key.split('.').reduce<unknown>((node, part) => (node as Record<string, unknown>)?.[part], zh)
      return String(value || key).replace(/\{(\w+)\}/g, (_, name) => String(params[name] ?? ''))
    },
  }),
}))

const coverage: MonitorCoverage = {
  requested_start: '2026-09-18T08:00:00Z', requested_end: '2026-09-18T08:15:00Z',
  coverage_start: '2026-09-18T08:05:00Z', data_through: '2026-09-18T08:10:00Z',
  computed_at: '2026-09-18T08:10:00Z', aggregation_lag_seconds: 0,
  coverage_complete: false, bucket_seconds: 300,
}
const metrics: MonitorMetric = {
  request_count: 0, success_requests: 0, error_requests: 0, token_count: 0,
  rpm: 0, tpm: 0, error_rate: 0, cache_rate: 0, cache_rate_numerator: 0, cache_rate_denominator: 0,
  ttft: { p50_ms: null, p95_ms: null, avg_ms: null, sample_count: 0 },
  duration: { p50_ms: null, p95_ms: null, avg_ms: null, sample_count: 0 },
}
function row(platform = 'openai'): MonitorMatrixRow {
  return {
    platform, group_id: 1, group_name: 'Test channel', metrics,
    health: { overall: 'unknown', error_rate: 'unknown', ttft: 'unknown', minimum_sample: 10 }, buckets: [],
  }
}
function render(rows: MonitorMatrixRow[]) {
  return mount(MonitorChannelCards, {
    props: { rows, coverage, countdown: 299, refreshing: false },
  })
}
describe('channel status cards', () => {
  it('retains cache rates for public responses whose request counters are redacted', () => {
    expect(monitorCacheRate({ ...metrics, availability_source: 'traffic', cache_rate: 0.952 })).toBe('95.2%')
    expect(monitorCacheRate({ ...metrics, availability_source: 'mixed', cache_rate: 0 })).toBe('0.00%')
    expect(monitorCacheRate({ ...metrics, availability_source: 'probe' })).toBe('-')
  })
  it('keeps unsampled availability unknown and uses combined probe availability when present', () => {
    expect(monitorAvailability(metrics)).toBe('-')
    expect(monitorAvailability({ ...metrics, availability_rate: 0 })).toMatch(/^0[.,]00%$/)
    expect(monitorAvailability({ ...metrics, availability_rate: 1, availability_source: 'probe' })).toMatch(/^100[.,]0%$/)
    expect(monitorAvailability({ ...metrics, request_count: 10, error_rate: 0.2 })).toMatch(/^80[.,]0%$/)
  })
  it('preserves empty slots at both ends of a partially covered window', () => {
    const channel = row()
    channel.buckets = [{ bucket_start: '2026-09-18T08:05:00+00:00', metrics, health: channel.health }]
    const slots = monitorCardSlots(coverage, channel)
    expect(slots).toHaveLength(3)
    expect(slots[0].bucket).toBeUndefined()
    expect(slots[1].bucket).toBe(channel.buckets[0])
    expect(slots[2].bucket).toBeUndefined()
  })
  it('groups by provider, retains missing metrics, and supports tapping history without exposing counts', async () => {
    const channel = row()
    channel.metrics = { ...metrics, availability_rate: 1, availability_source: 'probe', probe_sample_count: 12345 }
    channel.health = { ...channel.health, overall: 'healthy', error_rate: 'healthy', score: 100 }
    channel.buckets = [{ bucket_start: '2026-09-18T08:05:00Z', metrics: channel.metrics, health: channel.health }]
    const wrapper = render([channel, { ...row(), group_id: 2 }, row('grok')])
    expect(wrapper.findAll('.platform-heading')).toHaveLength(2)
    expect(wrapper.findAll('.channel-card')).toHaveLength(3)
    expect(wrapper.findAll('.channel-metric dd').slice(0, 3).map(el => el.text())).toEqual(['-', '100.0%', '-'])
    expect(wrapper.text()).not.toContain('12345')
    expect(wrapper.findAll('.history-slot')[1].classes()).toContain('health-score10')
    await wrapper.findAll('.history-slot')[1].trigger('click')
    expect(wrapper.find('.history-detail').text()).toContain('账户探测')
    await wrapper.findAll('.history-slot')[1].trigger('click')
    expect(wrapper.find('.history-detail').exists()).toBe(false)
  })
})
