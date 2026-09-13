<template>
  <div>
    <label class="input-label">{{ t('admin.accounts.upstreamBilling.calibrationFactor') }}</label>
    <input
      v-model.number="value"
      type="number"
      min="0"
      step="any"
      required
      class="input"
      data-testid="upstream-rate-calibration"
    />
    <p class="input-hint">{{ t('admin.accounts.upstreamBilling.calibrationHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const value = defineModel<number>({ required: true })
const { t } = useI18n()
</script>
