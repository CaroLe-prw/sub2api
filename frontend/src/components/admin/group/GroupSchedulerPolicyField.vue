<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type {
  OpenAISchedulerConfig,
  OpenAISchedulerProfile,
} from '@/types'

import { OPENAI_SCHEDULER_PROFILES } from './groupSchedulerPolicy'

const profile = defineModel<OpenAISchedulerProfile>('profile', {
  required: true,
})
const config = defineModel<OpenAISchedulerConfig>('config', {
  required: true,
})

const { t } = useI18n()

type SchedulerNumericKey = keyof Pick<
  OpenAISchedulerConfig,
  | 'top_k'
  | 'priority'
  | 'load'
  | 'queue'
  | 'error_rate'
  | 'ttft'
  | 'reset'
  | 'quota_headroom'
  | 'upstream_cost'
  | 'previous_response'
  | 'session_sticky'
>

const profileOptions = computed(() =>
  OPENAI_SCHEDULER_PROFILES.map((value) => ({
    value,
    label: t(`admin.groups.scheduler.profiles.${value}.label`),
  })),
)

const weightFields = computed<
  Array<{
    key: Exclude<SchedulerNumericKey, 'top_k'>
    label: string
    defaultValue: number
  }>
>(() => [
  {
    key: 'priority',
    label: t('admin.groups.scheduler.weights.priority'),
    defaultValue: 0.5,
  },
  {
    key: 'load',
    label: t('admin.groups.scheduler.weights.load'),
    defaultValue: 1.5,
  },
  {
    key: 'queue',
    label: t('admin.groups.scheduler.weights.queue'),
    defaultValue: 1.5,
  },
  {
    key: 'error_rate',
    label: t('admin.groups.scheduler.weights.errorRate'),
    defaultValue: 4,
  },
  {
    key: 'ttft',
    label: t('admin.groups.scheduler.weights.ttft'),
    defaultValue: 2.5,
  },
  {
    key: 'reset',
    label: t('admin.groups.scheduler.weights.reset'),
    defaultValue: 0.2,
  },
  {
    key: 'quota_headroom',
    label: t('admin.groups.scheduler.weights.quotaHeadroom'),
    defaultValue: 0.8,
  },
  {
    key: 'upstream_cost',
    label: t('admin.groups.scheduler.weights.upstreamCost'),
    defaultValue: 1.5,
  },
  {
    key: 'previous_response',
    label: t('admin.groups.scheduler.weights.previousResponse'),
    defaultValue: 0.3,
  },
  {
    key: 'session_sticky',
    label: t('admin.groups.scheduler.weights.sessionSticky'),
    defaultValue: 0.1,
  },
])

const profileDescription = computed(() =>
  t(`admin.groups.scheduler.profiles.${profile.value}.description`),
)

function updateNumber(
  key: SchedulerNumericKey,
  event: Event,
  minimum: number,
) {
  const input = event.target as HTMLInputElement
  if (input.value.trim() === '') {
    config.value = {
      ...config.value,
      [key]: null,
    }
    return
  }
  const value = input.valueAsNumber
  if (!Number.isFinite(value) || value < minimum) {
    return
  }
  config.value = {
    ...config.value,
    [key]: value,
  }
}

function updateBoolean(
  key:
    | 'sticky_weighted_enabled'
    | 'subscription_priority_enabled',
  event: Event,
) {
  config.value = {
    ...config.value,
    [key]: (event.target as HTMLInputElement).checked,
  }
}
</script>

<template>
  <section class="col-span-full border-t border-gray-200 pt-4 dark:border-gray-700">
    <label class="input-label">
      {{ t('admin.groups.scheduler.title') }}
    </label>
    <select v-model="profile" class="input">
      <option
        v-for="option in profileOptions"
        :key="option.value"
        :value="option.value"
      >
        {{ option.label }}
      </option>
    </select>
    <p class="input-hint">{{ profileDescription }}</p>

    <div
      v-if="profile === 'custom'"
      class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3"
    >
      <div>
        <label class="input-label">
          {{ t('admin.groups.scheduler.weights.topK') }}
        </label>
        <input
          :value="config.top_k ?? ''"
          :placeholder="t('admin.groups.scheduler.defaultPlaceholder', { value: 4 })"
          type="number"
          min="1"
          step="1"
          class="input"
          @input="updateNumber('top_k', $event, 1)"
        />
      </div>

      <div v-for="field in weightFields" :key="field.key">
        <label class="input-label">{{ field.label }}</label>
        <input
          :value="config[field.key] ?? ''"
          :placeholder="
            t('admin.groups.scheduler.defaultPlaceholder', {
              value: field.defaultValue,
            })
          "
          type="number"
          min="0"
          step="any"
          class="input"
          @input="updateNumber(field.key, $event, 0)"
        />
      </div>

      <label class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
        <input
          :checked="config.sticky_weighted_enabled"
          type="checkbox"
          class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          @change="updateBoolean('sticky_weighted_enabled', $event)"
        />
        <span>
          <span class="block font-medium">
            {{ t('admin.groups.scheduler.stickyWeighted') }}
          </span>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.groups.scheduler.stickyWeightedHint') }}
          </span>
        </span>
      </label>

      <label class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
        <input
          :checked="config.subscription_priority_enabled"
          type="checkbox"
          class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
          @change="updateBoolean('subscription_priority_enabled', $event)"
        />
        <span>
          <span class="block font-medium">
            {{ t('admin.groups.scheduler.subscriptionPriority') }}
          </span>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.groups.scheduler.subscriptionPriorityHint') }}
          </span>
        </span>
      </label>
    </div>
  </section>
</template>
