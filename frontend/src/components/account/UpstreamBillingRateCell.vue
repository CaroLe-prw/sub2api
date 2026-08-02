<template>
  <div v-if="eligible" class="flex min-h-6 min-w-[8rem] items-center gap-1">
    <HelpTooltip class="-ml-1" width-class="w-max max-w-[calc(100vw-2rem)]" data-testid="upstream-billing-details">
      <template #trigger>
        <span
          class="cursor-help border-b border-dotted border-gray-300 font-mono text-sm font-medium text-gray-800 dark:border-dark-600 dark:text-gray-200"
          data-testid="upstream-billing-rate"
        >
          {{ primaryValue }}
        </span>
      </template>
      <div class="space-y-1">
        <p>{{ t('admin.accounts.upstreamBilling.schedulingRate', { value: schedulingRate }) }}</p>
        <p>
          {{
            hasEffectiveRate
              ? t('admin.accounts.upstreamBilling.schedulingSourceCalibrated')
              : t('admin.accounts.upstreamBilling.schedulingSourceFallback')
          }}
        </p>
        <p>{{ t('admin.accounts.upstreamBilling.calibration', { value: calibration }) }}</p>
        <p>{{ t('admin.accounts.upstreamBilling.manualFallback', { value: manualFallbackRate }) }}</p>
        <template v-if="hasEffectiveRate && data">
          <p v-if="isNewAPISnapshot">
            {{
              t('admin.accounts.upstreamBilling.newapiGroupRate', {
                group: data.newapi_group || '-',
                value: data.group_rate_multiplier
              })
            }}
          </p>
          <template v-else>
            <p>{{ t('admin.accounts.upstreamBilling.groupRate', { value: data.group_rate_multiplier }) }}</p>
            <p v-if="data.user_rate_multiplier != null">
              {{ t('admin.accounts.upstreamBilling.userRate', { value: data.user_rate_multiplier }) }}
            </p>
            <p>
              {{
                data.peak_rate_enabled
                  ? t('admin.accounts.upstreamBilling.peakRate', {
                      start: data.peak_start,
                      end: data.peak_end,
                      value: data.peak_rate_multiplier,
                      timezone: data.timezone
                    })
                  : t('admin.accounts.upstreamBilling.noPeakRate')
              }}
            </p>
          </template>
          <p>{{ t('admin.accounts.upstreamBilling.effectiveRate', { value: currentEffectiveRate ?? '-' }) }}</p>
          <p v-if="balanceInsufficient" class="text-red-400" data-testid="upstream-billing-balance-detail">
            {{ t('admin.accounts.upstreamBilling.balanceUnavailable') }}
          </p>
          <p v-else-if="balance != null" data-testid="upstream-billing-balance-detail">
            {{ t(balanceTranslationKey, { value: formatBalance(balance) }) }}
          </p>
          <p v-if="balanceAlertActive" class="text-red-400">
            {{
              t('admin.accounts.upstreamBilling.balanceLow', {
                value: formatBalance(balance ?? 0),
                threshold: formatBalance(snapshot?.balance_alert?.threshold ?? 0)
              })
            }}
          </p>
          <p>{{ t('admin.accounts.upstreamBilling.updatedAt', { value: formatDate(snapshot?.received_at) }) }}</p>
        </template>
        <template v-else-if="stale && lastDetectedRate != null">
          <p data-testid="upstream-billing-last-rate">
            {{ t('admin.accounts.upstreamBilling.lastDetectedRate', { value: lastDetectedRate }) }}
          </p>
          <p data-testid="upstream-billing-last-time">
            {{ t('admin.accounts.upstreamBilling.lastDetectedAt', { value: formatDate(snapshot?.received_at) }) }}
          </p>
          <p data-testid="upstream-billing-elapsed">
            {{ t('admin.accounts.upstreamBilling.elapsedSince', { value: elapsedSinceLastSuccess }) }}
          </p>
        </template>
        <p v-else>{{ statusLabel || '-' }}</p>
        <p
          v-if="probeEnabled && globalProbeEnabled !== false && nextProbeAt"
          data-testid="upstream-billing-next-probe"
        >
          {{ t('admin.accounts.upstreamBilling.nextProbeAt', { value: formatDate(nextProbeAt) }) }}
        </p>
        <p class="mt-2 border-t border-white/15 pt-2" data-testid="upstream-billing-probe-state">
          {{ t('admin.accounts.upstreamBilling.accountProbeState') }}
          <span :class="probeEnabled ? 'text-emerald-400' : 'text-red-400'">
            {{ probeEnabled ? t('admin.accounts.upstreamBilling.enabled') : t('admin.accounts.upstreamBilling.disabled') }}
          </span>
        </p>
        <p
          v-if="globalProbeEnabled === false"
          class="mt-1"
          data-testid="upstream-billing-global-probe-state"
        >
          {{ t('admin.accounts.upstreamBilling.globalProbeState') }}
          <span class="text-red-400">{{ t('admin.accounts.upstreamBilling.disabled') }}</span>
        </p>
      </div>
    </HelpTooltip>
    <span
      v-if="balanceInsufficient"
      class="whitespace-nowrap text-xs font-medium text-red-600 dark:text-red-400"
      data-testid="upstream-billing-balance"
    >
      {{ t('admin.accounts.upstreamBilling.balanceUnavailable') }}
    </span>
    <span
      v-else-if="balance != null"
      :class="balanceAlertActive ? 'text-red-600 dark:text-red-400' : 'text-gray-500 dark:text-gray-400'"
      class="whitespace-nowrap font-mono text-xs"
      data-testid="upstream-billing-balance"
    >
      {{ formatBalance(balance) }}
    </span>
    <span v-if="statusLabel" :class="statusClass" class="whitespace-nowrap text-[10px] font-medium">
      {{ statusLabel }}
    </span>
    <button
      type="button"
      class="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
      :disabled="probing"
      :aria-label="t('admin.accounts.upstreamBilling.manualProbe')"
      :title="t('admin.accounts.upstreamBilling.manualProbe')"
      data-testid="upstream-billing-probe"
      @click="$emit('probe')"
    >
      <Icon name="refresh" size="xs" :class="{ 'animate-spin': probing }" />
    </button>
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatMultiplier } from '@/utils/formatters'
import type { Account, UpstreamBillingProbeSnapshot } from '@/types'

