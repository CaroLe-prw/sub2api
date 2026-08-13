<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolAccountModelPolicy, PoolMonitorAccount, PoolProbeResult } from '@/api/admin/channelMonitor'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import Icon from '@/components/icons/Icon.vue'
import MonitorChannelGrid from './MonitorChannelGrid.vue'
import MonitorAccountWhitelistDialog from './MonitorAccountWhitelistDialog.vue'
import MonitorModelHistoryDialog from './MonitorModelHistoryDialog.vue'
import MonitorModelGroupList from './MonitorModelGroupList.vue'
import type { ProbeHistoryByPlan } from './monitorDataTypes'

const { t } = useI18n()
const appStore = useAppStore()
const view = ref<'channel' | 'model'>('channel')
const loading = ref(true)
const accounts = ref<PoolMonitorAccount[]>([])
const search = ref('')
const selectedAccount = ref<PoolMonitorAccount | null>(null)
const histories = ref<ProbeHistoryByPlan>({})
const historyLoading = ref(false)
const runningPlanId = ref<number | null>(null)
const policyAccount = ref<PoolMonitorAccount | null>(null)
const accountPolicy = ref<PoolAccountModelPolicy | null>(null)
const policyLoading = ref(false)
const policySaving = ref(false)

const filteredAccounts = computed(() => {
  const query = search.value.trim().toLowerCase()
  if (!query) return accounts.value
  return accounts.value.filter((account) =>
    `${account.name} ${account.account_id} ${account.platform} ${account.type} ${account.models.map((model) => model.model).join(' ')}`
      .toLowerCase().includes(query))
})

const modelCount = computed(() => new Set(accounts.value.flatMap((account) =>
  account.models.map((probe) => probe.model))).size)
const healthyAccounts = computed(() => accounts.value.filter((account) =>
  account.models.some((model) => model.status === 'success') && !account.models.some((model) => model.status === 'failed')).length)
const issueAccounts = computed(() => accounts.value.filter((account) => account.models.some((model) => model.status === 'failed')).length)

async function load() {
  loading.value = true
  try {
    const result = await adminAPI.channelMonitor.listPoolOverview()
    accounts.value = result.items ?? []
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.dataPanel.loadError')))
  } finally {
    loading.value = false
  }
}

async function openAccount(account: PoolMonitorAccount) {
  selectedAccount.value = account
  histories.value = {}
  historyLoading.value = true
  try {
    const entries = await Promise.all(account.models.map(async (model) => [
      model.plan_id,
      await adminAPI.channelMonitor.listPoolProbeResults(model.plan_id, 100),
    ] as const))
    if (selectedAccount.value?.account_id === account.account_id) {
      histories.value = Object.fromEntries(entries)
    }
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.dataPanel.historyError')))
  } finally {
    historyLoading.value = false
  }
}

async function runProbe(planId: number) {
  if (runningPlanId.value != null) return
  runningPlanId.value = planId
  try {
    const result = await adminAPI.channelMonitor.runPoolProbe(planId)
    const current = histories.value[planId] ?? []
    histories.value = { ...histories.value, [planId]: [result as PoolProbeResult, ...current] }
    await load()
    if (selectedAccount.value) {
      selectedAccount.value = accounts.value.find((account) => account.account_id === selectedAccount.value?.account_id) ?? selectedAccount.value
    }
    appStore.showSuccess(t('admin.channelMonitor.runSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.runFailed')))
  } finally {
    runningPlanId.value = null
  }
}

async function openAccountPolicy(account: PoolMonitorAccount) {
  policyAccount.value = account
  accountPolicy.value = null
  policyLoading.value = true
  try {
    accountPolicy.value = await adminAPI.channelMonitor.getPoolAccountModelPolicy(account.account_id)
  } catch (error: unknown) {
    policyAccount.value = null
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.dataPanel.accountWhitelist.loadError')))
  } finally {
    policyLoading.value = false
  }
}

