<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { paymentVerificationAPI } from '@/api/admin/paymentVerification'
import type { PaymentVerificationAction, PaymentVerificationAlert, PaymentVerificationConfig, PaymentVerificationConfigUpdate } from '@/api/admin/paymentVerification'
import type { OpsTelegramNotificationDraft } from '@/api/admin/ops'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores'

const props = defineProps<{ templates: OpsTelegramNotificationDraft[] }>()
const { t } = useI18n()
const app = useAppStore()
const key = 'admin.ops.telegram.paymentVerification.'
const loading = ref(true)
const config = ref<PaymentVerificationConfig | null>(null)
const adminIDs = ref('')
const savedDraft = ref('')
const saving = ref(false)
const connecting = ref(false)
const scanning = ref(false)
const recordsLoading = ref(false)
const recordsFailed = ref(false)
const alerts = ref<PaymentVerificationAlert[]>([])
const handlingID = ref<number | null>(null)
const banTarget = ref<PaymentVerificationAlert | null>(null)
const busy = computed(() => saving.value || connecting.value || scanning.value)
const templateOptions = computed(() => [
  { value: '', label: t(`${key}selectTemplate`) },
  ...props.templates.filter(template => template.enabled).map(template => ({
    value: template.id, label: template.name || t('admin.ops.telegram.unnamedTemplate')
  }))
])
const draft = computed(() => JSON.stringify({
  enabled: config.value?.enabled,
  template_id: config.value?.template_id,
  lookback_days: config.value?.lookback_days,
  interval_minutes: config.value?.interval_minutes,
  webhook_url: config.value?.webhook_url,
  adminIDs: adminIDs.value
}))
const dirty = computed(() => draft.value !== savedDraft.value)
const visibleAlerts = computed(() => alerts.value.filter(alert => alert.result !== 'verified'))

function assign(value: PaymentVerificationConfig): void {
  config.value = { ...value }
  adminIDs.value = (value.admin_telegram_ids || []).join('\n')
  savedDraft.value = draft.value
  if (!config.value.webhook_url) {
    config.value.webhook_url = `${window.location.origin}/api/v1/payment/verification/telegram`
  }
}

function message(error: unknown, fallback: string): string {
  const detail = (error as { response?: { data?: { detail?: unknown } } })?.response?.data?.detail
  return typeof detail === 'string' ? detail : t(`${key}${fallback}`)
}

async function load(): Promise<void> {
  loading.value = true
  try {
    assign(await paymentVerificationAPI.getConfig())
  } catch (error) {
    app.showError(message(error, 'loadFailed'))
  } finally {
    loading.value = false
  }
}

function payload(): PaymentVerificationConfigUpdate | null {
  if (!config.value) return null
  const tokens = adminIDs.value.split(/[\s,，]+/).filter(Boolean)
  const ids = [...new Set(tokens.map(Number))]
  if (tokens.some(token => !/^\d+$/.test(token)) || ids.some(id => !Number.isSafeInteger(id) || id <= 0)) {
    app.showError(t(`${key}invalidAdminIDs`))
    return null
  }
  const { enabled, template_id, lookback_days, interval_minutes } = config.value
  if (!Number.isInteger(lookback_days) || lookback_days < 1 || lookback_days > 365 ||
      !Number.isInteger(interval_minutes) || interval_minutes < 1 || interval_minutes > 60) {
    app.showError(t(`${key}invalidSchedule`))
    return null
  }
  if (enabled && !props.templates.some(template => template.enabled && template.id === template_id)) {
    app.showError(t(`${key}templateRequired`))
    return null
  }
  if (enabled && ids.length === 0) {
    app.showError(t(`${key}adminsRequired`))
    return null
  }
  const webhook_url = config.value.webhook_url.trim()
  if (enabled || webhook_url) {
    try {
      const url = new URL(webhook_url)
      if (url.protocol !== 'https:' || url.username || url.password || url.search || url.hash) throw new Error('invalid URL')
    } catch {
      app.showError(t(`${key}invalidWebhook`))
      return null
    }
  }
  return { enabled, template_id, lookback_days, interval_minutes, admin_telegram_ids: ids, webhook_url }
}

async function save(): Promise<void> {
  if (busy.value) return
  const value = payload()
  if (!value) return
  saving.value = true
  try {
    assign(await paymentVerificationAPI.updateConfig(value))
    app.showSuccess(t(`${key}saveSuccess`))
  } catch (error) {
    app.showError(message(error, 'saveFailed'))
  } finally {
    saving.value = false
  }
}

async function connect(): Promise<void> {
  if (busy.value || dirty.value || !config.value?.enabled || !config.value.template_id) return
  connecting.value = true
  try {
    await paymentVerificationAPI.connectTelegram()
    if (config.value) config.value.webhook_configured = true
    app.showSuccess(t(`${key}connectSuccess`))
  } catch (error) {
    app.showError(message(error, 'connectFailed'))
  } finally {
    connecting.value = false
  }
}