const props = withDefaults(defineProps<{
  account: Account
  now: number
  probing?: boolean
  globalProbeEnabled?: boolean
}>(), {
  globalProbeEnabled: true
})

defineEmits<{
  (event: 'probe'): void
}>()

const { t } = useI18n()
const CLOCK_SKEW_TOLERANCE_MS = 5 * 60 * 1000
// 探测资格已放宽到全部 API-key 平台（上游是 sub2api 即可应答）。
const eligible = computed(() => props.account.type === 'apikey')
const snapshot = computed<UpstreamBillingProbeSnapshot | undefined>(() => props.account.extra?.upstream_billing_probe)
const data = computed(() => snapshot.value?.data)
const balance = computed(() => {
  const value = data.value?.balance
  return typeof value === 'number' && Number.isFinite(value) ? value : null
})
const newAPIBalanceSnapshot = computed(() => props.account.extra?.newapi_balance_snapshot)
const balanceInsufficient = computed(() => {
  if (balance.value != null) return balance.value <= 0
  return newAPIBalanceSnapshot.value?.overall_available === false
})
const balanceAlertActive = computed(() => snapshot.value?.balance_alert?.active === true)
const balanceTranslationKey = computed(() => data.value?.balance_kind === 'subscription_remaining'
  ? 'admin.accounts.upstreamBilling.subscriptionRemaining'
  : 'admin.accounts.upstreamBilling.walletBalance')
const isNewAPISnapshot = computed(() => data.value?.source === 'newapi')
const probeEnabled = computed(() => props.account.extra?.newapi_sync_enabled === true
  || props.account.extra?.upstream_billing_probe_enabled === true)
