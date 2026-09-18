<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'

interface PayloadSnapshot {
  method?: string
  path?: string
  content_type?: string
  body?: string
  bytes?: number
  truncated?: boolean
  omitted_reason?: string
}
interface Attempt {
  account_id: number
  status_code: number
  request_id?: string
  request: PayloadSnapshot
  response: PayloadSnapshot
}
interface Diagnostics {
  client_request: PayloadSnapshot
  upstream_attempts: Attempt[]
  dropped_attempts?: number
}
const props = defineProps<{ diagnostics?: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const data = computed<Diagnostics | null>(() => {
  try {
    const parsed = JSON.parse(props.diagnostics || '')
    if (!parsed || typeof parsed.client_request !== 'object' || !parsed.client_request || !Array.isArray(parsed.upstream_attempts)) return null
    return parsed
  } catch {
    return null
  }
})
const sections = computed(() => {
  if (!data.value) return []
  const rows: Array<{ key: string; label: string; payload: PayloadSnapshot; info?: string }> = [
    { key: 'client-request', label: t('admin.ops.errorDetail.records.clientRequest'), payload: data.value.client_request }
  ]
  data.value.upstream_attempts.forEach((attempt, index) => {
    if (!attempt?.request || !attempt?.response) return
    const info = t('admin.ops.errorDetail.records.attempt', { index: index + 1, account: attempt.account_id, status: attempt.status_code || '—' })
    rows.push({ key: `upstream-request-${index}`, label: t('admin.ops.errorDetail.records.upstreamRequest'), payload: attempt.request, info })
    rows.push({ key: `upstream-response-${index}`, label: t('admin.ops.errorDetail.records.upstreamResponse'), payload: attempt.response, info: [info, attempt.request_id].filter(Boolean).join(' · ') })
  })
  return rows
})
function pretty(body: string): string {
  try { return JSON.stringify(JSON.parse(body), null, 2) } catch { return body }
}
function omission(reason?: string): string {
  const known = ['too_large', 'non_json', 'encoded_body', 'empty_or_unread', 'read_error', 'unavailable', 'not_captured']
  return t(`admin.ops.errorDetail.records.omitted.${known.includes(reason || '') ? reason : 'unavailable'}`)
}
</script>

<template>
  <section class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900" data-testid="request-diagnostics">
    <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">{{ t('admin.ops.errorDetail.records.title') }}</h3>
    <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorDetail.records.hint') }}</p>
    <p v-if="!data" class="mt-4 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorDetail.records.notCollected') }}</p>
    <template v-else>
      <p v-if="data.dropped_attempts" class="mt-3 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.ops.errorDetail.records.dropped', { count: data.dropped_attempts }) }}</p>
      <div class="mt-4 space-y-4">
        <div v-for="section in sections" :key="section.key" class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800" :data-testid="section.key">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h4 class="text-sm font-bold text-gray-900 dark:text-white">{{ section.label }}</h4>
            <button v-if="section.payload.body" type="button" class="text-xs font-medium text-primary-600 hover:text-primary-500" @click="copyToClipboard(pretty(section.payload.body))">{{ t('common.copy') }}</button>
          </div>
          <p v-if="section.info" class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ section.info }}</p>
          <p class="mt-2 break-all font-mono text-xs text-gray-500 dark:text-gray-400">{{ [section.payload.method, section.payload.path, section.payload.content_type].filter(Boolean).join(' · ') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.errorDetail.records.bytes', { count: Math.max(0, section.payload.bytes || 0) }) }}</p>
          <p v-if="section.payload.truncated" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.ops.errorDetail.records.truncated') }}</p>
          <pre v-if="section.payload.body" class="mt-3 max-h-[400px] overflow-auto rounded-lg bg-gray-50 p-3 text-xs text-gray-800 dark:bg-dark-900 dark:text-gray-100"><code>{{ pretty(section.payload.body) }}</code></pre>
          <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">{{ omission(section.payload.omitted_reason) }}</p>
        </div>
      </div>
    </template>
  </section>
</template>
