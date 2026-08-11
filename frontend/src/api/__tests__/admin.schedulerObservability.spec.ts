import { describe, expect, it } from "vitest";

import { normalizeSchedulerObservabilitySnapshot } from "@/api/admin/schedulerObservability";
import type { SchedulerObservabilitySnapshot } from "@/features/scheduler-observability/types";

describe("scheduler observability response normalization", () => {
  it("turns nullable collection fields into empty arrays", () => {
    const raw = {
      generatedAt: "2026-08-10T18:00:00+08:00",
      timeRange: "1h",
      retentionMode: "memory",
      retentionMax: 2_000,
      metrics: {
        requests: 1,
        stickyRequests: 0,
        stickyHitRate: 0,
        switchedRequests: 0,
        switches: 0,
        switchRate: 0,
        stableSessions: 0,
        sessions: 0,
        sessionStability: 0,
        cacheReadTokens: 0,
        cacheEligibleTokens: 0,
        followUpCacheRate: 0,
      },
      switchReasons: null,
      groups: null,
      models: null,
      accounts: null,
      apiKeys: null,
      traces: [{ accountPath: null, attempts: null, candidates: null }],
      sessions: [{ accountIds: null, accountNames: null, turnAccounts: null }],
    } as unknown as SchedulerObservabilitySnapshot;

    const normalized = normalizeSchedulerObservabilitySnapshot(raw);

    expect(normalized.switchReasons).toEqual([]);
    expect(normalized.retentionMode).toBe("memory");
    expect(normalized.retentionDays).toBe(7);
    expect(normalized.groups).toEqual([]);
    expect(normalized.models).toEqual([]);
    expect(normalized.accounts).toEqual([]);
    expect(normalized.apiKeys).toEqual([]);
    expect(normalized.traces[0].accountPath).toEqual([]);
    expect(normalized.traces[0].attempts).toEqual([]);
    expect(normalized.traces[0].candidates).toEqual([]);
    expect(normalized.sessions[0].accountIds).toEqual([]);
    expect(normalized.sessions[0].accountNames).toEqual({});
    expect(normalized.sessions[0].turnAccounts).toEqual([]);
  });
});
