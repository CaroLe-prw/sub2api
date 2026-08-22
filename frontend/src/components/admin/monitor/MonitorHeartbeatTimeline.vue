<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolProbeHeartbeat } from '@/api/admin/schedulerProbes'
import MonitorHeartbeatTooltip from './MonitorHeartbeatTooltip.vue'

const VISIBLE_SAMPLE_LIMIT = 60

const props = defineProps<{
  samples: PoolProbeHeartbeat[]
  emptyLabel?: string
}>()

const { t } = useI18n()

const visibleSamples = computed(() => [...props.samples]
  .sort((left, right) => Date.parse(left.created_at) - Date.parse(right.created_at))
  .slice(-VISIBLE_SAMPLE_LIMIT))

const firstSample = computed(() => visibleSamples.value[0] ?? null)
const lastSample = computed(() => visibleSamples.value.at(-1) ?? null)

function formatCompactRelativeTime(value: string): string {
  const timestamp = Date.parse(value)
  if (Number.isNaN(timestamp)) return '-'

  const elapsedSeconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000))
  if (elapsedSeconds < 60) return t('admin.channelMonitor.dataPanel.relativeTime.now')

  const elapsedMinutes = Math.floor(elapsedSeconds / 60)
  if (elapsedMinutes < 60) {
    return t('admin.channelMonitor.dataPanel.relativeTime.minutes', { n: elapsedMinutes })
  }

  const elapsedHours = Math.floor(elapsedMinutes / 60)
  if (elapsedHours < 24) {
    return t('admin.channelMonitor.dataPanel.relativeTime.hours', { n: elapsedHours })
  }

  return t('admin.channelMonitor.dataPanel.relativeTime.days', { n: Math.floor(elapsedHours / 24) })
}
</script>

<template>
  <div :aria-label="t('admin.channelMonitor.dataPanel.recentTrend')">
    <div v-if="visibleSamples.length" class="mt-3 flex h-5 items-end gap-1 overflow-hidden">
      <MonitorHeartbeatTooltip v-for="sample in visibleSamples" :key="sample.id" :sample="sample" />
    </div>
    <div v-else class="mt-3 flex h-5 items-end">
      <span class="text-xs text-gray-400">{{ emptyLabel || t('admin.channelMonitor.dataPanel.noHistory') }}</span>
    </div>

    <div v-if="firstSample && lastSample" class="mt-1.5 grid grid-cols-[1fr_auto_1fr] items-center gap-2 text-[10px] leading-4 text-gray-400 dark:text-gray-500">
      <span>{{ t('admin.channelMonitor.dataPanel.timelineStart', { time: formatCompactRelativeTime(firstSample.finished_at) }) }}</span>
      <span class="tabular-nums">{{ t('admin.channelMonitor.dataPanel.visibleSampleCount', { n: visibleSamples.length, limit: VISIBLE_SAMPLE_LIMIT }) }}</span>
      <span class="text-right">{{ t('admin.channelMonitor.dataPanel.timelineLast', { time: formatCompactRelativeTime(lastSample.finished_at) }) }}</span>
    </div>
  </div>
</template>
