<script setup lang="ts">
import { computed, shallowRef, watch } from "vue";
import { useI18n } from "vue-i18n";

import Pagination from "@/components/common/Pagination.vue";
import AppLayout from "@/components/layout/AppLayout.vue";
import Icon from "@/components/icons/Icon.vue";
import SchedulerOverviewCards from "@/features/scheduler-observability/SchedulerOverviewCards.vue";
import SchedulerObservabilityFilters from "@/features/scheduler-observability/SchedulerObservabilityFilters.vue";
import SchedulerSessionPanel from "@/features/scheduler-observability/SchedulerSessionPanel.vue";
import SchedulerTraceDrawer from "@/features/scheduler-observability/SchedulerTraceDrawer.vue";
import SchedulerTraceTable from "@/features/scheduler-observability/SchedulerTraceTable.vue";
import type { SchedulerTrace } from "@/features/scheduler-observability/types";
import {
  useSchedulerObservability,
  type SchedulerTimeRange,
  type SchedulerTraceFilter,
  type SchedulerRequestTypeFilter,
} from "@/features/scheduler-observability/useSchedulerObservability";
import { getPersistedPageSize } from "@/composables/usePersistedPageSize";

type DetailTab = "requests" | "sessions";
const { t } = useI18n();

const activeTab = shallowRef<DetailTab>("requests");
const traceFilter = shallowRef<SchedulerTraceFilter>("all");
const requestType = shallowRef<SchedulerRequestTypeFilter>("all");
const timeRange = shallowRef<SchedulerTimeRange>("1h");
const groupId = shallowRef<number | "all">("all");
const model = shallowRef("");
const accountId = shallowRef<number | "all">("all");
const apiKeyId = shallowRef<number | "all">("all");
const searchQuery = shallowRef("");
const requestPage = shallowRef(1);
const sessionPage = shallowRef(1);
const pageSize = shallowRef(getPersistedPageSize());
const selectedTraceId = shallowRef<string | null>(null);
const currentPage = computed({
  get: () => activeTab.value === "requests" ? requestPage.value : sessionPage.value,
  set: (value: number) => {
    if (activeTab.value === "requests") requestPage.value = value;
    else sessionPage.value = value;
  },
});

watch(searchQuery, () => {
  currentPage.value = 1;
});

const timeRanges: SchedulerTimeRange[] = ["15m", "1h", "6h", "24h", "7d"];
const {
  snapshot,
  traces: schedulerTraces,
  sessions: schedulerSessions,
  groups,
  models,
  accounts,
  apiKeys,
  switchReasonCounts,
  metrics: schedulerOverviewMetrics,
  isLoading,
  errorMessage,
  refresh,
} = useSchedulerObservability({
  timeRange,
  groupId,
  model,
  accountId,
  apiKeyId,
  view: activeTab,
  page: currentPage,
  pageSize,
  search: searchQuery,
  traceFilter,
  requestType,
});

const traceFilters = computed<Array<{ key: SchedulerTraceFilter; count: number }>>(() => [
  { key: "all", count: snapshot.value?.traceCounts.all ?? 0 },
  { key: "sticky", count: snapshot.value?.traceCounts.sticky ?? 0 },
  { key: "switch", count: snapshot.value?.traceCounts.switch ?? 0 },
  { key: "failed", count: snapshot.value?.traceCounts.failed ?? 0 },
]);

const selectedTrace = computed<SchedulerTrace | null>(() => {
  if (!selectedTraceId.value) return null;
  return schedulerTraces.value.find((trace) => trace.id === selectedTraceId.value) ?? null;
});

function traceNeedsCompletionRefresh(trace: SchedulerTrace | null): boolean {
  if (!trace || trace.status === "failed" || trace.status === "canceled") return false;
  return !trace.attempts.some((attempt) => attempt.kind === "request_success");
}

watch(
  () => traceNeedsCompletionRefresh(selectedTrace.value),
  (shouldRefresh, _, onCleanup) => {
    if (!shouldRefresh) return;
    const timer = window.setInterval(() => {
      if (!isLoading.value) void refresh();
    }, 2_000);
    onCleanup(() => window.clearInterval(timer));
  },
  { immediate: true },
);

