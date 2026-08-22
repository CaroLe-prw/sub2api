<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolMonitorAccount, PoolMonitorModel, PoolProbeHeartbeat, PoolProbeResult } from '@/api/admin/schedulerProbes'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import MonitorHeartbeatTimeline from './MonitorHeartbeatTimeline.vue'
import { resolveCurrentProbe } from './monitorCurrentProbeState'
import { summarizeCombinedHealth } from './monitorCombinedHealth'
import type { CombinedMonitorHealthState } from './monitorCombinedHealth'
import type { ProbeHistoryByPlan } from './monitorDataTypes'

const props = defineProps<{
  show: boolean
  account: PoolMonitorAccount | null
  histories: ProbeHistoryByPlan
  loading: boolean
  runningPlanId: number | null
}>()

const emit = defineEmits<{ close: []; run: [planId: number] }>()
const { t } = useI18n()
const title = computed(() => props.account
  ? t('admin.channelMonitor.dataPanel.channelDetailTitle', { name: props.account.name })
  : t('admin.channelMonitor.dataPanel.detail'))

const allSamples = computed(() => Object.values(props.histories).flatMap((samples) =>
  Array.isArray(samples) ? samples : []))
const averageLatency = computed(() => {
  const successful = allSamples.value.filter((item) => item.status === 'success')
  return successful.length
    ? Math.round(successful.reduce((sum, item) => sum + item.latency_ms, 0) / successful.length)
    : null
})
const availability = computed(() => allSamples.value.length
  ? allSamples.value.filter((item) => item.status === 'success').length / allSamples.value.length * 100
  : null)
const failures = computed(() => allSamples.value.filter((item) => item.status !== 'success').length)
const userSuccesses = computed(() => props.account?.models.reduce(
  (total, model) => total + Math.max(0, model.user_traffic?.success_count ?? 0),
  0,
) ?? 0)
const userFailures = computed(() => props.account?.models.reduce(
  (total, model) => total + Math.max(0, model.user_traffic?.failure_count ?? 0),
  0,
) ?? 0)
const userSamples = computed(() => userSuccesses.value + userFailures.value)
const userWindowMinutes = computed(() => props.account?.models.find((model) => model.user_traffic)?.user_traffic?.window_minutes ?? 30)

function currentModelState(model: PoolMonitorModel): CombinedMonitorHealthState {
  return summarizeCombinedHealth(model, history(model.plan_id)).state
}

function currentModelLatency(model: PoolMonitorModel): number | null {
  return resolveCurrentProbe(model, history(model.plan_id)).latencyMs
}

const accountCombinedState = computed<CombinedMonitorHealthState>(() => {
  const states = props.account?.models.map(currentModelState) ?? []
  if (states.length > 0 && states.every((state) => state === 'failed')) return 'failed'
  if (states.some((state) => state === 'failed' || state === 'degraded')) return 'degraded'
  if (states.length > 0 && states.every((state) => state === 'success')) return 'success'
  return 'pending'
})

const accountCombinedLabel = computed(() => {
  return t(`admin.channelMonitor.dataPanel.combinedStatus.${accountCombinedState.value}`)
})

const combinedCurrentSuccesses = computed(() => props.account?.models.reduce(
  (total, model) => total + summarizeCombinedHealth(model, history(model.plan_id)).successCount,
  0,
) ?? 0)
const combinedCurrentFailures = computed(() => props.account?.models.reduce(
  (total, model) => total + summarizeCombinedHealth(model, history(model.plan_id)).failureCount,
  0,
) ?? 0)

function history(planId: number): PoolProbeResult[] {
  return props.histories[planId] ?? []
}

function userTrafficHistory(model: PoolMonitorModel): PoolProbeHeartbeat[] {
  return (model.user_traffic?.recent_events ?? []).map((event) => ({
    id: event.id,
    plan_id: model.plan_id,
    status: event.status,
    ttft_ms: event.ttft_ms,
    latency_ms: event.latency_ms ?? 0,
    started_at: event.created_at,
    finished_at: event.created_at,
    created_at: event.created_at,
    source: 'user',
  }))
}

