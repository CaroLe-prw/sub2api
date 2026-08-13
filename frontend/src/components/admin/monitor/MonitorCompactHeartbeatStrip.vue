<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolProbeHeartbeat } from '@/api/admin/channelMonitor'
import MonitorHeartbeatTooltip from './MonitorHeartbeatTooltip.vue'

const props = withDefaults(defineProps<{
  samples: PoolProbeHeartbeat[]
  limit?: number
}>(), {
  limit: 18,
})

const { t } = useI18n()
const visibleSamples = computed(() => [...props.samples]
  .sort((left, right) => Date.parse(left.finished_at) - Date.parse(right.finished_at))
  .slice(-props.limit))
</script>

<template>
  <div class="flex h-3 items-center gap-0.5 overflow-hidden" :aria-label="t('admin.channelMonitor.dataPanel.recentTrend')" data-testid="compact-heartbeat-strip">
    <MonitorHeartbeatTooltip v-for="sample in visibleSamples" :key="`${sample.plan_id}-${sample.id}`" :sample="sample" compact />
    <template v-if="visibleSamples.length === 0">
      <span v-for="index in Math.min(limit, 12)" :key="index" class="h-1.5 min-w-1 flex-1 rounded-[2px] bg-gray-200 dark:bg-dark-600" />
    </template>
  </div>
</template>
