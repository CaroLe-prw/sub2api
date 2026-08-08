<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { UpstreamBillingMode } from './upstreamBilling'

const props = withDefaults(defineProps<{
  allowNewApi?: boolean
}>(), {
  allowNewApi: false
})

const mode = defineModel<UpstreamBillingMode>('mode', { required: true })
const { t } = useI18n()

const modeOptions = computed(() => [
  { value: 'off', label: t('admin.accounts.upstreamBilling.modes.off') },
  { value: 'sub2api', label: t('admin.accounts.upstreamBilling.modes.sub2api') },
  ...(props.allowNewApi
    ? [{ value: 'newapi' as const, label: t('admin.accounts.upstreamBilling.modes.newapi') }]
    : [])
])
</script>

<template>
  <div class="space-y-4" data-testid="upstream-billing-source-field">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
      <div>
        <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.mode') }}</label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.upstreamBilling.modeHint') }}
        </p>
      </div>
      <div class="w-full sm:w-56">
        <Select
          v-model="mode"
          :options="modeOptions"
          data-testid="upstream-billing-mode"
        />
      </div>
    </div>
  </div>
</template>
