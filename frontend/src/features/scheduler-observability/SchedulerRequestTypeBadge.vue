<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { SchedulerRequestType } from "./types";

const props = defineProps<{
  requestType: SchedulerRequestType;
}>();

const { t } = useI18n();

const badgeClass = computed(() => {
  if (props.requestType === "ws_v2") return "bg-violet-100 text-violet-800 dark:bg-violet-900 dark:text-violet-200";
  if (props.requestType === "stream") return "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200";
  return "bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200";
});
</script>

<template>
  <span class="inline-flex items-center rounded px-2 py-0.5 text-[10px] font-medium" :class="badgeClass">
    {{ t(`admin.schedulerObservability.requestTypes.${requestType}`) }}
  </span>
</template>
