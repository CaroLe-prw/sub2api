<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { PoolMonitorAccount, PoolMonitorModel } from '@/api/admin/schedulerProbes'
import MonitorCompactHeartbeatStrip from '@/components/admin/monitor/MonitorCompactHeartbeatStrip.vue'
import Icon from '@/components/icons/Icon.vue'
import type { HeartbeatSource } from '@/components/admin/monitor/monitorHeartbeatAggregation'
import { summarizeCombinedHealth } from '@/components/admin/monitor/monitorCombinedHealth'
import type { CombinedMonitorHealth } from '@/components/admin/monitor/monitorCombinedHealth'
import { hasActiveProbe, monitorModelKey } from '@/components/admin/monitor/monitorModelProbe'

const props = defineProps<{
  monitor: PoolMonitorAccount | null
  loading: boolean
}>()

const emit = defineEmits<{
  detail: [monitor: PoolMonitorAccount]
}>()

const { t } = useI18n()

function modelHealth(model: PoolMonitorModel): CombinedMonitorHealth {
  return summarizeCombinedHealth(model)
}

const totalModels = computed(() => props.monitor?.models.length ?? 0)
const availableModels = computed(() => props.monitor?.models.filter((model) => modelHealth(model).available).length ?? 0)
const hasFailure = computed(() => props.monitor?.models.some((model) => modelHealth(model).state === 'failed') ?? false)
const hasDegraded = computed(() => props.monitor?.models.some((model) => modelHealth(model).state === 'degraded') ?? false)
const state = computed<'healthy' | 'degraded' | 'partial' | 'failed' | 'pending' | 'unmonitored'>(() => {
  if (!props.monitor || totalModels.value === 0) return 'unmonitored'
  if (availableModels.value === totalModels.value) return hasDegraded.value ? 'degraded' : 'healthy'
  if (availableModels.value > 0) return 'partial'
  if (hasFailure.value) return 'failed'
  return 'pending'
})
const heartbeatSources = computed<HeartbeatSource[]>(() => props.monitor?.models.filter(hasActiveProbe).map((model) => ({
  id: monitorModelKey(model),
  samples: model.recent_results ?? [],
})) ?? [])
const userSampleCount = computed(() => props.monitor?.models.reduce(
  (total, model) => total + modelHealth(model).userSampleCount,
  0,
) ?? 0)
const probeSampleCount = computed(() => props.monitor?.models.reduce(
  (total, model) => total + modelHealth(model).probeSampleCount,
  0,
) ?? 0)
const stateLabel = computed(() => t(`admin.accounts.modelAvailability.states.${state.value}`))
const detailLabel = computed(() => props.monitor
  ? t('admin.accounts.modelAvailability.openDetail', {
    available: availableModels.value,
    total: totalModels.value,
  })
  : stateLabel.value)

function openDetail() {
  if (props.monitor) emit('detail', props.monitor)
}
</script>

<template>
  <div v-if="loading && !monitor" class="flex min-w-32 items-center gap-2 text-xs text-gray-400">
    <Icon name="refresh" size="xs" class="animate-spin" />
    <span>{{ t('common.loading') }}</span>
  </div>

  <button
    v-else-if="monitor"
    type="button"
    class="group min-w-36 text-left"
    :title="detailLabel"
    :aria-label="detailLabel"
    @click="openDetail"
  >
    <div class="flex items-center justify-between gap-2">
      <span
        class="font-mono text-sm font-semibold tabular-nums"
        :class="{
          'text-emerald-600 dark:text-emerald-400': state === 'healthy',
          'text-amber-600 dark:text-amber-400': state === 'degraded' || state === 'partial',
          'text-red-600 dark:text-red-400': state === 'failed',
          'text-gray-500 dark:text-gray-400': state === 'pending',
        }"
      >
        {{ availableModels }}/{{ totalModels }}
      </span>
      <span class="flex items-center gap-1 text-[10px] text-gray-400 transition-colors group-hover:text-primary-600 dark:group-hover:text-primary-400">
        {{ stateLabel }}
        <Icon name="chevronRight" size="xs" />
      </span>
    </div>
    <div class="mt-1.5">
      <MonitorCompactHeartbeatStrip :sources="heartbeatSources" coverage-unit="model" :limit="10" />
    </div>
    <div class="mt-1 text-[9px] leading-3 text-gray-400">
      {{ t('admin.accounts.modelAvailability.sourceCounts', { users: userSampleCount, probes: probeSampleCount }) }}
    </div>
  </button>

  <span v-else class="text-xs text-gray-400 dark:text-dark-500">
    {{ stateLabel }}
  </span>
</template>
