<template>
  <div class="space-y-5">
    <fieldset>
      <legend class="input-label">{{ t('admin.quotaReset.policy.windows') }}</legend>
      <div class="grid grid-cols-1 gap-2 sm:grid-cols-3">
        <label
          v-for="window in windows"
          :key="window.key"
          class="flex cursor-pointer items-center gap-2 rounded-lg border border-gray-200 px-3 py-2.5 text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-700"
        >
          <input
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            :checked="modelValue[window.key]"
            @change="updateWindow(window.key, ($event.target as HTMLInputElement).checked)"
          />
          <span>{{ t(window.label) }}</span>
        </label>
      </div>
    </fieldset>

    <fieldset>
      <legend class="input-label">{{ t('admin.quotaReset.policy.windowMode') }}</legend>
      <div class="space-y-2">
        <label
          v-for="mode in modes"
          :key="mode.value"
          class="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 px-3 py-3 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:hover:bg-dark-700"
        >
          <input
            type="radio"
            class="mt-0.5 h-4 w-4 border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            :name="`${idPrefix}-window-mode`"
            :value="mode.value"
            :checked="modelValue.window_start_mode === mode.value"
            @change="updateMode(mode.value)"
          />
          <span>
            <span class="block text-sm font-medium text-gray-800 dark:text-gray-100">
              {{ t(mode.title) }}
            </span>
            <span class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
              {{ t(mode.hint) }}
            </span>
          </span>
        </label>
      </div>
    </fieldset>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type {
  QuotaResetPolicy,
  QuotaResetWindowStartMode
} from '@/api/admin/quotaReset'

const { t } = useI18n()

const props = withDefaults(
  defineProps<{
    modelValue: QuotaResetPolicy
    idPrefix?: string
  }>(),
  {
    idPrefix: 'quota-reset'
  }
)

const emit = defineEmits<{
  'update:modelValue': [value: QuotaResetPolicy]
}>()

const windows = [
  { key: 'daily', label: 'admin.quotaReset.policy.daily' },
  { key: 'weekly', label: 'admin.quotaReset.policy.weekly' },
  { key: 'monthly', label: 'admin.quotaReset.policy.monthly' }
] as const

const modes: Array<{
  value: QuotaResetWindowStartMode
  title: string
  hint: string
}> = [
  {
    value: 'current',
    title: 'admin.quotaReset.policy.modes.current',
    hint: 'admin.quotaReset.policy.modes.currentHint'
  },
  {
    value: 'natural_day',
    title: 'admin.quotaReset.policy.modes.naturalDay',
    hint: 'admin.quotaReset.policy.modes.naturalDayHint'
  },
  {
    value: 'preserve',
    title: 'admin.quotaReset.policy.modes.preserve',
    hint: 'admin.quotaReset.policy.modes.preserveHint'
  }
]

function updateWindow(key: 'daily' | 'weekly' | 'monthly', value: boolean) {
  emit('update:modelValue', { ...props.modelValue, [key]: value })
}

function updateMode(window_start_mode: QuotaResetWindowStartMode) {
  emit('update:modelValue', { ...props.modelValue, window_start_mode })
}
</script>
