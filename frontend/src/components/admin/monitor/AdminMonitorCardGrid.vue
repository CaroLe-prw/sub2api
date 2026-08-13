<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import type { UserMonitorView } from '@/api/channelMonitor'
import EmptyState from '@/components/common/EmptyState.vue'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'

const props = defineProps<{
  items: ChannelMonitor[]
  loading: boolean
}>()

const { t } = useI18n()

const cards = computed(() => props.items.map((monitor): {
  monitor: ChannelMonitor
  item: UserMonitorView
} => ({
  monitor,
  item: {
    id: monitor.id,
    name: monitor.name,
    provider: monitor.provider,
    group_name: monitor.group_name,
    primary_model: monitor.primary_model,
    primary_status: monitor.primary_status,
    primary_latency_ms: monitor.primary_latency_ms,
    primary_ping_latency_ms: monitor.primary_ping_latency_ms ?? null,
    availability_7d: monitor.availability_7d,
    extra_models: monitor.extra_models_status,
    timeline: monitor.timeline ?? [],
  },
})))
</script>

<template>
  <div class="h-full overflow-y-auto p-4 sm:p-5">
    <div class="mb-4 rounded-xl border border-blue-200 bg-blue-50 px-4 py-3 text-xs text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-200">
      {{ t('admin.channelMonitor.cardPreview.securityHint') }}
    </div>

    <div v-if="loading && cards.length === 0" class="grid grid-cols-1 gap-5 md:grid-cols-2 2xl:grid-cols-3">
      <div v-for="index in 6" :key="index" class="h-[330px] animate-pulse rounded-2xl bg-gray-100 dark:bg-dark-700/50"></div>
    </div>

    <EmptyState
      v-else-if="cards.length === 0"
      :title="t('admin.channelMonitor.noMonitorsYet')"
      :description="t('admin.channelMonitor.createFirstMonitor')"
    />

    <div v-else class="grid grid-cols-1 gap-5 md:grid-cols-2 2xl:grid-cols-3">
      <article v-for="card in cards" :key="card.monitor.id" class="min-w-0">
        <div class="mb-2 flex items-center justify-between gap-2 px-1">
          <span :class="card.monitor.public_visible && card.monitor.enabled ? 'badge badge-success' : 'badge badge-gray'">
            {{ card.monitor.public_visible && card.monitor.enabled
              ? t('admin.channelMonitor.publish.public')
              : t('admin.channelMonitor.publish.private') }}
          </span>
          <span v-if="!card.monitor.enabled" class="badge badge-warning">
            {{ t('admin.channelMonitor.cardPreview.disabled') }}
          </span>
        </div>
        <MonitorCard
          :item="card.item"
          window="7d"
          :availability-value="card.item.primary_status ? card.item.availability_7d : null"
          :countdown-seconds="null"
          :interactive="false"
        />
      </article>
    </div>
  </div>
</template>
