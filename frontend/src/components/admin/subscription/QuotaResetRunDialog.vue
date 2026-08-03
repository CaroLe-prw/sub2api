<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  QuotaResetPolicy,
  QuotaResetWindowStartMode,
  SubscriptionQuotaResetRunRequest
} from '@/api/admin/quotaReset'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import QuotaResetPolicyFields from './QuotaResetPolicyFields.vue'

const props = defineProps<{
  show: boolean
  groups: AdminGroup[]
  loading: boolean
}>()

const emit = defineEmits<{
  close: []
  submit: [request: SubscriptionQuotaResetRunRequest]
}>()

const { t } = useI18n()

function defaultRequest(): SubscriptionQuotaResetRunRequest {
  return {
    group_ids: [],
    daily: true,
    weekly: false,
    monthly: false,
    window_start_mode: 'current',
    restart_schedule: true
  }
}

const form = reactive<SubscriptionQuotaResetRunRequest>(defaultRequest())

const policy = computed<QuotaResetPolicy>({
  get: () => ({
    daily: form.daily,
    weekly: form.weekly,
    monthly: form.monthly,
    window_start_mode: form.window_start_mode
  }),
  set: (value) => Object.assign(form, value)
})

const selectedGroups = computed(() =>
  props.groups.filter((group) => form.group_ids.includes(group.id))
)

const selectedWindows = computed(() => {
  const labels: string[] = []
  if (form.daily) labels.push(t('admin.quotaReset.policy.daily'))
  if (form.weekly) labels.push(t('admin.quotaReset.policy.weekly'))
  if (form.monthly) labels.push(t('admin.quotaReset.policy.monthly'))
  return labels
})

const modeLabelKeys: Record<QuotaResetWindowStartMode, string> = {
  current: 'admin.quotaReset.policy.modes.current',
  natural_day: 'admin.quotaReset.policy.modes.naturalDay',
  preserve: 'admin.quotaReset.policy.modes.preserve'
}

const canSubmit = computed(
  () => form.group_ids.length > 0 && selectedWindows.value.length > 0 && !props.loading
)

watch(
  () => props.show,
  (show) => {
    if (show) Object.assign(form, defaultRequest())
  }
)

function close() {
  if (!props.loading) emit('close')
}

function submit() {
  if (!canSubmit.value) return
  emit('submit', { ...form, group_ids: [...form.group_ids] })
}
</script>

<template>
  <BaseDialog
    :show="show"
    :title="t('admin.quotaReset.runConfirmTitle')"
    width="wide"
    :close-on-escape="!loading"
    :show-close-button="!loading"
    @close="close"
  >
    <form id="quota-reset-run-form" class="space-y-6" @submit.prevent="submit">
      <div
        class="rounded-lg border border-blue-200 bg-blue-50 p-3 text-sm text-blue-800 dark:border-blue-500/30 dark:bg-blue-500/10 dark:text-blue-300"
      >
        {{ t('admin.quotaReset.manualDescription') }}
      </div>

      <fieldset :disabled="loading" class="space-y-6">
        <div>
          <GroupSelector v-model="form.group_ids" :groups="groups" searchable />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.quotaReset.groupsHint') }}
          </p>
          <p v-if="form.group_ids.length === 0" class="mt-1 text-xs text-red-600 dark:text-red-400">
            {{ t('admin.quotaReset.selectGroup') }}
          </p>
        </div>

        <QuotaResetPolicyFields v-model="policy" id-prefix="manual-reset" />
        <p v-if="selectedWindows.length === 0" class="text-xs text-red-600 dark:text-red-400">
          {{ t('admin.quotaReset.selectWindow') }}
        </p>

        <div role="radiogroup" aria-labelledby="manual-reset-schedule-mode" class="space-y-3">
          <p id="manual-reset-schedule-mode" class="text-sm font-medium text-gray-800 dark:text-gray-100">
            {{ t('admin.quotaReset.manualScheduleMode') }}
          </p>
          <label class="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <input
              v-model="form.restart_schedule"
              type="radio"
              name="manual-reset-schedule-mode"
              :value="true"
              class="mt-0.5 h-4 w-4 border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            />
            <span>
              <span class="block text-sm font-medium text-gray-800 dark:text-gray-100">
                {{ t('admin.quotaReset.manualRestartSchedule') }}
              </span>
              <span class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.quotaReset.manualRestartScheduleHint') }}
              </span>
            </span>
          </label>
          <label class="flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <input
              v-model="form.restart_schedule"
              data-test="quota-reset-keep-schedule"
              type="radio"
              name="manual-reset-schedule-mode"
              :value="false"
              class="mt-0.5 h-4 w-4 border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500"
            />
            <span>
              <span class="block text-sm font-medium text-gray-800 dark:text-gray-100">
                {{ t('admin.quotaReset.manualKeepSchedule') }}
              </span>
              <span class="mt-0.5 block text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.quotaReset.manualKeepScheduleHint') }}
              </span>
            </span>
          </label>
        </div>
      </fieldset>

      <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800">
        <p class="text-sm font-medium text-gray-900 dark:text-gray-100">
          {{ t('admin.quotaReset.manualSummaryTitle') }}
        </p>
        <dl class="mt-3 grid grid-cols-[auto,1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.users.groups') }}</dt>
          <dd class="text-right text-gray-900 dark:text-gray-100">
            {{ selectedGroups.map((group) => group.name).join(', ') || t('admin.quotaReset.manualNoGroups') }}
          </dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.policy.windows') }}</dt>
          <dd class="text-right text-gray-900 dark:text-gray-100">
            {{ selectedWindows.join(' / ') || '-' }}
          </dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.policy.windowMode') }}</dt>
          <dd class="text-right text-gray-900 dark:text-gray-100">
            {{ t(modeLabelKeys[form.window_start_mode]) }}
          </dd>
          <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.quotaReset.manualScheduleMode') }}</dt>
          <dd class="text-right text-gray-900 dark:text-gray-100">
            {{ t(form.restart_schedule ? 'admin.quotaReset.manualRestartSchedule' : 'admin.quotaReset.manualKeepSchedule') }}
          </dd>
        </dl>
      </div>

      <p class="text-xs text-amber-700 dark:text-amber-300">
        {{ t('admin.quotaReset.manualAnnouncementHint') }}
      </p>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button
          data-test="quota-reset-run-submit"
          type="submit"
          form="quota-reset-run-form"
          class="btn btn-primary"
          :disabled="!canSubmit"
        >
          {{ loading ? t('admin.quotaReset.running') : t('admin.quotaReset.runNow') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>
