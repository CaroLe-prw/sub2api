import type { MonitorCoverage, MonitorMatrixRow, MonitorMetric } from '@/api/channelMonitorV2'
import { formatMonitorPercent } from './monitorFormat'

/** Prefer the backend's combined traffic/probe availability; no samples means unknown. */
export function monitorAvailability(metric: MonitorMetric): string {
  const rate = metric.availability_rate ?? (metric.request_count > 0 ? 1 - metric.error_rate : null)
  return rate == null || !Number.isFinite(rate) ? '-' : formatMonitorPercent(rate)
}

export function monitorCacheRate(metric: MonitorMetric): string {
  // Public responses deliberately zero absolute counters, even with real traffic.
  const hasTraffic = metric.request_count > 0 || metric.availability_source === 'traffic' ||
    metric.availability_source === 'mixed' || metric.cache_rate > 0 ||
    metric.ttft.p50_ms != null || metric.duration.p50_ms != null
  return hasTraffic ? formatMonitorPercent(metric.cache_rate) : '-'
}

export function monitorCardSlots(coverage: MonitorCoverage, row: MonitorMatrixRow) {
  const step = Math.max(60, coverage.bucket_seconds || 300) * 1000
  const start = Date.parse(coverage.requested_start)
  const end = Date.parse(coverage.requested_end || coverage.data_through)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return []
  const buckets = new Map(row.buckets.map(bucket => [Date.parse(bucket.bucket_start), bucket]))
  const slots = []
  // Keep the whole requested window, including gaps and incomplete backfill.
  for (let time = Math.floor(start / step) * step; time < end; time += step) {
    slots.push({ time, bucket: buckets.get(time) })
  }
  return slots
}

export function monitorPlatformName(platform: string): string {
  const names: Record<string, string> = {
    openai: 'OpenAI', anthropic: 'Anthropic', claude: 'Claude', grok: 'Grok',
    gemini: 'Gemini', antigravity: 'Antigravity', deepseek: 'DeepSeek',
    qwen: 'Qwen', kimi: 'Kimi', doubao: 'Doubao',
  }
  return names[platform] || platform
}
