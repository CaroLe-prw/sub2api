<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  billingRateMultiplier: number
}>()

const model = defineModel<number | null>({ required: true })
const { t } = useI18n()

const inputValue = computed(() => model.value ?? '')

const handleInput = (event: Event) => {
  const input = event.target as HTMLInputElement
  if (input.value.trim() === '') {
    model.value = null
    return
  }
  const value = input.valueAsNumber
  if (Number.isFinite(value) && value >= 0) {
    model.value = value
  }
}
</script>

<template>
  <div>
    <label class="input-label">
      {{ t('admin.groups.form.maxAccountCostMultiplier') }}
    </label>
    <div class="relative">
      <input
        :value="inputValue"
        type="number"
        min="0"
        step="0.001"
        class="input pr-8"
        :placeholder="t('admin.groups.form.maxAccountCostMultiplierPlaceholder')"
        @input="handleInput"
      />
      <span class="absolute right-3 top-1/2 -translate-y-1/2 text-sm text-gray-500 dark:text-gray-400">
        x
      </span>
    </div>
    <p class="input-hint">
      {{
        t('admin.groups.form.maxAccountCostMultiplierHint', {
          rate: props.billingRateMultiplier,
        })
      }}
    </p>
  </div>
</template>
