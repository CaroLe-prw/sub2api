import { computed, onUnmounted, readonly, shallowRef, watch, type Ref } from "vue";
import { useI18n } from "vue-i18n";

import { getSchedulerObservabilitySnapshot } from "@/api/admin/schedulerObservability";
import type {
  SchedulerObservabilitySnapshot,
  SchedulerOverviewMetric,
  SchedulerRequestType,
} from "./types";

export type SchedulerTimeRange = "15m" | "1h" | "6h" | "24h" | "7d";
export type SchedulerGroupFilter = number | "all";
export type SchedulerEntityFilter = number | "all";
export type SchedulerObservabilityView = "requests" | "sessions";
export type SchedulerTraceFilter = "all" | "sticky" | "switch" | "failed";
export type SchedulerRequestTypeFilter = "all" | SchedulerRequestType;

interface SchedulerObservabilityOptions {
  timeRange: Ref<SchedulerTimeRange>;
  groupId: Ref<SchedulerGroupFilter>;
  model: Ref<string>;
  accountId: Ref<SchedulerEntityFilter>;
  apiKeyId: Ref<SchedulerEntityFilter>;
  view: Ref<SchedulerObservabilityView>;
  page: Ref<number>;
  pageSize: Ref<number>;
  search: Ref<string>;
  traceFilter: Ref<SchedulerTraceFilter>;
  requestType: Ref<SchedulerRequestTypeFilter>;
}

function isAbortError(error: unknown): boolean {
  const candidate = error as { name?: string; code?: string } | null;
  return candidate?.name === "AbortError" || candidate?.name === "CanceledError" || candidate?.code === "ERR_CANCELED";
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

export function useSchedulerObservability(options: SchedulerObservabilityOptions) {
  const { t } = useI18n();
  const snapshot = shallowRef<SchedulerObservabilitySnapshot | null>(null);
  const isLoading = shallowRef(false);
  const errorMessage = shallowRef("");
  let controller: AbortController | null = null;

  const traces = computed(() => snapshot.value?.traces ?? []);
  const sessions = computed(() => snapshot.value?.sessions ?? []);
  const groups = computed(() => snapshot.value?.groups ?? []);
  const models = computed(() => snapshot.value?.models ?? []);
  const accounts = computed(() => snapshot.value?.accounts ?? []);
  const apiKeys = computed(() => snapshot.value?.apiKeys ?? []);
  const switchReasonCounts = computed(() => snapshot.value?.switchReasons ?? []);
  const metrics = computed<SchedulerOverviewMetric[]>(() => {
    const value = snapshot.value?.metrics;
    if (!value) return [];
    return [
      {
        key: "requests",
        value: value.requests.toLocaleString(),
        detail: t("admin.schedulerObservability.metricLiveDetails.requests", { range: options.timeRange.value }),
        trend: null,
        tone: "neutral",
      },
      {
        key: "sticky",
        value: formatPercent(value.stickyHitRate),
        detail: t("admin.schedulerObservability.metricLiveDetails.sticky", {
          hits: value.stickyRequests,
          total: value.stickyDetectedRequests ?? value.requests,
        }),
        trend: null,
        tone: value.stickyHitRate >= 0.6 ? "success" : "warning",
      },
      {
        key: "switch",
        value: formatPercent(value.switchRate),
        detail: t("admin.schedulerObservability.metricLiveDetails.switch", { requests: value.switchedRequests, switches: value.switches }),
        trend: null,
        tone: value.switchRate > 0.1 ? "danger" : value.switchRate > 0.05 ? "warning" : "success",
      },
      {
        key: "stability",
        value: formatPercent(value.sessionStability),
        detail: t("admin.schedulerObservability.metricLiveDetails.stability", { stable: value.stableSessions, total: value.sessions }),
        trend: null,
        tone: value.sessionStability >= 0.8 ? "success" : "warning",
      },
      {
        key: "cache",
        value: formatPercent(value.followUpCacheRate),
        detail: t("admin.schedulerObservability.metricLiveDetails.cache", { read: value.cacheReadTokens.toLocaleString(), eligible: value.cacheEligibleTokens.toLocaleString() }),
        trend: null,
        tone: value.followUpCacheRate >= 0.5 ? "success" : "warning",
      },
    ];
  });

  async function load(): Promise<void> {
    controller?.abort();
    const current = new AbortController();
    controller = current;
    isLoading.value = true;
    errorMessage.value = "";
    try {
      snapshot.value = await getSchedulerObservabilitySnapshot(
        {
          timeRange: options.timeRange.value,
          groupId: options.groupId.value === "all" ? undefined : options.groupId.value,
          model: options.model.value.trim() || undefined,
          accountId: options.accountId.value === "all" ? undefined : options.accountId.value,
          apiKeyId: options.apiKeyId.value === "all" ? undefined : options.apiKeyId.value,
          view: options.view.value,
          page: options.page.value,
          pageSize: options.pageSize.value,
          search: options.search.value.trim() || undefined,
          traceFilter: options.traceFilter.value,
          requestType: options.requestType.value,
        },
        { signal: current.signal },
      );
    } catch (error) {
      if (!isAbortError(error)) {
        const message = (error as { message?: string } | null)?.message;
        errorMessage.value = message || t("admin.schedulerObservability.loadError");
      }
    } finally {
      if (controller === current) {
        controller = null;
        isLoading.value = false;
      }
    }
  }

  watch(
    [options.timeRange, options.groupId, options.model, options.accountId, options.apiKeyId, options.view, options.page, options.pageSize, options.search, options.traceFilter, options.requestType],
    load,
    { immediate: true },
  );
  onUnmounted(() => controller?.abort());

  return {
    snapshot: readonly(snapshot),
    traces,
    sessions,
    groups,
    models,
    accounts,
    apiKeys,
    switchReasonCounts,
    metrics,
    isLoading: readonly(isLoading),
    errorMessage: readonly(errorMessage),
    refresh: load,
  };
}
