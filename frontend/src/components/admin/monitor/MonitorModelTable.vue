<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatRelativeTime } from '@/utils/format'
import type { MonitorModelRow } from './monitorDataTypes'

defineProps<{ rows: MonitorModelRow[] }>()
const emit = defineEmits<{ detail: [row: MonitorModelRow] }>()
const { t } = useI18n()

function probeClass(status: string): string {
  if (status === 'success') return 'badge badge-success'
  if (status === 'failed') return 'badge badge-danger'
  return 'badge badge-gray'
}
</script>

<template>
  <div class="overflow-x-auto">
    <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
      <thead class="bg-gray-50/80 dark:bg-dark-800/60">
        <tr class="text-left text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
          <th class="px-4 py-3">{{ t('admin.channelMonitor.dataPanel.columns.model') }}</th>
          <th class="px-4 py-3">{{ t('admin.channelMonitor.dataPanel.columns.channel') }}</th>
          <th class="px-4 py-3">{{ t('admin.channelMonitor.dataPanel.columns.status') }}</th>
          <th class="px-4 py-3 text-right">{{ t('admin.channelMonitor.dataPanel.columns.latency') }}</th>
          <th class="px-4 py-3 text-right">{{ t('admin.channelMonitor.dataPanel.columns.availability') }}</th>
          <th class="px-4 py-3">{{ t('admin.channelMonitor.dataPanel.columns.checkedAt') }}</th>
          <th class="px-4 py-3 text-right">{{ t('admin.channelMonitor.dataPanel.columns.action') }}</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-800">
        <tr v-for="row in rows" :key="row.probe.plan_id" class="hover:bg-gray-50/70 dark:hover:bg-dark-700/40">
          <td class="px-4 py-3 font-mono text-xs font-semibold text-gray-900 dark:text-white">{{ row.probe.model }}</td>
          <td class="px-4 py-3">
            <div class="font-medium text-gray-800 dark:text-gray-100">{{ row.account.name }}</div>
            <div class="text-[11px] text-gray-400">#{{ row.account.account_id }} · {{ row.account.platform }} · {{ row.account.type }}</div>
          </td>
          <td class="px-4 py-3"><span :class="probeClass(row.probe.status)">{{ t(`admin.channelMonitor.dataPanel.probeStatus.${row.probe.status || 'pending'}`) }}</span></td>
          <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-200">{{ row.probe.latency_ms == null ? '-' : `${row.probe.latency_ms} ms` }}</td>
          <td class="px-4 py-3 text-right text-sm font-medium tabular-nums text-gray-800 dark:text-gray-100">{{ row.probe.availability == null ? '-' : `${row.probe.availability.toFixed(2)}%` }}</td>
          <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-500 dark:text-gray-400">{{ formatRelativeTime(row.probe.last_checked_at) }}</td>
          <td class="px-4 py-3 text-right">
            <button type="button" class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-500/10" @click="emit('detail', row)">
              <Icon name="chart" size="xs" />{{ t('admin.channelMonitor.dataPanel.detail') }}
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
