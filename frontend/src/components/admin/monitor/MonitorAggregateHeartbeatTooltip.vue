<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateTime } from '@/utils/format'
import type { AggregateHeartbeatBucket } from './monitorHeartbeatAggregation'

const props = defineProps<{
  bucket: AggregateHeartbeatBucket
  coverageUnit: 'model' | 'channel'
}>()

const { t } = useI18n()
const trigger = shallowRef<HTMLElement | null>(null)
const visible = shallowRef(false)
const placement = shallowRef<'top' | 'bottom'>('top')
const position = shallowRef({ top: '0px', left: '0px' })

const statusLabel = computed(() => t(`admin.channelMonitor.dataPanel.aggregateStatus.${props.bucket.status}`))
const resultBreakdownLabel = computed(() => t('admin.channelMonitor.dataPanel.aggregateBreakdown', {
  healthy: props.bucket.healthyCount,
  failed: props.bucket.failedCount,
}))
const coverageLabel = computed(() => t(
  `admin.channelMonitor.dataPanel.${props.coverageUnit === 'model' ? 'modelCoverage' : 'channelCoverage'}`,
  { observed: props.bucket.observedCount, expected: props.bucket.expectedCount },
))
const slowestTtftLabel = computed(() => props.bucket.slowestTtftMs == null
  ? t('admin.channelMonitor.dataPanel.unavailable')
  : `${props.bucket.slowestTtftMs}ms`)
const slowestTotalLabel = computed(() => props.bucket.slowestTotalDurationMs == null
  ? t('admin.channelMonitor.dataPanel.unavailable')
  : `${props.bucket.slowestTotalDurationMs}ms`)
const timeRangeLabel = computed(() => `${formatDateTime(props.bucket.startedAt)} – ${formatDateTime(props.bucket.finishedAt)}`)
const accessibleLabel = computed(() => [
  timeRangeLabel.value,
  statusLabel.value,
  coverageLabel.value,
  resultBreakdownLabel.value,
  `${t('admin.channelMonitor.dataPanel.slowestFirstToken')}: ${slowestTtftLabel.value}`,
  `${t('admin.channelMonitor.dataPanel.slowestTotalDuration')}: ${slowestTotalLabel.value}`,
].join(' · '))

function updatePosition() {
  if (!trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const placeBelow = rect.top < 150
  placement.value = placeBelow ? 'bottom' : 'top'
  position.value = {
    top: `${placeBelow ? rect.bottom + 8 : rect.top - 8}px`,
    left: `${Math.min(Math.max(rect.left + rect.width / 2, 150), window.innerWidth - 150)}px`,
  }
}

function openTooltip() {
  updatePosition()
  visible.value = true
}
</script>

<template>
  <button
    ref="trigger"
    type="button"
    class="h-1.5 min-w-1 flex-1 cursor-help appearance-none rounded-[2px] border-0 p-0 outline-none ring-offset-1 transition-[filter] hover:brightness-90 focus-visible:ring-2 focus-visible:ring-primary-500"
    :class="bucket.status === 'success' ? 'bg-emerald-400' : bucket.status === 'degraded' ? 'bg-amber-400' : bucket.status === 'failed' ? 'bg-red-500' : 'bg-gray-300 dark:bg-dark-500'"
    :aria-label="accessibleLabel"
    @mouseenter="openTooltip"
    @mouseleave="visible = false"
    @focus="openTooltip"
    @blur="visible = false"
  />

  <Teleport to="body">
    <div
      v-if="visible"
      role="tooltip"
      class="pointer-events-none fixed z-[99999] w-72 -translate-x-1/2 rounded-lg bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800"
      :class="placement === 'top' ? '-translate-y-full' : ''"
      :style="position"
    >
      <div class="mb-2 border-b border-white/10 pb-2">
        <div class="truncate text-[11px] text-gray-400">{{ timeRangeLabel }}</div>
        <div class="mt-1 flex items-center justify-between gap-3">
          <span :class="bucket.status === 'success' ? 'text-emerald-300' : bucket.status === 'degraded' ? 'text-amber-300' : bucket.status === 'failed' ? 'text-red-300' : 'text-gray-300'">{{ statusLabel }}</span>
          <span class="tabular-nums text-gray-300">{{ coverageLabel }}</span>
        </div>
        <div class="mt-1 text-[11px] tabular-nums text-gray-400">{{ resultBreakdownLabel }}</div>
      </div>
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 tabular-nums">
        <dt class="text-gray-400">{{ t('admin.channelMonitor.dataPanel.slowestFirstToken') }}</dt>
        <dd class="text-right font-semibold">{{ slowestTtftLabel }}</dd>
        <dt class="text-gray-400">{{ t('admin.channelMonitor.dataPanel.slowestTotalDuration') }}</dt>
        <dd class="text-right font-semibold">{{ slowestTotalLabel }}</dd>
      </dl>
    </div>
  </Teleport>
</template>