</script>

<template>
  <BaseDialog :show="show" :title="title" width="extra-wide" @close="emit('close')">
    <div v-if="account" class="space-y-5">
      <section class="rounded-2xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/40">
        <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <div class="flex items-center gap-2">
              <span class="h-2.5 w-2.5 rounded-full" :class="accountCombinedState === 'failed' ? 'bg-red-500' : accountCombinedState === 'degraded' ? 'bg-amber-500' : accountCombinedState === 'success' ? 'bg-emerald-500' : 'bg-gray-300'" />
              <strong class="text-base text-gray-900 dark:text-white">{{ account.name }}</strong>
            </div>
            <div class="mt-1 text-xs text-gray-400">#{{ account.account_id }} · {{ account.platform }} · {{ account.type }}</div>
          </div>
          <span class="badge badge-primary">{{ t('admin.channelMonitor.dataPanel.combinedHealth') }}</span>
        </div>
        <div class="mt-4 grid grid-cols-2 gap-3 border-t border-gray-200 pt-4 dark:border-dark-700 sm:grid-cols-4">
          <div><div class="text-[11px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.combinedState') }}</div><div class="mt-1 text-sm font-bold" :class="accountCombinedState === 'failed' ? 'text-red-600' : accountCombinedState === 'degraded' ? 'text-amber-600' : accountCombinedState === 'success' ? 'text-emerald-600' : 'text-gray-500'">{{ accountCombinedLabel }}</div></div>
          <div><div class="text-[11px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.scheduling') }}</div><div class="mt-1 text-sm font-bold text-gray-800 dark:text-gray-100">{{ account.schedulable ? t('admin.channelMonitor.dataPanel.automatic') : t('admin.channelMonitor.dataPanel.disabled') }}</div></div>
          <div><div class="text-[11px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.detectedModels') }}</div><div class="mt-1 text-sm font-bold text-gray-800 dark:text-gray-100">{{ account.models.length }}</div></div>
          <div><div class="text-[11px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.concurrency') }}</div><div class="mt-1 text-sm font-bold text-gray-800 dark:text-gray-100">{{ account.concurrency }}</div></div>
        </div>
      </section>

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <div class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700"><div class="text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.combinedSummary') }}</div><div class="mt-2 text-2xl font-black" :class="accountCombinedState === 'failed' ? 'text-red-500' : accountCombinedState === 'degraded' ? 'text-amber-500' : accountCombinedState === 'success' ? 'text-emerald-500' : 'text-gray-400'">{{ accountCombinedLabel }}</div><div class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.resultBreakdown', { success: combinedCurrentSuccesses, failed: combinedCurrentFailures }) }}</div></div>
        <div class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700"><div class="text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.userTraffic', { minutes: userWindowMinutes }) }}</div><div class="mt-2 text-2xl font-black tabular-nums text-gray-900 dark:text-white">{{ userSamples }}</div><div class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.resultBreakdown', { success: userSuccesses, failed: userFailures }) }}</div></div>
        <div class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700"><div class="text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.activeProbeSummary') }}</div><div class="mt-2 text-2xl font-black tabular-nums text-gray-900 dark:text-white">{{ allSamples.length }}</div><div class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.probeBreakdown', { availability: availability == null ? '-' : `${availability.toFixed(2)}%`, failed: failures, latency: averageLatency == null ? '-' : `${averageLatency}ms` }) }}</div></div>
      </div>

      <section>
        <div class="mb-3 flex items-start justify-between gap-4">
          <div>
            <h3 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.channelMonitor.dataPanel.modelPerformance') }}</h3>
            <p class="mt-1 text-[11px] leading-4 text-gray-400">{{ t('admin.channelMonitor.dataPanel.historyScope') }}</p>
          </div>
          <span class="text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.modelCount', { n: account.models.length }) }}</span>
        </div>
        <div v-if="loading" class="flex min-h-40 items-center justify-center text-gray-400"><Icon name="refresh" size="lg" class="animate-spin" /></div>
        <div v-else class="divide-y divide-gray-100 overflow-hidden rounded-2xl border border-gray-200 dark:divide-dark-700 dark:border-dark-700">
          <article v-for="model in account.models" :key="model.plan_id" class="p-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="flex flex-wrap items-center gap-2"><span class="h-2 w-2 rounded-full" :class="currentModelState(model) === 'success' ? 'bg-emerald-500' : currentModelState(model) === 'degraded' ? 'bg-amber-500' : currentModelState(model) === 'failed' ? 'bg-red-500' : 'bg-gray-300'" /><strong class="font-mono text-sm text-gray-900 dark:text-white">{{ model.model }}</strong><span class="text-xs" :class="currentModelState(model) === 'success' ? 'text-emerald-500' : currentModelState(model) === 'degraded' ? 'text-amber-500' : currentModelState(model) === 'failed' ? 'text-red-500' : 'text-gray-400'">{{ t(`admin.channelMonitor.dataPanel.combinedStatus.${currentModelState(model)}`) }}</span><span class="text-[10px] text-gray-400">{{ t('admin.channelMonitor.dataPanel.modelSourceCounts', { users: (model.user_traffic?.success_count ?? 0) + (model.user_traffic?.failure_count ?? 0), probes: history(model.plan_id).length }) }}</span></div>
              <div class="flex items-center gap-4"><span class="text-xs tabular-nums text-gray-500">{{ currentModelLatency(model) == null ? '-' : `${currentModelLatency(model)}ms` }}</span><span class="text-xs font-semibold tabular-nums text-emerald-500">{{ model.availability == null ? '-' : `${model.availability.toFixed(1)}%` }}</span><button type="button" class="btn btn-secondary btn-sm" :disabled="runningPlanId != null" @click="emit('run', model.plan_id)"><Icon name="refresh" size="xs" :class="runningPlanId === model.plan_id ? 'animate-spin' : ''" />{{ t('admin.channelMonitor.runNow') }}</button></div>
            </div>
            <div class="mt-3 space-y-3">
              <section class="rounded-xl bg-gray-50/80 px-3 pb-3 pt-2.5 dark:bg-dark-900/40">
                <div class="flex flex-wrap items-center justify-between gap-2 text-[11px]">
                  <span class="font-semibold text-gray-600 dark:text-gray-300">{{ t('admin.channelMonitor.dataPanel.userTrafficTimeline', { minutes: model.user_traffic?.window_minutes ?? 30 }) }}</span>
                  <span class="text-gray-400">{{ t('admin.channelMonitor.dataPanel.resultBreakdown', { success: model.user_traffic?.success_count ?? 0, failed: model.user_traffic?.failure_count ?? 0 }) }}</span>
                </div>
                <MonitorHeartbeatTimeline :samples="userTrafficHistory(model)" :empty-label="t('admin.channelMonitor.dataPanel.noUserTraffic')" />
              </section>
              <section class="rounded-xl bg-gray-50/80 px-3 pb-3 pt-2.5 dark:bg-dark-900/40">
                <div class="flex flex-wrap items-center justify-between gap-2 text-[11px]">
                  <span class="font-semibold text-gray-600 dark:text-gray-300">{{ t('admin.channelMonitor.dataPanel.activeProbeTimeline') }}</span>
                  <span class="text-gray-400">{{ t('admin.channelMonitor.dataPanel.timelineSampleCount', { n: history(model.plan_id).length }) }}</span>
                </div>
                <MonitorHeartbeatTimeline :samples="history(model.plan_id)" />
              </section>
            </div>
          </article>
        </div>
      </section>
    </div>
    <template #footer><div class="flex justify-end"><button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button></div></template>
  </BaseDialog>
</template>
