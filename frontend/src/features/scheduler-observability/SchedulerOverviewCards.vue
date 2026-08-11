<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import Icon from "@/components/icons/Icon.vue";
import type { SchedulerOverviewMetric } from "./types";

const props = defineProps<{
  metrics: SchedulerOverviewMetric[];
}>();

const { t } = useI18n();

const iconNames: Record<SchedulerOverviewMetric["key"], "chart" | "link" | "swap" | "shield" | "database"> = {
  requests: "chart",
  sticky: "link",
  switch: "swap",
  stability: "shield",
  cache: "database",
};

const toneClasses: Record<SchedulerOverviewMetric["tone"], string> = {
  neutral: "bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400",
  success: "bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400",
  warning: "bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-400",
  danger: "bg-rose-50 text-rose-600 dark:bg-rose-500/10 dark:text-rose-400",
};

const cards = computed(() =>
  props.metrics.map((metric) => ({
    ...metric,
    icon: iconNames[metric.key],
    toneClass: toneClasses[metric.tone],
    label: t(`admin.schedulerObservability.metrics.${metric.key}`),
  })),
);
</script>

<template>
  <section
    class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5"
    :aria-label="t('admin.schedulerObservability.sections.overview')"
  >
    <article
      v-for="card in cards"
      :key="card.key"
      class="card min-w-0 p-4 transition-colors duration-200 hover:border-gray-300 dark:hover:border-dark-500"
    >
      <div class="flex items-start justify-between gap-3">
        <div class="min-w-0">
          <p class="truncate text-xs font-medium text-gray-500 dark:text-dark-300">
            {{ card.label }}
          </p>
          <p class="mt-2 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">
            {{ card.value }}
          </p>
        </div>
        <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl" :class="card.toneClass">
          <Icon :name="card.icon" size="sm" :stroke-width="1.8" />
        </span>
      </div>
      <div class="mt-3 flex items-center justify-between gap-2 text-[11px]">
        <span class="truncate text-gray-500 dark:text-dark-400">{{ card.detail }}</span>
        <span
          v-if="card.trend != null"
          class="shrink-0 font-medium tabular-nums"
          :class="card.trend >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'"
        >
          {{ card.trend >= 0 ? "+" : "" }}{{ card.trend.toFixed(1) }}%
        </span>
      </div>
    </article>
  </section>
</template>