const reasonTones = ["bg-amber-500", "bg-orange-500", "bg-rose-500", "bg-violet-500", "bg-gray-400 dark:bg-dark-500"];
const switchReasons = computed(() => {
  const total = switchReasonCounts.value.reduce((sum, reason) => sum + reason.count, 0);
  return switchReasonCounts.value.slice(0, 5).map((reason, index) => ({
    ...reason,
    value: total > 0 ? (reason.count / total) * 100 : 0,
    tone: reasonTones[index % reasonTones.length],
  }));
});
const totalSwitchReasons = computed(() => switchReasonCounts.value.reduce((sum, reason) => sum + reason.count, 0));

function selectTrace(trace: SchedulerTrace) {
  selectedTraceId.value = trace.id;
  if (traceNeedsCompletionRefresh(trace) && !isLoading.value) void refresh();
}

function closeTrace() {
  selectedTraceId.value = null;
}

function updateGroupId(value: string | number | boolean | null) {
  groupId.value = typeof value === "number" ? value : "all";
  requestPage.value = 1;
  sessionPage.value = 1;
}

function updateModel(value: string | number | boolean | null) {
  model.value = typeof value === "string" ? value : "";
  resetDetailPages();
}

function updateAccountId(value: string | number | boolean | null) {
  accountId.value = typeof value === "number" ? value : "all";
  resetDetailPages();
}

function updateApiKeyId(value: string | number | boolean | null) {
  apiKeyId.value = typeof value === "number" ? value : "all";
  resetDetailPages();
}

function resetDetailPages() {
  requestPage.value = 1;
  sessionPage.value = 1;
}

function selectTimeRange(value: SchedulerTimeRange) {
  timeRange.value = value;
  requestPage.value = 1;
  sessionPage.value = 1;
}

function selectTab(value: DetailTab) {
  activeTab.value = value;
}

function searchSessionTraces(fingerprint: string) {
  activeTab.value = "requests";
  traceFilter.value = "all";
  searchQuery.value = fingerprint;
  requestPage.value = 1;
}

function selectTraceFilter(value: SchedulerTraceFilter) {
  traceFilter.value = value;
  requestPage.value = 1;
}

function selectRequestType(value: SchedulerRequestTypeFilter) {
  requestType.value = value;
  requestPage.value = 1;
}

function updatePageSize(value: number) {
  pageSize.value = value;
  requestPage.value = 1;
  sessionPage.value = 1;
}

function reasonLabel(reason: string): string {
  const key = `admin.schedulerObservability.reasons.${reason}`;
  const translated = t(key);
  return translated === key ? reason : translated;
}
</script>

