<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('admin.quotaReset.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.quotaReset.description') }}
          </p>
        </div>
        <button
          v-if="loaded"
          data-test="quota-reset-run-now"
          type="button"
          class="btn btn-primary inline-flex items-center gap-2"
          :disabled="runLoading || state?.status === 'running'"
          @click="showRunDialog = true"
        >
          <Icon
            :name="runLoading || state?.status === 'running' ? 'refresh' : 'play'"
            size="sm"
            :class="runLoading || state?.status === 'running' ? 'animate-spin' : ''"
          />
          {{ state?.status === 'running' ? t('admin.quotaReset.running') : t('admin.quotaReset.runNow') }}
        </button>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-16">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <div v-else-if="!loaded" class="card p-8 text-center">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.quotaReset.loadFailed') }}
        </p>
        <button type="button" class="btn btn-secondary mt-4" @click="loadPage">
          {{ t('common.retry') }}
        </button>
      </div>

      <template v-else>
        <section class="card">
          <div class="flex flex-col gap-3 border-b border-gray-100 px-6 py-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <div class="flex items-center gap-2">
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                  {{ t('admin.quotaReset.statusTitle') }}
                </h2>
                <span
                  class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium"
                  :class="statusBadgeClass"
                >
                  {{ statusLabel }}
                </span>
              </div>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.quotaReset.statusDescription') }}
              </p>
            </div>
            <button
              type="button"
              class="btn btn-secondary btn-sm inline-flex items-center gap-2"
              :disabled="statusLoading"
              @click="refreshState"
            >
              <Icon name="refresh" size="sm" :class="statusLoading ? 'animate-spin' : ''" />
              {{ t('admin.quotaReset.refresh') }}
            </button>
          </div>

          <div class="grid grid-cols-2 gap-3 p-6 lg:grid-cols-4">
            <div class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700/50">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.matchedCount') }}</p>
              <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ state?.matched_count ?? 0 }}</p>
            </div>
            <div class="rounded-lg bg-emerald-50 p-4 dark:bg-emerald-900/10">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.resetCount') }}</p>
              <p class="mt-2 text-2xl font-semibold text-emerald-700 dark:text-emerald-300">{{ state?.reset_count ?? 0 }}</p>
            </div>
            <div class="rounded-lg bg-amber-50 p-4 dark:bg-amber-900/10">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.skippedCount') }}</p>
              <p class="mt-2 text-2xl font-semibold text-amber-700 dark:text-amber-300">{{ state?.skipped_count ?? 0 }}</p>
            </div>
            <div class="rounded-lg bg-red-50 p-4 dark:bg-red-900/10">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.failedCount') }}</p>
              <p class="mt-2 text-2xl font-semibold text-red-700 dark:text-red-300">{{ state?.failed_count ?? 0 }}</p>
            </div>
          </div>

          <div class="grid grid-cols-1 gap-x-8 gap-y-4 border-t border-gray-100 px-6 py-5 text-sm dark:border-dark-700 md:grid-cols-2">
            <div>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.lastStartedAt') }}</p>
              <p class="mt-1 font-medium text-gray-800 dark:text-gray-200">{{ formatStatusTime(state?.last_started_at) }}</p>
            </div>
            <div>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.lastFinishedAt') }}</p>
              <p class="mt-1 font-medium text-gray-800 dark:text-gray-200">{{ formatStatusTime(state?.last_finished_at) }}</p>
            </div>
            <div>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.lastSuccessAt') }}</p>
              <p class="mt-1 font-medium text-gray-800 dark:text-gray-200">{{ formatStatusTime(state?.last_success_at) }}</p>
            </div>
            <div>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.nextRunAt') }}</p>
              <p class="mt-1 font-medium text-gray-800 dark:text-gray-200">{{ formatStatusTime(state?.next_run_at) }}</p>
            </div>
          </div>

          <div
            v-if="state?.last_error"
            class="border-t border-gray-100 bg-red-50 px-6 py-4 text-sm text-red-700 dark:border-dark-700 dark:bg-red-900/10 dark:text-red-300"
          >
            <span class="font-medium">{{ t('admin.quotaReset.lastError') }}：</span>
            {{ state.last_error }}
          </div>
        </section>

        <section class="card p-6">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('admin.quotaReset.configTitle') }}
            </h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.quotaReset.configDescription') }}
            </p>
          </div>

          <div class="mt-6 space-y-6">
            <label class="flex items-start gap-3">
              <input
                v-model="form.enabled"
                type="checkbox"
                class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
              />
              <span>
                <span class="block text-sm font-medium text-gray-800 dark:text-gray-100">
                  {{ t('admin.quotaReset.enabled') }}
                </span>
                <span class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
                  {{ t('admin.quotaReset.enabledHint') }}
                </span>
              </span>
            </label>

            <div class="max-w-sm">
              <label for="quota-reset-interval" class="input-label">
                {{ t('admin.quotaReset.intervalHours') }}
              </label>
              <input
                id="quota-reset-interval"
                v-model.number="form.interval_hours"
                type="number"
                min="1"
                step="1"
                class="input w-full"
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.quotaReset.intervalHint') }}
              </p>
            </div>

            <div>
              <GroupSelector
                v-model="form.group_ids"
                :groups="subscriptionGroups"
                searchable
              />
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.quotaReset.groupsHint') }}
              </p>
            </div>

            <QuotaResetPolicyFields v-model="policy" id-prefix="scheduled-reset" />
          </div>

          <div class="mt-6 flex justify-end">
            <button
              data-test="quota-reset-save"
              type="button"
              class="btn btn-primary"
              :disabled="saving"
              @click="saveConfig"
            >
              {{ saving ? t('admin.quotaReset.saving') : t('admin.quotaReset.save') }}
            </button>
          </div>
        </section>
      </template>

      <QuotaResetRunDialog
        :show="showRunDialog"
        :groups="subscriptionGroups"
        :loading="runLoading"
        @submit="runNow"
        @close="showRunDialog = false"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  QuotaResetPolicy,
  SubscriptionQuotaResetConfig,
  SubscriptionQuotaResetRunRequest,
  SubscriptionQuotaResetState,
  SubscriptionQuotaResetStatus
} from '@/api/admin/quotaReset'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import { formatDateTimeToMinute } from '@/utils/format'
import AppLayout from '@/components/layout/AppLayout.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import Icon from '@/components/icons/Icon.vue'
import QuotaResetPolicyFields from '@/components/admin/subscription/QuotaResetPolicyFields.vue'
import QuotaResetRunDialog from '@/components/admin/subscription/QuotaResetRunDialog.vue'

