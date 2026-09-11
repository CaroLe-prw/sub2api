<template>
  <BaseDialog :show="usageId !== null" :title="t('admin.usage.response.title')" width="wide" @close="$emit('close')">
    <p class="mb-4 text-sm text-gray-500">{{ t('admin.usage.response.help') }}</p>
    <p v-if="loading" class="py-6 text-sm">{{ t('common.loading') }}</p>
    <div v-else-if="error" class="text-sm text-red-500">
      {{ t('admin.usage.response.loadError') }}
      <button class="btn btn-secondary ml-2" @click="load">{{ t('common.retry') }}</button>
    </div>
    <p v-else-if="!record" class="py-6 text-sm text-gray-500">{{ t('admin.usage.response.unavailable') }}</p>
    <template v-else>
      <section v-for="side in sides" :key="side.key" class="mb-6 space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <h3 class="font-semibold">{{ t(`admin.usage.response.${side.key}`) }}</h3>
          <span class="text-sm" :class="side.value?.status === 'extra' ? 'text-red-500' : 'text-gray-500'">
            {{ t(`admin.usage.response.status.${side.value?.status ?? 'unavailable'}`) }}
          </span>
          <span v-if="side.value" class="text-xs text-gray-500">{{ side.value.bytes.toLocaleString() }} bytes · {{ side.value.json_documents }} JSON</span>
        </div>
        <template v-if="side.value">
          <p v-if="side.value.controls_escaped" class="text-sm text-amber-600">{{ t('admin.usage.response.controlsEscaped') }}</p>
          <p v-if="side.value.truncated || !side.value.complete" class="text-sm text-amber-600">{{ t('admin.usage.response.partial') }}</p>
          <div v-for="(issue, index) in side.value.issues" :key="index" class="rounded-lg border border-red-200 p-3 dark:border-red-900">
            <p class="mb-2 text-sm text-red-500">{{ t(`admin.usage.response.issue.${issue.kind}`, { frame: issue.frame }) }}</p>
            <template v-if="issue.json">
              <p class="text-xs text-gray-500">{{ t('admin.usage.response.firstJSON') }}</p>
              <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-all text-xs">{{ issue.json }}</pre>
            </template>
            <template v-if="issue.extra">
              <p class="mt-2 text-xs text-gray-500">{{ t('admin.usage.response.extraSample') }}</p>
              <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-all text-xs">{{ issue.extra }}</pre>
            </template>
          </div>
          <details :open="side.value.status === 'extra'">
            <summary class="cursor-pointer text-sm text-primary-600">{{ t('admin.usage.response.raw') }}</summary>
            <pre class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-800">{{ side.value.body }}</pre>
          </details>
        </template>
      </section>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { getResponseDiagnostics, type ResponseDiagnostics } from '@/api/admin/usage'
const props = defineProps<{ usageId: number | null }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const record = ref<ResponseDiagnostics | null>(null)
const loading = ref(false)
const error = ref(false)
let controller: AbortController | null = null
const sides = computed(() => [
  { key: 'upstream', value: record.value?.upstream },
  { key: 'downstream', value: record.value?.downstream }
])
async function load() {
  controller?.abort()
  record.value = null
  error.value = false
  loading.value = false
  if (props.usageId === null) return
  const request = new AbortController()
  controller = request
  loading.value = true
  try {
    const result = await getResponseDiagnostics(props.usageId, request.signal)
    if (!request.signal.aborted) record.value = result
  } catch {
    if (!request.signal.aborted) error.value = true
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch(() => props.usageId, load, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>
