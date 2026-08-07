<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  currentRate: number
}>()

const enabled = defineModel<boolean>('enabled', { required: true })
const threshold = defineModel<number | null>('threshold', { required: true })
const { t } = useI18n()

const isCurrentlyBlocked = computed(
  () => enabled.value && (threshold.value ?? 0) > 0 && props.currentRate > (threshold.value ?? 0)
)

function toggleEnabled() {
  const nextEnabled = !enabled.value
  enabled.value = nextEnabled
  if (nextEnabled && (!threshold.value || threshold.value <= 0) && props.currentRate > 0) {
    threshold.value = props.currentRate
  }
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between gap-4">
      <div>
        <label class="input-label mb-0">{{ t('keys.groupRateGuard') }}</label>
        <p class="input-hint mt-1">{{ t('keys.groupRateGuardHint') }}</p>
      </div>
      <button
        type="button"
        role="switch"
        :aria-checked="enabled"
        :aria-label="t('keys.groupRateGuard')"
        :class="[
          'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
          enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
        ]"
        @click="toggleEnabled"
      >
        <span
          :class="[
            'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
            enabled ? 'translate-x-4' : 'translate-x-0'
          ]"
        />
      </button>
    </div>

    <div v-if="enabled">
      <label class="input-label">{{ t('keys.maxGroupRateMultiplier') }}</label>
      <div class="relative">
        <input
          v-model.number="threshold"
          type="number"
          min="0.00000001"
          step="any"
          required
          class="input pr-8"
          :class="{ 'border-red-500 dark:border-red-500': isCurrentlyBlocked }"
          :placeholder="t('keys.maxGroupRateMultiplierPlaceholder')"
        />
        <span class="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-500">x</span>
      </div>
      <p class="input-hint">
        {{ t('keys.currentGroupRate', { rate: currentRate }) }}
      </p>
      <p v-if="isCurrentlyBlocked" class="mt-1 text-sm text-amber-600 dark:text-amber-400">
        {{ t('keys.groupRateGuardCurrentlyBlocked') }}
      </p>
    </div>
  </div>
</template>