const { t } = useI18n()
const appStore = useAppStore()

const form = reactive<SubscriptionQuotaResetConfig>({
  enabled: false,
  interval_hours: 24,
  group_ids: [],
  daily: true,
  weekly: true,
  monthly: true,
  window_start_mode: 'current'
})
const state = ref<SubscriptionQuotaResetState | null>(null)
const subscriptionGroups = ref<AdminGroup[]>([])
const loading = ref(true)
const loaded = ref(false)
const saving = ref(false)
const statusLoading = ref(false)
const runLoading = ref(false)
const showRunDialog = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | undefined

const policy = computed<QuotaResetPolicy>({
  get: () => ({
    daily: form.daily,
    weekly: form.weekly,
    monthly: form.monthly,
    window_start_mode: form.window_start_mode
  }),
  set: (value) => Object.assign(form, value)
})

const statusLabel = computed(() =>
  t(`admin.quotaReset.status.${state.value?.status ?? 'idle'}`)
)

const statusBadgeClass = computed(() => {
  const classes: Record<SubscriptionQuotaResetStatus, string> = {
    idle: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
    running: 'bg-blue-100 text-blue-700 dark:bg-blue-900/20 dark:text-blue-300',
    success: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
    partial_failed: 'bg-amber-100 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300',
    failed: 'bg-red-100 text-red-700 dark:bg-red-900/20 dark:text-red-300'
  }
  return classes[state.value?.status ?? 'idle']
})

