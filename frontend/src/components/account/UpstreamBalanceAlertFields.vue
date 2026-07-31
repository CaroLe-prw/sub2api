<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'

const enabled = defineModel<boolean>('enabled', { required: true })
const threshold = defineModel<number | null>('threshold', { required: true })
const { t } = useI18n()

function updateThreshold(event: Event): void {
  const raw = (event.target as HTMLInputElement).value
  if (raw === '') {
    threshold.value = null
    return
  }
  const value = Number(raw)
  threshold.value = Number.isFinite(value) ? value : null
}
</script>

<template>
  <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="upstream-balance-alert-fields">
    <div class="flex items-start justify-between gap-4">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.balanceAlert') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.upstreamBilling.balanceAlertHint') }}
        </p>
      </div>
      <Toggle
        v-model="enabled"
        :aria-label="t('admin.accounts.upstreamBilling.balanceAlert')"
        data-testid="upstream-balance-alert-enabled"
      />
    </div>
    <div v-if="enabled">
      <label class="input-label">{{ t('admin.accounts.upstreamBilling.balanceAlertThreshold') }}</label>
      <div class="relative">
        <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-sm text-gray-400">$</span>
        <input
          :value="threshold ?? ''"
          type="number"
          min="0"
          step="any"
          required
          class="input pl-7"
          data-testid="upstream-balance-alert-threshold"
          @input="updateThreshold"
        />
      </div>
      <p class="input-hint">{{ t('admin.accounts.upstreamBilling.balanceAlertThresholdHint') }}</p>
    </div>
  </div>
</template>
