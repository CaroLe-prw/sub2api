<template>
  <div class="mt-3 border-t border-gray-200 pt-3 dark:border-dark-600">
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <h4 class="text-xs font-semibold text-gray-900 dark:text-white">
        {{ t('admin.accounts.newapiSync.balance.title') }}
      </h4>
      <span
        class="text-xs font-medium"
        :class="stale ? 'text-amber-700 dark:text-amber-300' : 'text-emerald-700 dark:text-emerald-300'"
      >
        {{ stale
          ? t('admin.accounts.newapiSync.balance.stale')
          : t('admin.accounts.newapiSync.balance.fresh') }}
      </span>
    </div>

    <p v-if="!snapshot" class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.newapiSync.balance.empty') }}
    </p>

    <template v-else>
      <dl class="grid grid-cols-[minmax(8rem,auto)_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.tokenName') }}
        </dt>
        <dd class="break-words text-gray-900 dark:text-white">{{ snapshot.token.name || '-' }}</dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.tokenRemaining') }}
        </dt>
        <dd class="break-words font-mono text-gray-900 dark:text-white">
          {{ snapshot.token.unlimited_quota
            ? t('admin.accounts.newapiSync.balance.unlimited')
            : formatQuotaWithDisplay(snapshot.token.remaining_quota) }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.tokenUsed') }}
        </dt>
        <dd class="break-words font-mono text-gray-900 dark:text-white">
          {{ formatQuotaWithDisplay(snapshot.token.used_quota) }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.tokenTotal') }}
        </dt>
        <dd class="break-words font-mono text-gray-900 dark:text-white">
          {{ snapshot.token.unlimited_quota
            ? t('admin.accounts.newapiSync.balance.unlimited')
            : formatQuotaWithDisplay(snapshot.token.total_quota) }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.tokenUnlimited') }}
        </dt>
        <dd class="text-gray-900 dark:text-white">
          {{ snapshot.token.unlimited_quota ? t('common.yes') : t('common.no') }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.accountRemaining') }}
        </dt>
        <dd class="break-words font-mono text-gray-900 dark:text-white">
          {{ formatQuotaWithDisplay(snapshot.account.remaining_quota) }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.accountUsed') }}
        </dt>
        <dd class="break-words font-mono text-gray-900 dark:text-white">
          {{ formatQuotaWithDisplay(snapshot.account.used_quota) }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.accountTotal') }}
        </dt>
        <dd class="break-words font-mono text-gray-900 dark:text-white">
          {{ formatQuotaWithDisplay(snapshot.account.total_quota) }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.availability') }}
        </dt>
        <dd :class="snapshot.overall_available ? 'text-emerald-700 dark:text-emerald-300' : 'text-red-700 dark:text-red-300'">
          {{ snapshot.overall_available
            ? t('admin.accounts.newapiSync.balance.available')
            : t('admin.accounts.newapiSync.balance.unavailable') }}
        </dd>

        <dt class="text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.newapiSync.balance.lastSuccessAt') }}
        </dt>
        <dd class="break-words text-gray-900 dark:text-white">{{ formatDate(snapshot.synced_at) }}</dd>
      </dl>

      <p
        v-if="snapshot.warnings?.length"
        class="mt-3 text-xs text-amber-700 dark:text-amber-300"
      >
        {{ t('admin.accounts.newapiSync.balance.quotaMismatch') }}
      </p>
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
        {{ snapshot.quota_display && snapshot.quota_display.display_type !== 'TOKENS'
          ? t('admin.accounts.newapiSync.balance.convertedQuotaHint')
          : t('admin.accounts.newapiSync.balance.rawQuotaHint') }}
      </p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { NewAPIBalanceSnapshot } from '@/types'

const props = defineProps<{
  snapshot?: NewAPIBalanceSnapshot
  stale: boolean
}>()

const { t } = useI18n()

const formatQuota = (value: number) => Number.isSafeInteger(value)
  ? value.toLocaleString()
  : '-'

const formatQuotaWithDisplay = (value: number) => {
  const rawQuota = formatQuota(value)
  const display = props.snapshot?.quota_display
  if (
    rawQuota === '-'
    || !display
    || display.display_type === 'TOKENS'
    || !Number.isFinite(display.quota_per_unit)
    || display.quota_per_unit <= 0
    || !Number.isFinite(display.exchange_rate)
    || display.exchange_rate <= 0
  ) {
    return rawQuota
  }
  const amount = value / display.quota_per_unit * display.exchange_rate
  if (!Number.isFinite(amount)) return rawQuota
  const formattedAmount = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: Math.abs(amount) < 1 ? 4 : 2
  }).format(amount)
  return `${display.symbol || ''}${formattedAmount} (${rawQuota} quota)`
}

const formatDate = (value?: string) => {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>
