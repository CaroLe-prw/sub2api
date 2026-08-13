import type { PoolProbeHeartbeat } from '@/api/admin/channelMonitor'

export interface HeartbeatSource {
  id: string
  samples: PoolProbeHeartbeat[]
}

export interface AggregateHeartbeatBucket {
  startedAt: string
  finishedAt: string
  status: 'success' | 'degraded' | 'failed' | 'partial'
  observedCount: number
  expectedCount: number
  healthyCount: number
  failedCount: number
  slowestTtftMs: number | null
  slowestTotalDurationMs: number | null
}

export interface AggregateHeartbeatOptions {
  bucketMinutes?: number
  limit?: number
  nowMs?: number
}

export function aggregateHeartbeatBuckets(
  sources: HeartbeatSource[],
  options: AggregateHeartbeatOptions = {},
): AggregateHeartbeatBucket[] {
  const bucketMinutes = Math.max(1, options.bucketMinutes ?? 5)
  const limit = Math.max(1, options.limit ?? 12)
  const nowMs = options.nowMs ?? Date.now()
  const bucketDurationMs = bucketMinutes * 60_000
  const expectedSourceIds = new Set(sources.map((source) => source.id))
  const buckets = new Map<number, {
    sourceIds: Set<string>
    failedSourceIds: Set<string>
    slowestTtftMs: number | null
    slowestTotalDurationMs: number | null
  }>()

  for (const source of sources) {
    for (const sample of source.samples) {
      const timestamp = Date.parse(sample.finished_at)
      if (Number.isNaN(timestamp)) continue

      const bucketStart = Math.floor(timestamp / bucketDurationMs) * bucketDurationMs
      const bucket = buckets.get(bucketStart) ?? {
        sourceIds: new Set<string>(),
        failedSourceIds: new Set<string>(),
        slowestTtftMs: null,
        slowestTotalDurationMs: null,
      }
      bucket.sourceIds.add(source.id)
      if (sample.status === 'failed') bucket.failedSourceIds.add(source.id)
      if (sample.ttft_ms != null) {
        bucket.slowestTtftMs = Math.max(bucket.slowestTtftMs ?? 0, sample.ttft_ms)
      }
      bucket.slowestTotalDurationMs = Math.max(bucket.slowestTotalDurationMs ?? 0, sample.latency_ms)
      buckets.set(bucketStart, bucket)
    }
  }

  const latestBucketStart = Math.floor(nowMs / bucketDurationMs) * bucketDurationMs
  const firstBucketStart = latestBucketStart - (limit - 1) * bucketDurationMs
  return Array.from({ length: limit }, (_, index) => {
    const bucketStart = firstBucketStart + index * bucketDurationMs
    const bucket = buckets.get(bucketStart)
    const observedCount = bucket
      ? [...bucket.sourceIds].filter((id) => expectedSourceIds.has(id)).length
      : 0
    const failedCount = bucket
      ? [...bucket.failedSourceIds].filter((id) => expectedSourceIds.has(id)).length
      : 0
    const healthyCount = observedCount - failedCount
    const hasCompleteCoverage = observedCount === expectedSourceIds.size && expectedSourceIds.size > 0
    const status = hasCompleteCoverage && failedCount === expectedSourceIds.size
      ? 'failed'
      : failedCount > 0
        ? 'degraded'
        : hasCompleteCoverage
          ? 'success'
          : 'partial'
    return {
      startedAt: new Date(bucketStart).toISOString(),
      finishedAt: new Date(bucketStart + bucketDurationMs).toISOString(),
      status,
      observedCount,
      expectedCount: expectedSourceIds.size,
      healthyCount,
      failedCount,
      slowestTtftMs: bucket?.slowestTtftMs ?? null,
      slowestTotalDurationMs: bucket?.slowestTotalDurationMs ?? null,
    }
  })
}
