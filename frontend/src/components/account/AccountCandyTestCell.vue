<template>
  <div v-if="account.platform === 'openai' && account.type === 'oauth'" class="flex flex-col items-start gap-1" data-testid="account-candy-test">
    <button
      type="button"
      class="inline-flex items-center gap-1 text-xs font-medium disabled:cursor-wait focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
      :class="statusClass"
      :disabled="running"
      :title="t('admin.accounts.candyTest.hint')"
      :aria-label="t('admin.accounts.candyTest.run', { name: account.name })"
      :aria-busy="running"
      @click.stop="run"
    >
      <span class="h-1.5 w-1.5 rounded-full bg-current" :class="{ 'animate-pulse': running }" aria-hidden="true" />
      <span aria-live="polite">{{ t('admin.accounts.candyTest.title') }} · {{ statusLabel }}</span>
    </button>
    <template v-if="snapshot">
      <span class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400" data-testid="candy-test-counts">
        {{ t('admin.accounts.candyTest.counts', { passed: snapshot.passed, total: snapshot.total, errors: snapshot.errors }) }}
      </span>
      <div class="flex gap-0.5" :aria-label="t('admin.accounts.candyTest.history')">
        <span
          v-for="(sample, index) in snapshot.recent"
          :key="`${sample.checked_at}-${index}`"
          class="h-1.5 w-2 rounded-sm"
          :class="sample.status === 'passed' ? 'bg-emerald-500' : sample.status === 'failed' ? 'bg-red-500' : 'bg-amber-400'"
          :title="sampleTitle(sample)"
          role="img"
          :aria-label="sampleTitle(sample)"
          :data-status="sample.status"
        />
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { runCandyTest } from '@/api/admin/accounts'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode } from '@/utils/apiError'
import type { Account } from '@/types'
import type { CandyTestSample, CandyTestSnapshot } from '@/types/candyTest'

const props = defineProps<{ account: Account; groupId?: number }>()
const emit = defineEmits<{ updated: [snapshot: CandyTestSnapshot] }>()
const { t } = useI18n()
const appStore = useAppStore()
const running = ref(false)
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
  `${t(`admin.accounts.candyTest.${sample.status}`)} · ${sample.model} · ${new Date(sample.checked_at).toLocaleString()} · ${(sample.latency_ms / 1000).toFixed(1)}s`

async function run() {
  if (running.value || props.account.platform !== 'openai' || props.account.type !== 'oauth') return
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
      const key = extractApiErrorCode(error) === 'CANDY_TEST_GROUP_REQUIRED' ? 'groupRequired' : 'requestFailed'
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
  controller?.abort()
  controller = null
  running.value = false
  localResult.value = null
})
onBeforeUnmount(() => controller?.abort())
</script>
