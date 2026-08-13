<script setup lang="ts">
import { computed, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolMonitorAccount } from '@/api/admin/channelMonitor'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatRelativeTime } from '@/utils/format'
import type { MonitorModelRow } from './monitorDataTypes'

interface MonitorModelGroup {
  model: string
  rows: MonitorModelRow[]
}

const props = defineProps<{ accounts: PoolMonitorAccount[] }>()
const emit = defineEmits<{ detail: [account: PoolMonitorAccount] }>()
const { t } = useI18n()
const groupPage = shallowRef(1)
const expandedModel = shallowRef<string | null>(null)
const channelPages = ref<Record<string, number>>({})
const groupPageSize = 6
const channelPageSize = 10

const groups = computed<MonitorModelGroup[]>(() => {
  const byModel = new Map<string, MonitorModelRow[]>()
  for (const account of props.accounts) {
    for (const probe of account.models) {
      const rows = byModel.get(probe.model) ?? []
      rows.push({ account, probe })
      byModel.set(probe.model, rows)
    }
  }

  return [...byModel.entries()]
    .map(([model, rows]) => ({
      model,
      rows: [...rows].sort((a, b) => {
        const statusOrder = { failed: 0, '': 1, success: 2 }
        return statusOrder[a.probe.status] - statusOrder[b.probe.status]
          || a.account.name.localeCompare(b.account.name)
          || a.account.account_id - b.account.account_id
      }),
    }))
    .sort((a, b) => a.model.localeCompare(b.model))
})
const pagedGroups = computed(() => {
  const start = (groupPage.value - 1) * groupPageSize
  return groups.value.slice(start, start + groupPageSize)
})

watch(() => props.accounts, () => {
  groupPage.value = 1
  expandedModel.value = null
  channelPages.value = {}
})

watch(() => groups.value.length, (total) => {
  const lastPage = Math.max(1, Math.ceil(total / groupPageSize))
  if (groupPage.value > lastPage) groupPage.value = lastPage
  if (expandedModel.value && !groups.value.some((group) => group.model === expandedModel.value)) {
    expandedModel.value = null
  }
})

function probeClass(status: string): string {
  if (status === 'success') return 'badge badge-success'
  if (status === 'failed') return 'badge badge-danger'
  return 'badge badge-gray'
}

function healthyCount(group: MonitorModelGroup): number {
  return group.rows.filter((row) => row.probe.status === 'success').length
}

function toggleGroup(model: string) {
  expandedModel.value = expandedModel.value === model ? null : model
}

function channelPage(model: string): number {
  return channelPages.value[model] ?? 1
}

function setChannelPage(model: string, page: number) {
  channelPages.value = { ...channelPages.value, [model]: page }
}

function pagedRows(group: MonitorModelGroup): MonitorModelRow[] {
  const start = (channelPage(group.model) - 1) * channelPageSize
  return group.rows.slice(start, start + channelPageSize)
}
</script>

<template>
  <div class="space-y-4 p-4">
    <section
      v-for="group in pagedGroups"
      :key="group.model"
      class="overflow-hidden rounded-2xl border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"
      data-testid="model-group"
      :data-model="group.model"
    >
      <button
        type="button"
        class="flex w-full flex-wrap items-center justify-between gap-3 bg-gray-50/80 px-4 py-3 text-left transition hover:bg-gray-100/80 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 dark:bg-dark-900/30 dark:hover:bg-dark-700/50"
        :aria-expanded="expandedModel === group.model"
        @click="toggleGroup(group.model)"
      >
        <div class="flex min-w-0 items-center gap-2.5">
          <span class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-violet-100 text-violet-600 dark:bg-violet-500/15 dark:text-violet-300">
            <Icon name="cube" size="sm" />
          </span>
          <div class="min-w-0">
            <h3 class="truncate font-mono text-sm font-bold text-gray-900 dark:text-white">{{ group.model }}</h3>
            <p class="mt-0.5 text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.channelMonitor.dataPanel.modelGroupChannels', { n: group.rows.length }) }}</p>
          </div>
        </div>
        <div class="flex items-center gap-3">
          <span class="text-xs font-medium tabular-nums" :class="healthyCount(group) === group.rows.length ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'">
            {{ t('admin.channelMonitor.dataPanel.modelGroupHealthy', { healthy: healthyCount(group), total: group.rows.length }) }}
          </span>
          <Icon :name="expandedModel === group.model ? 'chevronUp' : 'chevronDown'" size="sm" class="text-gray-400" />
        </div>
      </button>

      <div v-if="expandedModel === group.model" class="overflow-x-auto border-t border-gray-100 dark:border-dark-700">
        <table class="min-w-full divide-y divide-gray-100 dark:divide-dark-700">
          <thead class="bg-white dark:bg-dark-800">
            <tr class="text-left text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
              <th class="px-4 py-2.5">{{ t('admin.channelMonitor.dataPanel.columns.channel') }}</th>
              <th class="px-4 py-2.5">{{ t('admin.channelMonitor.dataPanel.columns.status') }}</th>
              <th class="px-4 py-2.5 text-right">{{ t('admin.channelMonitor.dataPanel.columns.latency') }}</th>
              <th class="px-4 py-2.5 text-right">{{ t('admin.channelMonitor.dataPanel.columns.availability') }}</th>
              <th class="px-4 py-2.5">{{ t('admin.channelMonitor.dataPanel.columns.checkedAt') }}</th>
              <th class="px-4 py-2.5 text-right">{{ t('admin.channelMonitor.dataPanel.columns.action') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-800">
            <tr v-for="row in pagedRows(group)" :key="row.probe.plan_id" class="hover:bg-gray-50/70 dark:hover:bg-dark-700/40">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-800 dark:text-gray-100">{{ row.account.name }}</div>
                <div class="text-[11px] text-gray-400">#{{ row.account.account_id }} · {{ row.account.platform }} · {{ row.account.type }}</div>
              </td>
              <td class="px-4 py-3"><span :class="probeClass(row.probe.status)">{{ t(`admin.channelMonitor.dataPanel.probeStatus.${row.probe.status || 'pending'}`) }}</span></td>
              <td class="px-4 py-3 text-right text-sm tabular-nums text-gray-700 dark:text-gray-200">{{ row.probe.latency_ms == null ? '-' : `${row.probe.latency_ms} ms` }}</td>
              <td class="px-4 py-3 text-right text-sm font-medium tabular-nums text-gray-800 dark:text-gray-100">{{ row.probe.availability == null ? '-' : `${row.probe.availability.toFixed(2)}%` }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-xs text-gray-500 dark:text-gray-400">{{ formatRelativeTime(row.probe.last_checked_at) }}</td>
              <td class="px-4 py-3 text-right">
                <button type="button" class="inline-flex items-center gap-1 rounded-lg px-2 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-500/10" @click="emit('detail', row.account)">
                  <Icon name="chart" size="xs" />{{ t('admin.channelMonitor.dataPanel.detail') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <Pagination
          v-if="group.rows.length > channelPageSize"
          :total="group.rows.length"
          :page="channelPage(group.model)"
          :page-size="channelPageSize"
          :show-page-size-selector="false"
          @update:page="setChannelPage(group.model, $event)"
        />
      </div>
    </section>
    <Pagination v-if="groups.length > groupPageSize" :total="groups.length" :page="groupPage" :page-size="groupPageSize" :show-page-size-selector="false" @update:page="groupPage = $event" />
  </div>
</template>
