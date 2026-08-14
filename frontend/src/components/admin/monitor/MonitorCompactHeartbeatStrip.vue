<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import MonitorAggregateHeartbeatTooltip from './MonitorAggregateHeartbeatTooltip.vue'
import { aggregateHeartbeatBuckets } from './monitorHeartbeatAggregation'
import type { HeartbeatSource } from './monitorHeartbeatAggregation'

const props = withDefaults(defineProps<{
  sources: HeartbeatSource[]
  coverageUnit: 'model' | 'channel'
  limit?: number
}>(), {
  limit: 12,
})

const { t } = useI18n()
const buckets = computed(() => aggregateHeartbeatBuckets(props.sources, { limit: props.limit }))
</script>

<template>
  <div class="flex h-3 items-center gap-0.5 overflow-hidden" :aria-label="t('admin.channelMonitor.dataPanel.recentTrend')" data-testid="compact-heartbeat-strip">
    <MonitorAggregateHeartbeatTooltip v-for="bucket in buckets" :key="bucket.startedAt" :bucket="bucket" :coverage-unit="coverageUnit" />
  </div>
</template>
