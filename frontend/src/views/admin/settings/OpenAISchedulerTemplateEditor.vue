<script setup lang="ts">
import { computed, ref } from "vue";
import { useI18n } from "vue-i18n";

import Toggle from "@/components/common/Toggle.vue";
import type {
  OpenAISchedulerTemplate,
  OpenAISchedulerTemplateProfile,
  OpenAISchedulerTemplates,
} from "@/api/admin/settings";

const templates = defineModel<OpenAISchedulerTemplates>({ required: true });
const { t } = useI18n();

const activeProfile = ref<OpenAISchedulerTemplateProfile>("sla");

type NumericKey = Exclude<keyof OpenAISchedulerTemplate, "sticky_weighted_enabled" | "subscription_priority_enabled">;

const numericFields = computed<Array<{ key: NumericKey; label: string }>>(() => [
  { key: "top_k", label: t("admin.settings.openaiSchedulerTemplates.topK") },
  { key: "priority", label: t("admin.settings.openaiSchedulerTemplates.priority") },
  { key: "load", label: t("admin.settings.openaiSchedulerTemplates.load") },
  { key: "queue", label: t("admin.settings.openaiSchedulerTemplates.queue") },
  { key: "error_rate", label: t("admin.settings.openaiSchedulerTemplates.errorRate") },
  { key: "ttft", label: t("admin.settings.openaiSchedulerTemplates.ttft") },
  { key: "reset", label: t("admin.settings.openaiSchedulerTemplates.reset") },
  { key: "quota_headroom", label: t("admin.settings.openaiSchedulerTemplates.quotaHeadroom") },
  { key: "upstream_cost", label: t("admin.settings.openaiSchedulerTemplates.upstreamCost") },
  { key: "previous_response", label: t("admin.settings.openaiSchedulerTemplates.previousResponse") },
  { key: "session_sticky", label: t("admin.settings.openaiSchedulerTemplates.sessionSticky") },
]);

const profileOptions = computed<Array<{ value: OpenAISchedulerTemplateProfile; label: string }>>(() => [
  { value: "sla", label: t("admin.settings.openaiSchedulerTemplates.profiles.sla") },
  { value: "balanced", label: t("admin.settings.openaiSchedulerTemplates.profiles.balanced") },
  { value: "cost", label: t("admin.settings.openaiSchedulerTemplates.profiles.cost") },
]);

const activeTemplate = computed(() => templates.value[activeProfile.value]);

function updateTemplate<K extends keyof OpenAISchedulerTemplate>(
  key: K,
  value: OpenAISchedulerTemplate[K],
) {
  templates.value = {
    ...templates.value,
    [activeProfile.value]: {
      ...activeTemplate.value,
      [key]: value,
    },
  };
}

function updateNumber(key: NumericKey, event: Event) {
  const input = event.target as HTMLInputElement;
  const value = Number(input.value);
  const minimum = key === "top_k" ? 1 : 0;
  if (!Number.isFinite(value) || value < minimum) return;
  updateTemplate(key, value as OpenAISchedulerTemplate[NumericKey]);
}
</script>

<template>
  <div class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-700">
    <div class="flex flex-col gap-1">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ t("admin.settings.openaiSchedulerTemplates.title") }}
      </h3>
      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t("admin.settings.openaiSchedulerTemplates.description") }}
      </p>
    </div>

    <div class="mt-4 flex flex-wrap gap-2" role="tablist">
      <button
        v-for="option in profileOptions"
        :key="option.value"
        type="button"
        role="tab"
        :aria-selected="activeProfile === option.value"
        :class="[
          'rounded-md border px-3 py-1.5 text-sm transition-colors',
          activeProfile === option.value
            ? 'border-primary-600 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/30 dark:text-primary-300'
            : 'border-gray-200 text-gray-600 hover:border-gray-300 dark:border-dark-600 dark:text-gray-300 dark:hover:border-dark-500',
        ]"
        @click="activeProfile = option.value"
      >
        {{ option.label }}
      </button>
    </div>

    <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <label v-for="field in numericFields" :key="field.key" class="block">
        <span class="input-label">{{ field.label }}</span>
        <input
          class="input"
          :value="activeTemplate[field.key]"
          :min="field.key === 'top_k' ? 1 : 0"
          :step="field.key === 'top_k' ? 1 : 'any'"
          type="number"
          inputmode="decimal"
          @input="updateNumber(field.key, $event)"
        />
      </label>

      <div class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
        <Toggle
          :model-value="activeTemplate.sticky_weighted_enabled"
          @update:model-value="updateTemplate('sticky_weighted_enabled', $event)"
        />
        <span>
          <span class="block font-medium">
            {{ t("admin.settings.openaiSchedulerTemplates.stickyWeighted") }}
          </span>
        </span>
      </div>

      <div class="flex items-start gap-3 text-sm text-gray-700 dark:text-gray-300">
        <Toggle
          :model-value="activeTemplate.subscription_priority_enabled"
          @update:model-value="updateTemplate('subscription_priority_enabled', $event)"
        />
        <span>
          <span class="block font-medium">
            {{ t("admin.settings.openaiSchedulerTemplates.subscriptionPriority") }}
          </span>
        </span>
      </div>
    </div>
  </div>
</template>
