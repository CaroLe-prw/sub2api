<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { Icon } from '@/components/icons'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'
import type { TokenAuditProgressEvent, TokenAuditResult, TokenAuditSample, TokenAuditFit } from '@/api/admin/accounts'
import TokenAuditResultTable from './TokenAuditResultTable.vue'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()

const status = shallowRef<'idle' | 'running' | 'success' | 'error' | 'cancelled'>('idle')
const modelId = shallowRef('gpt-5.6-sol')
const result = shallowRef<TokenAuditResult | null>(null)
const errorMessage = shallowRef('')
const currentSample = shallowRef('')
const progressTotal = shallowRef(6)
const progressCompleted = shallowRef(0)
const retryingSample = shallowRef('')
const auditAbortController = shallowRef<AbortController | null>(null)

const supported = computed(() => props.account?.platform === 'openai' && props.account?.type === 'apikey')
const canRun = computed(() => Boolean(props.account && supported.value && status.value !== 'running'))
const statusClass = computed(() => {
  if (result.value?.overall_status === 'suspicious') return 'text-amber-600 dark:text-amber-400'
  if (result.value?.overall_status === 'normal') return 'text-emerald-600 dark:text-emerald-400'
  return 'text-gray-500 dark:text-gray-400'
})

watch(() => props.show, (visible) => {
  if (visible) {
    status.value = 'idle'
    result.value = null
    errorMessage.value = ''
    currentSample.value = ''
    progressCompleted.value = 0
    modelId.value = 'gpt-5.6-sol'
  }
})

const close = () => {
  if (status.value === 'running') {
    auditAbortController.value?.abort()
    return
  }
  emit('close')
}

const cancelAudit = () => {
  auditAbortController.value?.abort()
}

const runAudit = async () => {
  if (!props.account || !canRun.value) return
  status.value = 'running'
  const abortController = new AbortController()
  auditAbortController.value = abortController
  result.value = null
  errorMessage.value = ''
  currentSample.value = ''
  progressCompleted.value = 0
  try {
    await adminAPI.accounts.runTokenAuditStream(props.account.id, modelId.value.trim() || 'gpt-5.6-sol', handleProgressEvent, abortController.signal)
    if (abortController.signal.aborted) {
      currentSample.value = ''
      status.value = 'cancelled'
      return
    }
    const finalResult = result.value as TokenAuditResult | null
    if (finalResult) status.value = finalResult.overall_status === 'suspicious' ? 'error' : 'success'
  } catch (error: any) {
    if (abortController.signal.aborted) {
      currentSample.value = ''
      status.value = 'cancelled'
      return
    }
    status.value = 'error'
    errorMessage.value = error?.response?.data?.message || error?.message || t('admin.accounts.tokenAudit.failed')
  } finally {
    if (auditAbortController.value === abortController) auditAbortController.value = null
  }
}

const makePartialResult = (): TokenAuditResult => ({
  account_id: props.account?.id || 0,
  model_id: modelId.value.trim() || 'gpt-5.6-sol',
  tokenizer_name: 'tiktoken-go',
  tokenizer_version: 'v0.8.0',
  tokenizer_encoding: 'o200k_base',
  tokenizer_exact_match: false,
  samples: [],
  variable_growth_status: 'insufficient_evidence',
  output_cap_status: 'insufficient_evidence',
  fixed_context_status: 'insufficient_evidence',
  overall_status: 'insufficient_evidence',
  completed: 0,
})

const handleProgressEvent = (event: TokenAuditProgressEvent) => {
  if (event.total) progressTotal.value = event.total
  if (event.type === 'started') {
    result.value = makePartialResult()
    return
  }
  if (event.type === 'sample_started') {
    currentSample.value = event.name || ''
    if (event.completed !== undefined) progressCompleted.value = event.completed
    if (result.value && event.name && !result.value.samples.some(sample => sample.name === event.name)) {
      result.value = { ...result.value, samples: [...result.value.samples, {
        name: event.name,
        kind: event.name.startsWith('code') ? 'code' : event.name.startsWith('chinese') ? 'chinese' : 'english',
        local_tokens: event.local_tokens || 0,
        sha256: '',
        request_id_present: false,
        response_id_present: false,
        account_header_present: false,
        channel_header_present: false,
        upstream_header_present: false,
        transport_error: false,
        timed_out: false,
        json_parsed: false,
      }] }
    }
    return
  }
  if (event.type === 'sample_finished' && event.sample && result.value) {
    progressCompleted.value = event.completed || 0
    currentSample.value = ''
    const samples = result.value.samples.map(sample => sample.name === event.sample?.name ? event.sample : sample)
    result.value = { ...result.value, samples, completed: progressCompleted.value }
    return
  }
  if (event.type === 'completed' && event.result) {
    progressCompleted.value = event.result.completed
    currentSample.value = ''
    result.value = event.result
  }
}

