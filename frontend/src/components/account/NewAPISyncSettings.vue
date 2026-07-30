<template>
  <section
    v-if="enabled"
    class="space-y-4"
    data-testid="newapi-sync-settings"
  >
    <div v-if="loading" class="flex h-20 items-center justify-center text-gray-400">
      <Icon name="refresh" size="sm" class="animate-spin" />
    </div>

    <template v-else>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div class="sm:col-span-2">
          <label class="input-label" for="newapi-base-url">
            {{ t('admin.accounts.newapiSync.baseUrl') }}
          </label>
          <input
            id="newapi-base-url"
            v-model.trim="form.newapi_base_url"
            type="url"
            class="input font-mono"
            placeholder="https://newapi.example.com"
            autocomplete="off"
          />
          <p class="input-hint">{{ t('admin.accounts.newapiSync.baseUrlHint') }}</p>
        </div>

        <div class="sm:col-span-2">
          <label class="input-label" for="newapi-user-id">
            {{ t('admin.accounts.newapiSync.userId') }}
          </label>
          <input
            id="newapi-user-id"
            v-model.number="form.newapi_user_id"
            type="number"
            min="1"
            step="1"
            class="input"
          />
        </div>

        <div class="sm:col-span-2">
          <label class="input-label" for="newapi-access-token">
            {{ t('admin.accounts.newapiSync.accessToken') }}
          </label>
          <input
            id="newapi-access-token"
            v-model="form.newapi_user_access_token"
            type="password"
            class="input font-mono"
            autocomplete="new-password"
            data-1p-ignore
            data-lpignore="true"
            data-bwignore="true"
            :placeholder="t('admin.accounts.newapiSync.secretPlaceholder')"
          />
          <p class="input-hint">{{ t('admin.accounts.newapiSync.secretHint') }}</p>
        </div>
      </div>

      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="saving || testing || syncing"
          data-testid="newapi-sync-test"
          @click="testConnection"
        >
          <Icon name="refresh" size="xs" class="mr-1.5" :class="{ 'animate-spin': testing }" />
          {{ t('admin.accounts.newapiSync.testConnection') }}
        </button>
        <button
          type="button"
          class="btn btn-primary btn-sm"
          :disabled="saving || testing || syncing"
          data-testid="newapi-sync-run"
          @click="syncNow"
        >
          <Icon name="refresh" size="xs" class="mr-1.5" :class="{ 'animate-spin': syncing }" />
          {{ t('admin.accounts.newapiSync.syncNow') }}
        </button>
      </div>

      <div
        v-if="config"
        class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800/50"
      >
        <div class="grid grid-cols-[minmax(7rem,auto)_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.status') }}</span>
          <span class="font-medium" :class="statusClass">{{ statusLabel }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.lastSyncAt') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ formatDate(config.newapi_last_sync_at) }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.userGroup') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ resolvedUserGroup || '-' }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.tokenGroup') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ resolvedTokenGroup || '-' }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.actualGroup') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ resolvedActualGroup || '-' }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.currentRatio') }}</span>
          <span class="font-mono text-gray-900 dark:text-white">{{ formatRatio(displayRatio) }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.ratioSource') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ ratioSourceLabel }}</span>

          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.newapiSync.crossGroupRetry') }}</span>
          <span class="text-gray-900 dark:text-white">
            {{ crossGroupRetry ? t('common.yes') : t('common.no') }}
          </span>
        </div>

        <p
          v-if="config.newapi_last_sync_error"
          class="mt-3 break-words border-t border-gray-200 pt-2 text-xs text-amber-700 dark:border-dark-600 dark:text-amber-300"
        >
          {{ errorLabel(config.newapi_last_sync_error) }}
        </p>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import type {
  NewAPIRatioSource,
  NewAPISyncConfig,
  NewAPISyncConfigUpdate,
  NewAPISyncResult
} from '@/types'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  accountId: number
  enabled: boolean
}>()
const emit = defineEmits<{ synced: [ratio: number] }>()
const { t, te } = useI18n()
const appStore = useAppStore()

const defaultForm = (): NewAPISyncConfigUpdate => ({
  newapi_sync_enabled: false,
  newapi_base_url: '',
  newapi_user_access_token: '',
  newapi_user_id: 0
})

const form = reactive<NewAPISyncConfigUpdate>(defaultForm())
const config = shallowRef<NewAPISyncConfig | null>(null)
const preview = shallowRef<NewAPISyncResult | null>(null)
const loading = shallowRef(false)
const saving = shallowRef(false)
const testing = shallowRef(false)
const syncing = shallowRef(false)
let pendingLoad: Promise<void> | null = null

const applyConfig = (next: NewAPISyncConfig) => {
  config.value = next
  Object.assign(form, {
    newapi_sync_enabled: props.enabled,
    newapi_base_url: next.newapi_base_url,
    newapi_user_access_token: next.newapi_user_access_token,
    newapi_user_id: next.newapi_user_id
  })
}