async function saveAccountPolicy(whitelist: string[]) {
  if (!policyAccount.value || policySaving.value) return
  policySaving.value = true
  try {
    accountPolicy.value = await adminAPI.channelMonitor.updatePoolAccountModelPolicy(policyAccount.value.account_id, whitelist)
    appStore.showSuccess(t('admin.channelMonitor.dataPanel.accountWhitelist.saveSuccess'))
    policyAccount.value = null
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('admin.channelMonitor.dataPanel.accountWhitelist.saveError')))
  } finally {
    policySaving.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-800">
    <header class="border-b border-gray-100 p-4 dark:border-dark-700 sm:p-5">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div class="flex items-center gap-2">
          <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400"><Icon name="chart" size="sm" /></span>
          <div><h2 class="text-sm font-bold text-gray-900 dark:text-white">{{ t('admin.channelMonitor.dataPanel.title') }}</h2><p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelMonitor.dataPanel.description') }}</p></div>
        </div>
        <button type="button" class="btn btn-secondary btn-sm self-start" :disabled="loading" @click="load"><Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />{{ t('common.refresh') }}</button>
      </div>

      <div class="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4">
        <div class="rounded-xl bg-gray-50 px-3 py-2.5 dark:bg-dark-900/40"><div class="text-lg font-bold text-gray-900 dark:text-white">{{ accounts.length }}</div><div class="text-[11px] text-gray-500">{{ t('admin.channelMonitor.dataPanel.stats.channels') }}</div></div>
        <div class="rounded-xl bg-violet-50 px-3 py-2.5 dark:bg-violet-500/10"><div class="text-lg font-bold text-violet-700 dark:text-violet-300">{{ modelCount }}</div><div class="text-[11px] text-violet-600/80">{{ t('admin.channelMonitor.dataPanel.stats.models') }}</div></div>
        <div class="rounded-xl bg-emerald-50 px-3 py-2.5 dark:bg-emerald-500/10"><div class="text-lg font-bold text-emerald-700 dark:text-emerald-300">{{ healthyAccounts }}</div><div class="text-[11px] text-emerald-600/80">{{ t('admin.channelMonitor.dataPanel.stats.operational') }}</div></div>
        <div class="rounded-xl bg-red-50 px-3 py-2.5 dark:bg-red-500/10"><div class="text-lg font-bold text-red-700 dark:text-red-300">{{ issueAccounts }}</div><div class="text-[11px] text-red-600/80">{{ t('admin.channelMonitor.dataPanel.stats.issues') }}</div></div>
      </div>

      <div class="mt-4 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div class="tabs inline-flex w-full sm:w-auto" role="tablist">
          <button type="button" class="tab flex-1 sm:flex-none" :class="view === 'channel' ? 'tab-active' : ''" @click="view = 'channel'">{{ t('admin.channelMonitor.dataPanel.viewByChannel') }}</button>
          <button type="button" class="tab flex-1 sm:flex-none" :class="view === 'model' ? 'tab-active' : ''" @click="view = 'model'">{{ t('admin.channelMonitor.dataPanel.viewByModel') }}</button>
        </div>
        <input v-model="search" type="search" class="input sm:max-w-xs" :placeholder="t('admin.channelMonitor.dataPanel.searchPlaceholder')" />
      </div>
    </header>

    <div v-if="loading && accounts.length === 0" class="flex min-h-52 items-center justify-center text-gray-400"><Icon name="refresh" size="lg" class="animate-spin" /></div>
    <div v-else-if="filteredAccounts.length === 0" class="flex min-h-52 flex-col items-center justify-center px-6 text-center"><p class="text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('admin.channelMonitor.dataPanel.empty') }}</p><p class="mt-1 max-w-lg text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelMonitor.dataPanel.emptyHint') }}</p></div>
    <MonitorChannelGrid v-else-if="view === 'channel'" :accounts="filteredAccounts" @detail="openAccount" @manage="openAccountPolicy" />
    <MonitorModelGroupList v-else :accounts="filteredAccounts" @detail="openAccount" />

    <MonitorModelHistoryDialog :show="selectedAccount != null" :account="selectedAccount" :histories="histories" :loading="historyLoading" :running-plan-id="runningPlanId" @close="selectedAccount = null" @run="runProbe" />
    <MonitorAccountWhitelistDialog :show="policyAccount != null" :account="policyAccount" :policy="accountPolicy" :loading="policyLoading" :saving="policySaving" @close="policyAccount = null" @save="saveAccountPolicy" />
  </section>
</template>
