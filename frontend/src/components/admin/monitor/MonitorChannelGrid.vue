<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolMonitorAccount } from '@/api/admin/schedulerProbes'
import Icon from '@/components/icons/Icon.vue'
import { formatRelativeTime } from '@/utils/format'
import MonitorCompactHeartbeatStrip from './MonitorCompactHeartbeatStrip.vue'
import type { HeartbeatSource } from './monitorHeartbeatAggregation'
import { monitorAvailabilityTextClass } from './monitorAvailability'

const props = defineProps<{ accounts: PoolMonitorAccount[] }>()
const emit = defineEmits<{
  detail: [account: PoolMonitorAccount]
  manage: [account: PoolMonitorAccount]
}>()
const { t } = useI18n()

function state(account: PoolMonitorAccount): 'healthy' | 'issue' | 'pending' {
  if (account.models.some((model) => model.status === 'failed')) return 'issue'
  if (account.models.some((model) => model.status === 'success')) return 'healthy'
  return 'pending'
}

function avgLatency(account: PoolMonitorAccount): number | null {
  const values = account.models.flatMap((model) => model.latency_ms == null ? [] : [model.latency_ms])
  return values.length ? Math.round(values.reduce((sum, value) => sum + value, 0) / values.length) : null
}

function availability(account: PoolMonitorAccount): number | null {
  const samples = account.models.reduce((sum, model) => sum + model.sample_count, 0)
  if (!samples) return null
  const successes = account.models.reduce((sum, model) => sum + model.sample_count - model.failure_count, 0)
  return successes / samples * 100
}

function lastChecked(account: PoolMonitorAccount): string | null {
  const values = account.models.flatMap((model) => model.last_checked_at ? [model.last_checked_at] : [])
  return values.sort().at(-1) ?? null
}

function heartbeatSources(account: PoolMonitorAccount): HeartbeatSource[] {
  return account.models.map((model) => ({
    id: String(model.plan_id),
    samples: model.recent_results ?? [],
  }))
}

const sorted = computed(() => [...props.accounts].sort((a, b) => {
  const order = { issue: 0, pending: 1, healthy: 2 }
  return order[state(a)] - order[state(b)] || a.account_id - b.account_id
}))
</script>

<template>
  <div class="grid gap-3 p-4 md:grid-cols-2 xl:grid-cols-3">
    <article
      v-for="account in sorted"
      :key="account.account_id"
      class="rounded-2xl border border-gray-200 bg-white p-4 text-left transition hover:-translate-y-0.5 hover:border-primary-300 hover:shadow-md dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-700"
    >
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="h-2.5 w-2.5 shrink-0 rounded-full" :class="state(account) === 'healthy' ? 'bg-emerald-500' : state(account) === 'issue' ? 'bg-red-500' : 'bg-gray-300'" />
            <strong class="truncate text-sm text-gray-900 dark:text-white">{{ account.name }}</strong>
          </div>
          <div class="mt-1 text-[11px] text-gray-400">#{{ account.account_id }} · {{ account.platform }} · {{ account.type }}</div>
        </div>
        <span class="badge badge-primary">{{ t('admin.channelMonitor.dataPanel.streaming') }}</span>
      </div>
      <div class="mt-4 grid grid-cols-3 gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
        <div><div class="text-sm font-bold text-gray-800 dark:text-gray-100">{{ account.models.length }}</div><div class="text-[10px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.stats.models') }}</div></div>
        <div><div class="text-sm font-bold text-gray-800 dark:text-gray-100">{{ avgLatency(account) == null ? '-' : `${avgLatency(account)}ms` }}</div><div class="text-[10px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.avgLatency') }}</div></div>
        <div><div class="text-sm font-bold" :class="monitorAvailabilityTextClass(availability(account))">{{ availability(account) == null ? '-' : `${availability(account)!.toFixed(1)}%` }}</div><div class="text-[10px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.availability') }}</div></div>
      </div>
      <div class="mt-3 border-t border-gray-100 pt-3 dark:border-dark-700">
        <MonitorCompactHeartbeatStrip :sources="heartbeatSources(account)" coverage-unit="model" />
      </div>
      <div class="mt-3 flex items-center justify-between gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
        <span>{{ formatRelativeTime(lastChecked(account)) }}</span>
        <div class="flex items-center gap-2">
          <button type="button" class="btn btn-secondary btn-sm" @click="emit('manage', account)">
            <Icon name="cog" size="xs" />{{ t('admin.channelMonitor.dataPanel.manageModels') }}
          </button>
          <button type="button" class="btn btn-secondary btn-sm" @click="emit('detail', account)">
            {{ t('admin.channelMonitor.dataPanel.detail') }}<Icon name="chevronRight" size="xs" />
          </button>
        </div>
      </div>
    </article>
  </div>
</template>