const load = async () => {
  loading.value = true
  preview.value = null
  try {
    applyConfig(await adminAPI.accounts.getNewAPISyncConfig(props.accountId))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.newapiSync.loadFailed')))
  } finally {
    loading.value = false
  }
}

const startLoad = () => {
  const request = load()
  pendingLoad = request
  void request.finally(() => {
    if (pendingLoad === request) pendingLoad = null
  })
  return request
}

const configDirty = computed(() => {
  const current = config.value
  if (!current) return true
  return current.newapi_sync_enabled !== props.enabled
    || current.newapi_base_url !== form.newapi_base_url
    || current.newapi_user_access_token !== form.newapi_user_access_token
    || current.newapi_user_id !== form.newapi_user_id
})

const saveConfig = async (notify: boolean, force = false): Promise<boolean> => {
  if (pendingLoad) await pendingLoad
  form.newapi_sync_enabled = props.enabled
  if (!force && !configDirty.value) return true
  saving.value = true
  try {
    applyConfig(await adminAPI.accounts.updateNewAPISyncConfig(props.accountId, { ...form }))
    if (notify) appStore.showSuccess(t('admin.accounts.newapiSync.saved'))
    return true
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(
      error,
      t,
      'admin.accounts.newapiSync.errors',
      t('admin.accounts.newapiSync.saveFailed')
    ))
    return false
  } finally {
    saving.value = false
  }
}

const testConnection = async () => {
  preview.value = null
  if (!(await saveConfig(false, true))) return
  testing.value = true
  try {
    preview.value = await adminAPI.accounts.testNewAPISyncConnection(props.accountId)
    appStore.showSuccess(t('admin.accounts.newapiSync.testSuccess'))
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(
      error,
      t,
      'admin.accounts.newapiSync.errors',
      t('admin.accounts.newapiSync.testFailed')
    ))
  } finally {
    testing.value = false
  }
}

const syncNow = async () => {
  preview.value = null
  if (!(await saveConfig(false, true))) return
  syncing.value = true
  try {
    const result = await adminAPI.accounts.syncNewAPIRatio(props.accountId)
    preview.value = result
    await load()
    if (typeof result.new_ratio === 'number') emit('synced', result.new_ratio)
    appStore.showSuccess(result.changed
      ? t('admin.accounts.newapiSync.syncChanged')
      : t('admin.accounts.newapiSync.syncUnchanged'))
  } catch (error) {
    await load()
    appStore.showError(extractI18nErrorMessage(
      error,
      t,
      'admin.accounts.newapiSync.errors',
      t('admin.accounts.newapiSync.syncFailed')
    ))
  } finally {
    syncing.value = false
  }
}

const resolution = computed(() => preview.value?.resolution)
const resolvedUserGroup = computed(() =>
  resolution.value?.user_group || config.value?.newapi_resolved_user_group || ''
)
const resolvedTokenGroup = computed(() =>
  resolution.value?.token_group || config.value?.newapi_resolved_token_group || ''
)
const resolvedActualGroup = computed(() =>
  resolution.value?.actual_group || config.value?.newapi_resolved_actual_group || ''
)
const displayRatio = computed(() =>
  preview.value?.new_ratio ?? config.value?.current_ratio
)
const ratioSource = computed<NewAPIRatioSource>(() =>
  resolution.value?.ratio_source || config.value?.newapi_ratio_source || ''
)
const crossGroupRetry = computed(() =>
  resolution.value?.cross_group_retry ?? config.value?.newapi_cross_group_retry ?? false
)
const status = computed(() => preview.value?.status || config.value?.newapi_last_sync_status || 'never')
const statusLabel = computed(() => t(`admin.accounts.newapiSync.statuses.${status.value}`))
const statusClass = computed(() => {
  if (status.value === 'ok') return 'text-emerald-700 dark:text-emerald-300'
  if (status.value === 'failed') return 'text-red-700 dark:text-red-300'
  return 'text-gray-700 dark:text-gray-300'
})
const ratioSourceLabel = computed(() => {
  if (!ratioSource.value) return '-'
  return t(`admin.accounts.newapiSync.sources.${ratioSource.value}`)
})

const formatDate = (value?: string) => {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
const formatRatio = (value?: number) => {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return `${Number(value.toFixed(8))}x`
}
const errorLabel = (code: string) => {
  if (/^newapi_.+_http_\d+$/.test(code)) {
    return t('admin.accounts.newapiSync.errors.newapi_http_error')
  }
  const key = `admin.accounts.newapiSync.errors.${code}`
  return te(key) ? t(key) : code
}

const persistConfig = () => saveConfig(false)

defineExpose({ persistConfig })

onMounted(startLoad)
watch(() => props.accountId, startLoad)
watch(() => props.enabled, (enabled) => {
  form.newapi_sync_enabled = enabled
})
</script>
