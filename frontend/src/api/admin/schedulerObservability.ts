import { apiClient } from "../client";

import type { SchedulerObservabilitySnapshot, SchedulerRequestType } from "@/features/scheduler-observability/types";

export interface SchedulerObservabilityQuery {
  timeRange: "15m" | "1h" | "6h" | "24h" | "7d";
  groupId?: number;
  model?: string;
  accountId?: number;
  apiKeyId?: number;
  view: "requests" | "sessions";
  page: number;
  pageSize: number;
  search?: string;
  traceFilter?: "all" | "sticky" | "switch" | "failed";
  requestType?: "all" | SchedulerRequestType;
}

export function normalizeSchedulerObservabilitySnapshot(
  snapshot: SchedulerObservabilitySnapshot,
): SchedulerObservabilitySnapshot {
  return {
    ...snapshot,
    enabled: snapshot.enabled !== false,
    view: snapshot.view === "sessions" ? "sessions" : "requests",
    retentionMode: snapshot.retentionMode === "hybrid" ? "hybrid" : "memory",
    retentionDays: snapshot.retentionDays || 7,
    switchReasons: snapshot.switchReasons ?? [],
    groups: snapshot.groups ?? [],
    models: snapshot.models ?? [],
    accounts: snapshot.accounts ?? [],
    apiKeys: snapshot.apiKeys ?? [],
    pagination: snapshot.pagination ?? { page: 1, pageSize: 20, total: 0, pages: 0 },
    traceCounts: snapshot.traceCounts ?? { all: 0, sticky: 0, switch: 0, failed: 0 },
    traces: (snapshot.traces ?? []).map((trace) => ({
      ...trace,
      platform: trace.platform || "openai",
      requestType: ["ws_v2", "stream", "sync"].includes(trace.requestType) ? trace.requestType : "sync",
      cyberBlocked: trace.cyberBlocked === true,
      accountPath: trace.accountPath ?? [],
      attempts: trace.attempts ?? [],
      candidates: trace.candidates ?? [],
    })),
    sessions: (snapshot.sessions ?? []).map((session) => ({
      ...session,
      accountIds: session.accountIds ?? [],
      accountNames: session.accountNames ?? {},
      turnAccounts: session.turnAccounts ?? [],
    })),
  };
}

export async function getSchedulerObservabilitySnapshot(
  query: SchedulerObservabilityQuery,
  options?: { signal?: AbortSignal },
): Promise<SchedulerObservabilitySnapshot> {
  const { data } = await apiClient.get<SchedulerObservabilitySnapshot>(
    "/admin/scheduler-observability/snapshot",
    {
      params: {
        time_range: query.timeRange,
        group_id: query.groupId,
        model: query.model || undefined,
        account_id: query.accountId,
        api_key_id: query.apiKeyId,
        view: query.view,
        page: query.page,
        page_size: query.pageSize,
        search: query.search || undefined,
        trace_filter: query.view === "requests" ? query.traceFilter : undefined,
        request_type: query.view === "requests" && query.requestType !== "all" ? query.requestType : undefined,
      },
      signal: options?.signal,
    },
  );
  return normalizeSchedulerObservabilitySnapshot(data);
}
