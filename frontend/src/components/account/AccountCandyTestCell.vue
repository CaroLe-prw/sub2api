<template>
  <div v-if="account.platform === 'openai' && (account.type === 'oauth' || account.type === 'apikey')" class="flex flex-col items-start gap-1" data-testid="account-candy-test">
    <button
      type="button"
      class="inline-flex items-center gap-1 text-xs font-medium disabled:cursor-not-allowed focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
      :class="statusClass"
      :disabled="running || !account.schedulable"
      :title="t(account.schedulable ? 'admin.accounts.candyTest.hint' : 'admin.accounts.candyTest.schedulingDisabled')"
      :aria-label="t('admin.accounts.candyTest.run', { name: account.name })"
      :aria-busy="running"
      @click.stop="run"
    >
      <span class="h-1.5 w-1.5 rounded-full bg-current" :class="{ 'animate-pulse': running }" aria-hidden="true" />
      <span aria-live="polite">{{ t('admin.accounts.candyTest.title') }} · {{ statusLabel }}</span>
    </button>
    <span v-if="!account.schedulable" class="text-xs text-gray-500 dark:text-gray-400" data-testid="candy-test-disabled">
      {{ t('admin.accounts.candyTest.schedulingDisabled') }}
    </span>
    <span v-if="consecutiveFailures === 1" class="text-xs text-amber-600 dark:text-amber-400" data-testid="candy-first-failure">
      {{ t('admin.accounts.candyTest.firstFailure') }}
    </span>
    <span v-if="schedulingBlocked" class="text-xs text-red-600 dark:text-red-400" :title="t('admin.accounts.candyTest.blockedHint')" data-testid="candy-scheduling-blocked">
      {{ t('admin.accounts.candyTest.blocked') }}
    </span>
    <template v-if="snapshot">
      <span class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400" data-testid="candy-test-counts">
        {{ t('admin.accounts.candyTest.counts', { passed: snapshot.passed, total: snapshot.passed + snapshot.failed, errors: snapshot.errors }) }}
      </span>
      <div class="flex gap-0.5" :aria-label="t('admin.accounts.candyTest.history')">
        <button
          v-for="(sample, index) in snapshot.recent"
          :key="`${sample.checked_at}-${index}`"
          type="button"
          class="flex h-4 w-3 items-center justify-center rounded hover:bg-gray-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:bg-dark-700"
          :title="t('admin.accounts.candyTest.viewRecord', { details: sampleTitle(sample) })"
          :aria-label="t('admin.accounts.candyTest.viewRecord', { details: sampleTitle(sample) })"
          :data-status="sample.status"
          @click.stop="openRecord(sample)"
        >
          <span class="h-1.5 w-2 rounded-sm" :class="sample.status === 'passed' ? 'bg-emerald-500' : sample.status === 'failed' ? 'bg-red-500' : 'bg-amber-400'" aria-hidden="true" />
        </button>
      </div>
    </template>
    <BaseDialog
      v-if="selectedSample"
      :show="detailsOpen"
      :title="t('admin.accounts.candyTest.detailsTitle')"
      width="wide"
      close-on-click-outside
      @close="detailsOpen = false"
      @after-leave="selectedSample = null"
    >
      <div class="space-y-4" data-testid="candy-record-details">
        <p class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ account.name }}</p>
        <p class="break-words text-xs text-gray-500 dark:text-gray-400">{{ sampleTitle(selectedSample) }}</p>
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.accounts.candyTest.answerRule') }}</p>
        <template v-if="selectedSample.response_text">
          <h4 class="text-sm font-medium text-gray-900 dark:text-gray-100">{{ t('admin.accounts.candyTest.modelAnswer') }}</h4>
          <pre class="max-h-96 overflow-y-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-3 text-sm text-gray-800 dark:bg-dark-900 dark:text-gray-200" data-testid="candy-model-answer">{{ selectedSample.response_text }}</pre>
          <p v-if="selectedSample.response_truncated" class="text-xs text-amber-600 dark:text-amber-400" data-testid="candy-response-truncated">{{ t('admin.accounts.candyTest.answerTruncated') }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.candyTest.answerPrivacy') }}</p>
        </template>
        <p v-else class="text-sm text-gray-500 dark:text-gray-400" data-testid="candy-answer-unavailable">{{ t(selectedSample.status === 'error' ? 'admin.accounts.candyTest.noAnswerOnError' : 'admin.accounts.candyTest.answerNotRecorded') }}</p>
      </div>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { runCandyTest } from '@/api/admin/accounts'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode } from '@/utils/apiError'