const fitSamples = (samples: TokenAuditSample[], englishOnly: boolean): TokenAuditFit | undefined => {
  const points = samples.filter(sample => (!englishOnly || sample.kind === 'english') && sample.input_tokens !== undefined)
  if (points.length < 2) return undefined
  const xm = points.reduce((sum, point) => sum + point.local_tokens, 0) / points.length
  const ym = points.reduce((sum, point) => sum + (point.input_tokens || 0), 0) / points.length
  const sxx = points.reduce((sum, point) => sum + (point.local_tokens - xm) ** 2, 0)
  if (!sxx) return undefined
  const slope = points.reduce((sum, point) => sum + (point.local_tokens - xm) * ((point.input_tokens || 0) - ym), 0) / sxx
  const intercept = ym - slope * xm
  const residual = points.reduce((sum, point) => sum + ((point.input_tokens || 0) - intercept - slope * point.local_tokens) ** 2, 0)
  const total = points.reduce((sum, point) => sum + ((point.input_tokens || 0) - ym) ** 2, 0)
  return { intercept, slope, r2: total > 0 ? 1 - residual / total : 1, sample_count: points.length, confidence_limited: points.length < 6 || residual === 0 }
}

const recalculateResult = (source: TokenAuditResult): TokenAuditResult => {
  const allFit = fitSamples(source.samples, false)
  const englishFit = fitSamples(source.samples, true)
  const samples = source.samples.map(sample => allFit && sample.input_tokens !== undefined ? {
    ...sample,
    difference_tokens: sample.input_tokens - sample.local_tokens,
    variable_tokens: sample.input_tokens - allFit.intercept,
  } : sample)
  const next = { ...source, samples, all_fit: allFit, english_fit: englishFit, fixed_context_estimate: allFit?.intercept, variable_amplification: allFit?.slope }
  const english256 = samples.find(sample => sample.kind === 'english' && sample.local_tokens === 256 && sample.input_tokens !== undefined)?.input_tokens
  if (english256 !== undefined) {
    const code256 = samples.find(sample => sample.kind === 'code' && sample.input_tokens !== undefined && sample.local_tokens === 256)?.input_tokens
    const chinese256 = samples.find(sample => sample.kind === 'chinese' && sample.input_tokens !== undefined && sample.local_tokens === 256)?.input_tokens
    next.code_extra_tokens = code256 === undefined ? undefined : code256 - english256
    next.chinese_extra_tokens = chinese256 === undefined ? undefined : chinese256 - english256
  }
  next.variable_growth_status = allFit && allFit.slope >= 0.9 && allFit.slope <= 1.1 && allFit.r2 >= 0.98 ? 'normal' : allFit ? 'suspicious' : 'insufficient_evidence'
  next.fixed_context_status = allFit ? 'evidence_only' : 'insufficient_evidence'
  next.output_cap_status = samples.some(sample => sample.output_tokens !== undefined && sample.output_tokens > 1) ? 'suspicious' : samples.some(sample => sample.output_tokens !== undefined) ? 'evidence_only' : 'insufficient_evidence'
  next.overall_status = next.output_cap_status === 'suspicious' || next.variable_growth_status === 'suspicious'
    ? 'suspicious'
    : next.variable_growth_status === 'normal' && next.completed === progressTotal.value ? 'normal' : 'insufficient_evidence'
  return next
}

const retrySample = async (sample: TokenAuditSample) => {
  if (!props.account || retryingSample.value) return
  retryingSample.value = sample.name
  try {
    const updated = await adminAPI.accounts.retryTokenAuditSample(props.account.id, modelId.value.trim() || 'gpt-5.6-sol', sample.name)
    if (result.value) result.value = recalculateResult({ ...result.value, samples: result.value.samples.map(item => item.name === updated.name ? updated : item) })
  } catch (error: any) {
    errorMessage.value = error?.response?.data?.message || error?.message || t('admin.accounts.tokenAudit.failed')
  } finally {
    retryingSample.value = ''
  }
}
</script>

