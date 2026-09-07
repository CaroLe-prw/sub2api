<template>
  <div class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-600" data-testid="response-model-mapping">
    <div>
      <label class="input-label">{{ t('admin.accounts.responseModelMapping.title') }}</label>
      <p class="input-hint">{{ t('admin.accounts.responseModelMapping.hint') }}</p>
    </div>
    <div v-for="(row, index) in modelValue" :key="index" class="flex items-center gap-2">
      <input
        :value="row.from"
        class="input min-w-0 flex-1"
        required
        :aria-label="t('admin.accounts.responseModelMapping.from')"
        :placeholder="t('admin.accounts.responseModelMapping.from')"
        @input="updateRow(index, 'from', ($event.target as HTMLInputElement).value)"
      />
      <span aria-hidden="true">→</span>
      <input
        :value="row.to"
        class="input min-w-0 flex-1"
        required
        :aria-label="t('admin.accounts.responseModelMapping.to')"
        :placeholder="t('admin.accounts.responseModelMapping.to')"
        @input="updateRow(index, 'to', ($event.target as HTMLInputElement).value)"
      />
      <button type="button" class="shrink-0 text-sm text-red-500" @click="removeRow(index)">
        {{ t('common.delete') }}
      </button>
    </div>
    <button type="button" class="btn btn-secondary" :disabled="modelValue.length >= 64" @click="addRow">
      {{ t('admin.accounts.responseModelMapping.add') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ResponseModelMappingRow } from '@/utils/responseModelMapping'

const props = defineProps<{ modelValue: ResponseModelMappingRow[] }>()
const emit = defineEmits<{ 'update:modelValue': [rows: ResponseModelMappingRow[]] }>()
const { t } = useI18n()

function updateRow(index: number, field: 'from' | 'to', value: string) {
  emit('update:modelValue', props.modelValue.map((row, i) => i === index ? { ...row, [field]: value } : row))
}
function removeRow(index: number) {
  emit('update:modelValue', props.modelValue.filter((_, i) => i !== index))
}
function addRow() {
  emit('update:modelValue', [...props.modelValue, { from: '', to: '' }])
}
</script>
