<template>
  <div>
    <label class="input-label" for="gemini-upstream-protocol">{{ t('admin.accounts.gemini.upstreamProtocol') }}</label>
    <select id="gemini-upstream-protocol" class="input" data-testid="gemini-upstream-protocol" :value="modelValue" @change="onChange">
      <option value="gemini">{{ t('admin.accounts.gemini.nativeProtocol') }}</option>
      <option value="chat_completions">{{ t('admin.accounts.gemini.openaiProtocol') }}</option>
    </select>
    <p class="input-hint">{{ t('admin.accounts.gemini.upstreamProtocolHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: 'gemini' | 'chat_completions'] }>()
const { t } = useI18n()
function onChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  emit('update:modelValue', value === 'chat_completions' ? value : 'gemini')
}
</script>