const nextProbeAt = computed(() => {
  const value = snapshot.value?.next_probe_at
  return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? value : ''
})
const receivedAt = computed(() => typeof snapshot.value?.received_at === 'string' ? Date.parse(snapshot.value.received_at) : Number.NaN)
const freshUntil = computed(() => {
  if (typeof snapshot.value?.fresh_until === 'string') return Date.parse(snapshot.value.fresh_until)
  if (snapshot.value?.status !== 'ok' || typeof snapshot.value.next_probe_at !== 'string') return Number.NaN
  const nextProbeAt = Date.parse(snapshot.value.next_probe_at)
  return Number.isFinite(nextProbeAt) && nextProbeAt > receivedAt.value
    ? receivedAt.value + 2 * (nextProbeAt - receivedAt.value)
    : Number.NaN
})
const validTimestamps = computed(() => {
  if (!Number.isFinite(receivedAt.value) || receivedAt.value > props.now + CLOCK_SKEW_TOLERANCE_MS) return false
  return Number.isFinite(freshUntil.value) && freshUntil.value > receivedAt.value
})
const stale = computed(() => {
  if (!snapshot.value) return false
  if (!Number.isFinite(receivedAt.value)) return snapshot.value.status === 'ok'
  if (!validTimestamps.value) return true
  return props.now > freshUntil.value
})
const parseMinute = (value?: string) => {
  if (typeof value !== 'string') return null
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const hour = Number(match[1])
  const minute = Number(match[2])
  return hour < 24 && minute < 60 ? hour * 60 + minute : null
}
const minuteInTimeZone = (timestamp: number, timeZone?: string) => {
  if (!timeZone) return null
  try {
    const parts = new Intl.DateTimeFormat('en-GB', {
      timeZone,
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23'
    }).formatToParts(new Date(timestamp))
    const hour = Number(parts.find(part => part.type === 'hour')?.value)
    const minute = Number(parts.find(part => part.type === 'minute')?.value)
    return Number.isInteger(hour) && Number.isInteger(minute) ? hour * 60 + minute : null
  } catch {
    return null
  }
}
const currentEffectiveRate = computed(() => {
  const billing = data.value
  if (!billing) return null
  if (billing.billing_scope !== 'token') return null
  const base = billing.resolved_rate_multiplier
  if (typeof base !== 'number' || !Number.isFinite(base) || base < 0) return null
  if (typeof billing.peak_rate_enabled !== 'boolean') return null
  if (!billing.peak_rate_enabled) return base
  const start = parseMinute(billing.peak_start)
  const end = parseMinute(billing.peak_end)
  const minute = minuteInTimeZone(props.now, billing.timezone)
  const peak = billing.peak_rate_multiplier
  if (start == null || end == null || minute == null || start >= end || typeof peak !== 'number' || !Number.isFinite(peak) || peak < 0) return null
  const value = minute >= start && minute < end ? base * peak : base
  return Number.isFinite(value) ? value : null
})
const lastDetectedRate = computed(() => {
  const value = data.value?.effective_rate_multiplier
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
    ? Number(value.toPrecision(12))
    : null
})
const elapsedSinceLastSuccess = computed(() => {
  if (!Number.isFinite(receivedAt.value)) return '-'
  const elapsedMinutes = Math.max(0, Math.floor((props.now - receivedAt.value) / 60_000))
  if (elapsedMinutes < 1) return t('admin.accounts.upstreamBilling.justNow')
  if (elapsedMinutes < 60) return t('admin.accounts.upstreamBilling.minutesAgo', { count: elapsedMinutes })
  const elapsedHours = Math.floor(elapsedMinutes / 60)
  if (elapsedHours < 24) return t('admin.accounts.upstreamBilling.hoursAgo', { count: elapsedHours })
  return t('admin.accounts.upstreamBilling.daysAgo', { count: Math.floor(elapsedHours / 24) })
})
const effectiveRate = computed(() => {
  if (!validTimestamps.value || stale.value || !['ok', 'failed'].includes(snapshot.value?.status ?? '')) return '-'
  const value = currentEffectiveRate.value
  return value == null ? '-' : `${formatMultiplier(value)}x`
})
const hasEffectiveRate = computed(() => effectiveRate.value !== '-')
const calibration = computed(() => {
  const value = props.account.extra?.openai_upstream_rate_calibration
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 1
})
const manualFallbackRate = computed(() => {
  const value = props.account.rate_multiplier
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 1
})
const schedulingRate = computed(() => {
  const value = hasEffectiveRate.value
    ? (currentEffectiveRate.value ?? 0) * calibration.value
    : manualFallbackRate.value
  return Number(value.toPrecision(12))
})
const statusLabel = computed(() => {
  if (!snapshot.value) return t('admin.accounts.upstreamBilling.notProbed')
  if (snapshot.value.status === 'unsupported') return t('admin.accounts.upstreamBilling.unsupported')
  if (stale.value) return t('admin.accounts.upstreamBilling.stale')
  if (snapshot.value.status === 'failed') return t('admin.accounts.upstreamBilling.failed')
  return ''
})
const statusClass = computed(() => {
  if (!snapshot.value) return 'text-gray-400 dark:text-gray-500'
  if (snapshot.value.status === 'unsupported') return 'text-gray-500 dark:text-gray-400'
  if (stale.value) return 'text-amber-600 dark:text-amber-400'
  if (snapshot.value.status === 'failed') return 'text-red-600 dark:text-red-400'
  return ''
})
const primaryValue = computed(() => `${schedulingRate.value}x`)
const formatBalance = (value: number) => `$${value.toLocaleString(undefined, { maximumFractionDigits: 4 })}`
const formatDate = (value?: string) => value
  ? new Date(value).toLocaleString(undefined, {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    })
  : '-'
</script>