async function refresh(): Promise<void> {
  if (recordsLoading.value) return
  recordsLoading.value = true
  recordsFailed.value = false
  try {
    alerts.value = await paymentVerificationAPI.getAlerts(50)
  } catch (error) {
    recordsFailed.value = true
    app.showError(message(error, 'recordsFailed'))
  } finally {
    recordsLoading.value = false
  }
}

async function scan(): Promise<void> {
  if (busy.value || dirty.value || !config.value?.enabled) return
  scanning.value = true
  try {
    const result = await paymentVerificationAPI.scan()
    app.showSuccess(t(`${key}scanSuccess`, { checked: result.checked, anomalies: result.anomalies, errors: result.errors }))
    await refresh()
  } catch (error) {
    app.showError(message(error, 'scanFailed'))
  } finally {
    scanning.value = false
  }
}

function canBan(alert: PaymentVerificationAlert): boolean {
  return alert.state === 'pending' && ['unpaid', 'amount_mismatch', 'trade_mismatch'].includes(alert.result)
}

async function resolve(alert: PaymentVerificationAlert, action: PaymentVerificationAction): Promise<void> {
  if (handlingID.value !== null || alert.state !== 'pending' || (action === 'ban' && !canBan(alert))) return
  handlingID.value = alert.id
  banTarget.value = null
  try {
    const result = await paymentVerificationAPI.resolve(alert.id, action)
    alerts.value = alerts.value.map(value => value.id === result.id ? result : value)
    app.showSuccess(t(`${key}${action === 'ban' ? 'banSuccess' : 'ignoreSuccess'}`))
  } catch (error) {
    app.showError(message(error, 'resolveFailed'))
  } finally {
    handlingID.value = null
  }
}

function amount(value: number | null, currency: string): string {
  return value == null ? t(`${key}unknownAmount`) : `${Number(value).toFixed(2)} ${currency || 'CNY'}`
}

onMounted(() => { void load(); void refresh() })
</script>

