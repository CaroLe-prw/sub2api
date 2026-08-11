<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import Select, { type SelectOption } from "@/components/common/Select.vue";
import type {
  SchedulerObservabilityFilterOption,
  SchedulerObservabilityGroup,
} from "./types";

const props = defineProps<{
  groupId: number | "all";
  model: string;
  accountId: number | "all";
  apiKeyId: number | "all";
  groups: SchedulerObservabilityGroup[];
  models: string[];
  accounts: SchedulerObservabilityFilterOption[];
  apiKeys: SchedulerObservabilityFilterOption[];
}>();

const emit = defineEmits<{
  "update:group-id": [value: string | number | boolean | null];
  "update:model": [value: string | number | boolean | null];
  "update:account-id": [value: string | number | boolean | null];
  "update:api-key-id": [value: string | number | boolean | null];
}>();

const { t } = useI18n();

const groupOptions = computed<SelectOption[]>(() => [
  { value: "all", label: t("admin.schedulerObservability.filterOptions.allGroups") },
  ...props.groups.map((group) => ({ value: group.id, label: `${group.name} (#${group.id})` })),
]);

const modelOptions = computed<SelectOption[]>(() => [
  { value: "", label: t("admin.schedulerObservability.filterOptions.allModels") },
  ...props.models.map((model) => ({ value: model, label: model })),
]);

function entityOptions(
  values: SchedulerObservabilityFilterOption[],
  allLabel: string,
): SelectOption[] {
  return [
    { value: "all", label: allLabel },
    ...values.map((value) => ({ value: value.id, label: `${value.name} (#${value.id})` })),
  ];
}

const accountOptions = computed<SelectOption[]>(() => entityOptions(
  props.accounts,
  t("admin.schedulerObservability.filterOptions.allAccounts"),
));

const apiKeyOptions = computed<SelectOption[]>(() => entityOptions(
  props.apiKeys,
  t("admin.schedulerObservability.filterOptions.allApiKeys"),
));
</script>

<template>
  <div class="grid grid-cols-1 gap-2 sm:grid-cols-2 xl:grid-cols-4">
    <div>
      <label class="input-label">{{ t("admin.schedulerObservability.filterOptions.model") }}</label>
      <Select
        :model-value="model"
        :options="modelOptions"
        searchable
        :aria-label="t('admin.schedulerObservability.filterOptions.model')"
        @update:model-value="emit('update:model', $event)"
      />
    </div>
    <div>
      <label class="input-label">{{ t("admin.schedulerObservability.filterOptions.account") }}</label>
      <Select
        :model-value="accountId"
        :options="accountOptions"
        searchable
        :aria-label="t('admin.schedulerObservability.filterOptions.account')"
        @update:model-value="emit('update:account-id', $event)"
      />
    </div>
    <div>
      <label class="input-label">{{ t("admin.schedulerObservability.filterOptions.group") }}</label>
      <Select
        :model-value="groupId"
        :options="groupOptions"
        searchable
        :aria-label="t('admin.schedulerObservability.groupFilter')"
        @update:model-value="emit('update:group-id', $event)"
      />
    </div>
    <div>
      <label class="input-label">{{ t("admin.schedulerObservability.filterOptions.apiKey") }}</label>
      <Select
        :model-value="apiKeyId"
        :options="apiKeyOptions"
        searchable
        :aria-label="t('admin.schedulerObservability.filterOptions.apiKey')"
        @update:model-value="emit('update:api-key-id', $event)"
      />
    </div>
  </div>
</template>