<template>
  <AppLayout>
    <div class="space-y-5 pb-10">
      <header class="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
        <div>
          <div class="flex flex-wrap items-center gap-2">
            <h1 class="text-xl font-semibold tracking-tight text-gray-950 dark:text-white sm:text-2xl">
              {{ t("admin.schedulerObservability.title") }}
            </h1>
            <span class="inline-flex items-center gap-1 rounded-full bg-violet-50 px-2 py-1 text-[10px] font-semibold text-violet-700 ring-1 ring-inset ring-violet-200 dark:bg-violet-500/10 dark:text-violet-300 dark:ring-violet-500/20">
              <Icon name="beaker" size="xs" />
              {{ t("admin.schedulerObservability.liveBadge") }}
            </span>
          </div>
          <p class="mt-2 max-w-3xl text-sm leading-6 text-gray-600 dark:text-dark-300">
            {{ t("admin.schedulerObservability.description") }}
          </p>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <div class="inline-flex rounded-xl border border-gray-200 bg-white p-1 shadow-sm dark:border-dark-700 dark:bg-dark-800">
            <button
              v-for="range in timeRanges"
              :key="range"
              type="button"
              class="cursor-pointer rounded-lg px-3 py-1.5 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="timeRange === range
                ? 'bg-gray-900 text-white shadow-sm dark:bg-white dark:text-gray-950'
                : 'text-gray-500 hover:bg-gray-50 hover:text-gray-800 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white'"
              :aria-pressed="timeRange === range"
              @click="selectTimeRange(range)"
            >
              {{ range }}
            </button>
          </div>
          <button type="button" class="btn btn-secondary gap-2" :title="t('admin.schedulerObservability.refreshHint')" :disabled="isLoading" @click="refresh">
            <Icon name="refresh" size="sm" :class="isLoading ? 'animate-spin' : ''" />
            <span>{{ t("common.refresh") }}</span>
          </button>
        </div>
      </header>

      <div class="flex items-start gap-3 rounded-2xl border border-emerald-200 bg-emerald-50/80 p-4 text-emerald-900 dark:border-emerald-500/20 dark:bg-emerald-500/5 dark:text-emerald-100">
        <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0 text-emerald-600 dark:text-emerald-300" />
        <div class="min-w-0">
          <p class="text-xs font-semibold">{{ t("admin.schedulerObservability.liveTitle") }}</p>
          <p class="mt-1 text-xs leading-5 text-emerald-800/80 dark:text-emerald-100/70">
            {{ t(snapshot?.retentionMode === "hybrid"
              ? "admin.schedulerObservability.hybridLiveDescription"
              : "admin.schedulerObservability.liveDescription", {
              max: snapshot?.retentionMax?.toLocaleString() ?? "1,000",
              days: snapshot?.retentionDays ?? 7,
            }) }}
          </p>
        </div>
      </div>

      <div v-if="errorMessage" class="flex items-center justify-between gap-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs text-rose-700 dark:border-rose-500/20 dark:bg-rose-500/10 dark:text-rose-300" role="alert">
        <span>{{ t("admin.schedulerObservability.loadErrorWithMessage", { message: errorMessage }) }}</span>
        <button type="button" class="font-semibold underline underline-offset-2" @click="refresh">{{ t("admin.schedulerObservability.retry") }}</button>
      </div>

      <div v-if="snapshot && !snapshot.enabled" class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-500/20 dark:bg-amber-500/10 dark:text-amber-200" role="status">
        {{ t("admin.schedulerObservability.disabledHint") }}
      </div>

      <SchedulerOverviewCards :metrics="schedulerOverviewMetrics" />

      <section class="card p-4" :aria-label="t('admin.schedulerObservability.sections.switchReasons')">
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center">
          <div class="min-w-[190px]">
            <div class="flex items-center gap-2">
              <span class="flex h-8 w-8 items-center justify-center rounded-xl bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-300">
                <Icon name="swap" size="sm" />
              </span>
              <div>
                <h2 class="text-xs font-semibold text-gray-900 dark:text-white">{{ t("admin.schedulerObservability.switchReasons.title") }}</h2>
                <p class="mt-0.5 text-[10px] text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.switchReasons.liveDescription", { count: totalSwitchReasons }) }}</p>
              </div>
            </div>
          </div>
          <div class="flex-1">
            <div class="flex h-2.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
              <span v-for="reason in switchReasons" :key="reason.key" :class="reason.tone" :style="{ width: `${reason.value}%` }"></span>
            </div>
            <div class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-[11px] sm:grid-cols-4">
              <div v-for="reason in switchReasons" :key="`${reason.key}-legend`" class="flex items-center gap-2">
                <span class="h-2 w-2 rounded-full" :class="reason.tone"></span>
                <span class="truncate text-gray-600 dark:text-dark-300">{{ reasonLabel(reason.key) }}</span>
                <span class="ml-auto font-semibold tabular-nums text-gray-800 dark:text-dark-100">{{ reason.value.toFixed(0) }}% · {{ reason.count }}</span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="border-b border-gray-200 px-4 pt-4 dark:border-dark-700 sm:px-5">
          <div class="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
            <nav class="flex items-center gap-5" :aria-label="t('admin.schedulerObservability.sections.details')">
              <button
                v-for="tab in (['requests', 'sessions'] as const)"
                :key="tab"
                type="button"
                class="relative cursor-pointer pb-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-900"
                :class="activeTab === tab ? 'text-primary-700 dark:text-primary-300' : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'"
                :aria-current="activeTab === tab ? 'page' : undefined"
                @click="selectTab(tab)"
              >
                {{ t(`admin.schedulerObservability.tabs.${tab}`) }}
                <span v-if="activeTab === tab" class="absolute inset-x-0 bottom-0 h-0.5 rounded-full bg-primary-500"></span>
              </button>
            </nav>

            <div class="pb-3">
              <label class="relative block min-w-0 sm:w-72">
                <span class="sr-only">{{ t("admin.schedulerObservability.searchPlaceholder") }}</span>
                <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
                <input
                  v-model="searchQuery"
                  type="search"
                  class="input w-full py-2 pl-9 text-xs"
                  :placeholder="t('admin.schedulerObservability.searchPlaceholder')"
                />
              </label>
            </div>
          </div>

          <SchedulerObservabilityFilters
            class="border-t border-gray-100 py-3 dark:border-dark-800"
            :group-id="groupId"
            :model="model"
            :account-id="accountId"
            :api-key-id="apiKeyId"
            :groups="groups"
            :models="models"
            :accounts="accounts"
            :api-keys="apiKeys"
            @update:group-id="updateGroupId"
            @update:model="updateModel"
            @update:account-id="updateAccountId"
            @update:api-key-id="updateApiKeyId"
          />

          <div v-if="activeTab === 'requests'" class="flex flex-wrap items-center gap-2 pb-3 pt-3">
            <span class="mr-1 text-[11px] font-medium text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.requestTypeFilter") }}</span>
            <div class="mr-3 inline-flex rounded-md border border-gray-200 bg-gray-50 p-0.5 dark:border-dark-700 dark:bg-dark-800" role="group" :aria-label="t('admin.schedulerObservability.requestTypeFilter')">
              <button
                v-for="type in (['all', 'ws_v2', 'stream', 'sync'] as const)"
                :key="type"
                type="button"
                class="cursor-pointer rounded px-2.5 py-1 text-[11px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
                :class="requestType === type
                  ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                  : 'text-gray-500 hover:text-gray-800 dark:text-dark-400 dark:hover:text-white'"
                :aria-pressed="requestType === type"
                @click="selectRequestType(type)"
              >
                {{ t(`admin.schedulerObservability.requestTypes.${type}`) }}
              </button>
            </div>
            <button
              v-for="filter in traceFilters"
              :key="filter.key"
              type="button"
              class="cursor-pointer rounded-full px-2.5 py-1 text-[11px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
              :class="traceFilter === filter.key
                ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-950'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-dark-300 dark:hover:bg-dark-600'"
              :aria-pressed="traceFilter === filter.key"
              @click="selectTraceFilter(filter.key)"
            >
              {{ t(`admin.schedulerObservability.filters.${filter.key}`) }}
              <span class="ml-1 opacity-70">{{ filter.count }}</span>
            </button>
          </div>
        </div>

        <SchedulerTraceTable v-if="activeTab === 'requests'" :traces="schedulerTraces" @select="selectTrace" />
        <div v-else class="bg-gray-50/60 p-4 dark:bg-dark-950/20 sm:p-5">
          <SchedulerSessionPanel :sessions="schedulerSessions" @search-traces="searchSessionTraces" />
        </div>
        <Pagination
          v-if="(snapshot?.pagination.total ?? 0) > 0"
          :page="snapshot?.pagination.page ?? currentPage"
          :page-size="snapshot?.pagination.pageSize ?? pageSize"
          :total="snapshot?.pagination.total ?? 0"
          @update:page="currentPage = $event"
          @update:page-size="updatePageSize"
        />
      </section>
    </div>

    <SchedulerTraceDrawer :trace="selectedTrace" @close="closeTrace" />
  </AppLayout>
</template>