<template>
  <section class="card" aria-labelledby="payment-verification-heading">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="payment-verification-heading" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(`${key}title`) }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`${key}description`) }}</p>
    </div>
    <div v-if="loading" class="p-6 text-sm text-gray-500">{{ t('common.loading') }}</div>
    <div v-else-if="!config" class="p-6">
      <p class="mb-3 text-sm text-red-600">{{ t(`${key}loadFailed`) }}</p>
      <button type="button" class="btn btn-secondary" @click="load">{{ t(`${key}reload`) }}</button>
    </div>
    <div v-else class="space-y-5 p-6">
      <div class="rounded-lg bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-900/20 dark:text-blue-200">{{ t(`${key}manualOnly`) }}</div>
      <div class="flex items-center justify-between gap-4">
        <label for="payment-verification-enabled" class="text-sm font-medium">{{ t(`${key}enabled`) }}</label>
        <Toggle id="payment-verification-enabled" v-model="config.enabled" :disabled="busy" />
      </div>
      <div class="grid gap-5 md:grid-cols-2">
        <div>
          <label for="payment-verification-template" class="input-label">{{ t(`${key}template`) }}</label>
          <Select id="payment-verification-template" v-model="config.template_id" :options="templateOptions" :disabled="busy" />
          <p class="mt-1 text-xs text-gray-500">{{ t(`${key}templateHint`) }}</p>
        </div>
        <div>
          <label for="payment-verification-admins" class="input-label">{{ t(`${key}adminIDs`) }}</label>
          <textarea id="payment-verification-admins" v-model="adminIDs" rows="2" class="input w-full" :disabled="busy" />
          <p class="mt-1 text-xs text-gray-500">{{ t(`${key}adminIDsHint`) }}</p>
        </div>
        <div>
          <label for="payment-verification-lookback" class="input-label">{{ t(`${key}lookbackDays`) }}</label>
          <input id="payment-verification-lookback" v-model.number="config.lookback_days" type="number" min="1" max="365" step="1" class="input w-full" :disabled="busy" />
        </div>
        <div>
          <label for="payment-verification-interval" class="input-label">{{ t(`${key}intervalMinutes`) }}</label>
          <input id="payment-verification-interval" v-model.number="config.interval_minutes" type="number" min="1" max="60" step="1" class="input w-full" :disabled="busy" />
        </div>
      </div>
      <div>
        <label for="payment-verification-webhook" class="input-label">{{ t(`${key}webhookURL`) }}</label>
        <input id="payment-verification-webhook" v-model="config.webhook_url" type="url" class="input w-full" :disabled="busy" />
        <p class="mt-1 text-xs text-gray-500">{{ t(`${key}webhookHint`) }}</p>
      </div>
      <div class="space-y-2 rounded-lg border border-gray-200 p-4 text-sm dark:border-dark-600">
        <p :class="config.webhook_configured ? 'text-green-600' : 'text-gray-500'">{{ t(`${key}${config.webhook_configured ? 'connected' : 'notConnected'}`) }}</p>
        <p class="text-gray-500">{{ t(`${key}dedicatedBotHint`) }}</p>
        <p v-if="!config.enabled" class="text-gray-500">{{ t(`${key}enableFirst`) }}</p>
        <p v-if="dirty" class="text-amber-600">{{ t(`${key}saveFirst`) }}</p>
      </div>
      <div class="flex flex-wrap justify-end gap-3">
        <button type="button" class="btn btn-secondary" data-testid="connect-payment-bot" :disabled="busy || dirty || !config.enabled || !config.template_id" @click="connect">{{ t(`${key}${connecting ? 'connecting' : 'connect'}`) }}</button>
        <button type="button" class="btn btn-primary" data-testid="save-payment-verification" :disabled="busy" @click="save">{{ saving ? t('common.saving') : t(`${key}save`) }}</button>
      </div>
    </div>
    <div class="space-y-4 border-t border-gray-100 p-6 dark:border-dark-700">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h3 class="font-medium text-gray-900 dark:text-white">{{ t(`${key}recordsTitle`) }}</h3>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary btn-sm" data-testid="refresh-payment-verification" :disabled="recordsLoading || scanning" @click="refresh">{{ t(`${key}refresh`) }}</button>
          <button type="button" class="btn btn-secondary btn-sm" data-testid="scan-payment-verification" :disabled="!config?.enabled || busy || dirty" @click="scan">{{ t(`${key}${scanning ? 'scanning' : 'scan'}`) }}</button>
        </div>
      </div>
      <p class="text-xs text-gray-500">{{ t(`${key}recordsHint`) }}</p>
      <p v-if="recordsLoading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="recordsFailed" class="text-sm text-red-600">{{ t(`${key}recordsFailed`) }}</p>
      <p v-else-if="!visibleAlerts.length" class="py-4 text-center text-sm text-gray-500">{{ t(`${key}noRecords`) }}</p>
      <div v-if="visibleAlerts.length" class="overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead class="border-b border-gray-100 text-xs text-gray-500 dark:border-dark-700">
            <tr><th class="px-2 py-3">{{ t(`${key}user`) }}</th><th class="px-2 py-3">{{ t(`${key}order`) }}</th><th class="px-2 py-3">{{ t(`${key}amounts`) }}</th><th class="px-2 py-3">{{ t(`${key}problem`) }}</th><th class="px-2 py-3">{{ t(`${key}state`) }}</th><th class="px-2 py-3">{{ t('common.actions') }}</th></tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="alert in visibleAlerts" :key="alert.id" :data-testid="`payment-alert-${alert.id}`">
              <td class="px-2 py-3 align-top"><p>#{{ alert.user_id }}</p><p class="max-w-48 break-all text-xs text-gray-500">{{ alert.email }}</p></td>
              <td class="px-2 py-3 align-top"><p>#{{ alert.order_id }} · {{ alert.provider_name }}</p><p class="max-w-52 break-all text-xs text-gray-500">{{ alert.out_trade_no }}</p><p class="text-xs text-gray-500">{{ alert.order_status }}</p></td>
              <td class="whitespace-nowrap px-2 py-3 align-top"><p>{{ t(`${key}credited`) }}: {{ amount(alert.amount, 'USD') }}</p><p>{{ t(`${key}expected`) }}: {{ amount(alert.pay_amount, alert.currency) }}</p><p>{{ t(`${key}actual`) }}: {{ amount(alert.upstream_amount, alert.currency) }}</p></td>
              <td class="max-w-72 px-2 py-3 align-top"><p class="font-medium text-amber-700 dark:text-amber-400">{{ t(`${key}results.${alert.result}`) }}</p><p class="mt-1 break-words text-xs text-gray-500">{{ alert.reason }}</p><p class="mt-1 text-xs text-gray-500">{{ alert.checked_at }}</p></td>
              <td class="px-2 py-3 align-top"><p>{{ t(`${key}states.${alert.state}`) }}</p><p v-if="alert.handled_by" class="mt-1 text-xs text-gray-500">{{ alert.handled_by }}</p></td>
              <td class="px-2 py-3 align-top"><div v-if="alert.state === 'pending'" class="flex flex-wrap gap-2"><button v-if="canBan(alert)" type="button" class="btn btn-danger btn-sm whitespace-nowrap" :disabled="handlingID !== null" data-testid="ban-payment-user" @click="banTarget = alert">{{ t(`${key}ban`) }}</button><button type="button" class="btn btn-secondary btn-sm whitespace-nowrap" :disabled="handlingID !== null" data-testid="ignore-payment-alert" @click="resolve(alert, 'ignore')">{{ t(`${key}ignore`) }}</button></div></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <ConfirmDialog :show="!!banTarget" :title="t(`${key}confirmBanTitle`)" :message="t(`${key}confirmBanMessage`, { user: banTarget?.user_id, order: banTarget?.order_id })" :confirm-text="t(`${key}ban`)" danger @confirm="banTarget && resolve(banTarget, 'ban')" @cancel="banTarget = null" />
  </section>
</template>