<template>
  <BaseDialog :show="show" :title="t('admin.accounts.tokenAudit.title')" width="wide" @close="close">
    <div class="space-y-4">
      <div class="flex items-center justify-between rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-700">
        <div class="flex min-w-0 items-center gap-3">
          <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary-100 text-primary-700 dark:bg-primary-500/20 dark:text-primary-300">
            <Icon name="shield" size="sm" :stroke-width="2" />
          </div>
          <div class="min-w-0">
            <div class="truncate font-medium text-gray-900 dark:text-gray-100">{{ account?.name || '-' }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ account?.platform }} / {{ account?.type }}</div>
          </div>
        </div>
        <span :class="['text-sm font-semibold', statusClass]">
          {{ status === 'cancelled' ? t('admin.accounts.tokenAudit.cancelled') : result?.overall_status || t('admin.accounts.tokenAudit.ready') }}
        </span>
      </div>

      <div v-if="!supported" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200">
        {{ t('admin.accounts.tokenAudit.unsupported') }}
      </div>

      <div class="flex items-end gap-3">
        <label class="min-w-0 flex-1 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.tokenAudit.model') }}
          <input v-model="modelId" :disabled="status === 'running'" class="mt-1 block w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 outline-none focus:border-primary-500 dark:border-dark-500 dark:bg-dark-800 dark:text-gray-100" />
        </label>
        <button type="button" class="btn btn-primary btn-sm shrink-0" :disabled="!canRun" @click="runAudit">
          <Icon v-if="status === 'running'" name="refresh" size="sm" class="animate-spin" :stroke-width="2" />
          <Icon v-else name="play" size="sm" :stroke-width="2" />
          {{ status === 'running' ? t('admin.accounts.tokenAudit.running') : t('admin.accounts.tokenAudit.run') }}
        </button>
        <button v-if="status === 'running'" type="button" class="btn btn-secondary btn-sm shrink-0" @click="cancelAudit">
          <Icon name="x" size="sm" :stroke-width="2" />
          {{ t('admin.accounts.tokenAudit.cancel') }}
        </button>
      </div>

      <div v-if="status === 'running'" class="space-y-1">
        <div class="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
          <span>{{ t('admin.accounts.tokenAudit.progress') }} {{ progressCompleted }}/{{ progressTotal }}</span>
          <span>{{ currentSample || t('admin.accounts.tokenAudit.preparing') }}</span>
        </div>
        <div class="h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
          <div class="h-full bg-primary-500 transition-all" :style="{ width: `${Math.round(progressCompleted / progressTotal * 100)}%` }" />
        </div>
      </div>

      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
        {{ errorMessage }}
      </div>

      <div v-if="result?.stopped_reason" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200">
        {{ t('admin.accounts.tokenAudit.stoppedReason') }}: {{ result.stopped_reason }}
      </div>

      <template v-if="result">
        <div class="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
          <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.tokenAudit.fixedContextLength') }}</div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ result.fixed_context_estimate?.toFixed(1) ?? '-' }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.tokenAudit.slope') }}</div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ result.all_fit?.slope?.toFixed(3) ?? '-' }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">R²</div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ result.all_fit?.r2?.toFixed(3) ?? '-' }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.tokenAudit.completed') }}</div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ result.completed }}/6</div>
          </div>
        </div>

        <div class="flex flex-wrap gap-x-5 gap-y-1 text-xs text-gray-600 dark:text-gray-300">
          <span>{{ t('admin.accounts.tokenAudit.tokenizer') }}: {{ result.tokenizer_encoding }} / {{ result.tokenizer_version }}</span>
          <span>{{ t('admin.accounts.tokenAudit.variableGrowth') }}: {{ result.variable_growth_status }}</span>
          <span>{{ t('admin.accounts.tokenAudit.fixedContextStatus') }}: {{ result.fixed_context_status }}</span>
          <span>{{ t('admin.accounts.tokenAudit.outputCap') }}: {{ result.output_cap_status }}</span>
          <span>{{ t('admin.accounts.tokenAudit.englishFit') }}: {{ result.english_fit?.slope?.toFixed(3) ?? '-' }} / R² {{ result.english_fit?.r2?.toFixed(3) ?? '-' }}</span>
          <span>{{ t('admin.accounts.tokenAudit.codeExtra') }}: {{ result.code_extra_tokens?.toFixed(1) ?? '-' }}</span>
          <span>{{ t('admin.accounts.tokenAudit.chineseExtra') }}: {{ result.chinese_extra_tokens?.toFixed(1) ?? '-' }}</span>
        </div>
        <TokenAuditResultTable :result="result" :retrying-sample="retryingSample" @retry="retrySample" />
      </template>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="status === 'running'" @click="close">
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>
</template>
