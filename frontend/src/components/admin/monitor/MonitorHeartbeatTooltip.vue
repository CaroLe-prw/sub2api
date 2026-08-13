<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolProbeHeartbeat } from '@/api/admin/channelMonitor'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  sample: PoolProbeHeartbeat
  compact?: boolean
}>()

const { t } = useI18n()
const trigger = ref<HTMLElement | null>(null)
const visible = ref(false)
const placement = ref<'top' | 'bottom'>('top')
const position = ref({ top: '0px', left: '0px' })

const statusLabel = computed(() => t(`admin.channelMonitor.dataPanel.probeStatus.${props.sample.status}`))
const ttftLabel = computed(() => props.sample.ttft_ms == null
  ? t('admin.channelMonitor.dataPanel.unavailable')
  : `${props.sample.ttft_ms}ms`)
const totalDurationLabel = computed(() => `${props.sample.latency_ms}ms`)
const accessibleLabel = computed(() => [
  formatDateTime(props.sample.finished_at),
  statusLabel.value,
  `${t('admin.channelMonitor.dataPanel.firstToken')}: ${ttftLabel.value}`,
  `${t('admin.channelMonitor.dataPanel.totalDuration')}: ${totalDurationLabel.value}`,
].join(' · '))

function updatePosition() {
  if (!trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const placeBelow = rect.top < 110
  placement.value = placeBelow ? 'bottom' : 'top'
  position.value = {
    top: `${placeBelow ? rect.bottom + 8 : rect.top - 8}px`,
    left: `${Math.min(Math.max(rect.left + rect.width / 2, 120), window.innerWidth - 120)}px`,
  }
}

function openTooltip() {
  updatePosition()
  visible.value = true
}

function closeTooltip() {
  visible.value = false
}
</script>

<template>
  <button
    ref="trigger"
    type="button"
    class="flex-1 cursor-help appearance-none border-0 p-0 outline-none ring-offset-1 transition-[filter,transform] hover:brightness-90 focus-visible:ring-2 focus-visible:ring-primary-500"
    :class="[
      sample.status === 'success' ? 'bg-emerald-400' : 'bg-red-500',
      compact ? 'h-1.5 min-w-1 rounded-[2px]' : 'h-3 min-w-2 rounded-sm',
    ]"
    :aria-label="accessibleLabel"
    @mouseenter="openTooltip"
    @mouseleave="closeTooltip"
    @focus="openTooltip"
    @blur="closeTooltip"
  />

  <Teleport to="body">
    <div
      v-if="visible"
      role="tooltip"
      class="pointer-events-none fixed z-[99999] w-56 -translate-x-1/2 rounded-lg bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800"
      :class="placement === 'top' ? '-translate-y-full' : ''"
      :style="position"
    >
      <div class="mb-2 flex items-center justify-between gap-3 border-b border-white/10 pb-2">
        <span class="truncate text-gray-300">{{ formatDateTime(sample.finished_at) }}</span>
        <span :class="sample.status === 'success' ? 'text-emerald-300' : 'text-red-300'">{{ statusLabel }}</span>
      </div>
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 tabular-nums">
        <dt class="text-gray-400">{{ t('admin.channelMonitor.dataPanel.firstToken') }}</dt>
        <dd class="text-right font-semibold">{{ ttftLabel }}</dd>
        <dt class="text-gray-400">{{ t('admin.channelMonitor.dataPanel.totalDuration') }}</dt>
        <dd class="text-right font-semibold">{{ totalDurationLabel }}</dd>
      </dl>
    </div>
  </Teleport>
</template>