function assignConfig(config: SubscriptionQuotaResetConfig) {
  Object.assign(form, config, { group_ids: [...config.group_ids] })
}

function stopPolling() {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = undefined
  }
}

function schedulePoll() {
  stopPolling()
  if (state.value?.status === 'running') {
    pollTimer = setTimeout(pollState, 2000)
  }
}

async function pollState() {
  try {
    const response = await adminAPI.quotaReset.get()
    state.value = response.state
  } catch (error) {
    console.error('Failed to poll subscription quota reset state:', error)
  } finally {
    schedulePoll()
  }
}

function errorMessage(error: unknown, fallback: string) {
  const apiError = error as {
    response?: { data?: { detail?: string } }
    message?: string
  }
  return apiError.response?.data?.detail || apiError.message || fallback
}

function validationMessage(requireGroup = form.enabled): string | null {
  if (!Number.isInteger(form.interval_hours) || form.interval_hours < 1) {
    return t('admin.quotaReset.invalidInterval')
  }
  if (requireGroup && form.group_ids.length === 0) {
    return t('admin.quotaReset.selectGroup')
  }
  if (!form.daily && !form.weekly && !form.monthly) {
    return t('admin.quotaReset.selectWindow')
  }
  return null
}

async function loadPage() {
  loading.value = true
  try {
    const [response, groups] = await Promise.all([
      adminAPI.quotaReset.get(),
      adminAPI.groups.getAllIncludingInactive()
    ])
    assignConfig(response.config)
    state.value = response.state
    subscriptionGroups.value = groups.filter((group) => group.subscription_type === 'subscription')
    loaded.value = true
    schedulePoll()
  } catch (error) {
    loaded.value = false
    appStore.showError(errorMessage(error, t('admin.quotaReset.loadFailed')))
    console.error('Failed to load subscription quota reset config:', error)
  } finally {
    loading.value = false
  }
}

async function refreshState() {
  if (statusLoading.value) return
  statusLoading.value = true
  try {
    const response = await adminAPI.quotaReset.get()
    state.value = response.state
    schedulePoll()
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.quotaReset.refreshFailed')))
    console.error('Failed to refresh subscription quota reset state:', error)
  } finally {
    statusLoading.value = false
  }
}

async function persistConfig(notify = true, requireGroup = form.enabled): Promise<boolean> {
  const invalid = validationMessage(requireGroup)
  if (invalid) {
    appStore.showError(invalid)
    return false
  }

  saving.value = true
  try {
    const response = await adminAPI.quotaReset.update({
      ...form,
      group_ids: [...form.group_ids]
    })
    assignConfig(response.config)
    state.value = response.state
    if (notify) appStore.showSuccess(t('admin.quotaReset.configSaved'))
    schedulePoll()
    return true
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.quotaReset.saveFailed')))
    console.error('Failed to save subscription quota reset config:', error)
    return false
  } finally {
    saving.value = false
  }
}

function saveConfig() {
  void persistConfig()
}

async function runNow(request: SubscriptionQuotaResetRunRequest) {
  if (runLoading.value || state.value?.status === 'running') return
  runLoading.value = true
  try {
    if (state.value) {
      state.value = {
        ...state.value,
        status: 'running',
        last_started_at: new Date().toISOString(),
        last_finished_at: null,
        last_error: ''
      }
      schedulePoll()
    }
    const response = await adminAPI.quotaReset.run(request)
    state.value = response.state
    showRunDialog.value = false
    appStore.showSuccess(t('admin.quotaReset.runCompleted'))
    schedulePoll()
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.quotaReset.runFailed')))
    console.error('Failed to run subscription quota reset:', error)
  } finally {
    runLoading.value = false
  }
}

function formatStatusTime(value: string | null | undefined) {
  return value ? formatDateTimeToMinute(value) : t('admin.quotaReset.never')
}

onMounted(loadPage)
onBeforeUnmount(stopPolling)
</script>