import type { Account } from '@/types'
import type { CandyTestSample, CandyTestSnapshot } from '@/types/candyTest'

const props = defineProps<{ account: Account; groupId?: number }>()
const emit = defineEmits<{ updated: [snapshot: CandyTestSnapshot] }>()
const { t } = useI18n()
const appStore = useAppStore()
const running = ref(false)
const detailsOpen = ref(false)
const selectedSample = ref<CandyTestSample | null>(null)
const openRecord = (sample: CandyTestSample) => {
  selectedSample.value = sample
  detailsOpen.value = true
}
const localResult = ref<CandyTestSnapshot | null>(null)
let controller: AbortController | null = null
const lastSample = (value?: CandyTestSnapshot | null) => value?.recent[value.recent.length - 1]
const snapshot = computed(() => {
  const persisted = props.account.extra?.candy_test
  if (!localResult.value) return persisted
  if (!persisted) return localResult.value
  // A list refresh started before the test may contain an older snapshot.
  return Date.parse(lastSample(persisted)?.checked_at ?? '') >= Date.parse(lastSample(localResult.value)?.checked_at ?? '')
    ? persisted : localResult.value
})
const consecutiveFailures = computed(() => {
  const value = snapshot.value
  if (!value) return 0
  if ((value.consecutive_failures ?? 0) > 0) return value.consecutive_failures!
  const verdict = value.last_verdict ?? [...value.recent].reverse().find(sample => sample.status !== 'error')?.status
  if (verdict !== 'failed') return 0
  let count = 0
  for (const sample of [...value.recent].reverse()) {
    if (sample.status === 'passed') break
    if (sample.status === 'failed') count++
  }
  return Math.max(1, count)
})
const schedulingBlocked = computed(() => consecutiveFailures.value >= 2)
const status = computed(() => running.value ? 'running' : lastSample(snapshot.value)?.status ?? 'untested')
const statusLabel = computed(() => t(`admin.accounts.candyTest.${status.value}`))
const statusClass = computed(() => ({
  passed: 'text-emerald-600 dark:text-emerald-400',
  failed: 'text-red-600 dark:text-red-400',
  error: 'text-amber-600 dark:text-amber-400',
  running: 'text-primary-600 dark:text-primary-400',
  untested: 'text-gray-500 dark:text-gray-400'
})[status.value])
const sampleTitle = (sample: CandyTestSample) =>
  `${t(`admin.accounts.candyTest.${sample.status}`)} · ${sample.model}${sample.reasoning_effort ? ` · ${sample.reasoning_effort}` : ''} · ${new Date(sample.checked_at).toLocaleString()} · ${(sample.latency_ms / 1000).toFixed(1)}s`

async function run() {
  if (running.value || !props.account.schedulable || props.account.platform !== 'openai' || (props.account.type !== 'oauth' && props.account.type !== 'apikey')) return
  const id = props.account.id
  const request = new AbortController()
  controller = request
  running.value = true
  try {
    const result = await runCandyTest(id, props.groupId, request.signal)
    if (request.signal.aborted || props.account.id !== id) return
    localResult.value = result
    emit('updated', result)
  } catch (error) {
    if (!request.signal.aborted) {
      const code = extractApiErrorCode(error)
      const key = code === 'CANDY_TEST_SCHEDULING_DISABLED' ? 'schedulingDisabled'
        : code === 'CANDY_TEST_GROUP_REQUIRED' ? 'groupRequired'
          : code === 'CANDY_TEST_MODEL_NOT_ALLOWED' ? 'modelNotAllowed' : 'requestFailed'
      appStore.showError(t(`admin.accounts.candyTest.${key}`))
    }
  } finally {
    if (controller === request) {
      running.value = false
      controller = null
    }
  }
}
watch(() => props.account.id, () => {
  detailsOpen.value = false
  selectedSample.value = null
  controller?.abort()
  controller = null
  running.value = false
  localResult.value = null
})
onBeforeUnmount(() => controller?.abort())
</script>
