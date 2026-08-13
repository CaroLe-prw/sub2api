import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { schedulerSessions, schedulerTraces } from "@/features/scheduler-observability/mockData";
import SchedulerObservabilityView from "../SchedulerObservabilityView.vue";

const getSnapshotMock = vi.hoisted(() => vi.fn());

vi.mock("@/api/admin/schedulerObservability", () => ({
  getSchedulerObservabilitySnapshot: getSnapshotMock,
}));

const messages = vi.hoisted<Record<string, string>>(() => ({
  "admin.schedulerObservability.title": "调度观测",
  "admin.schedulerObservability.pageTabs.label": "调度观测视图",
  "admin.schedulerObservability.pageTabs.traces": "调度轨迹",
  "admin.schedulerObservability.pageTabs.probes": "渠道探测",
  "admin.schedulerObservability.liveTitle": "已连接当前节点的实时调度轨迹",
  "admin.schedulerObservability.filters.switch": "发生切号",
  "admin.schedulerObservability.requestTypeFilter": "请求类型",
  "admin.schedulerObservability.requestTypes.all": "全部类型",
  "admin.schedulerObservability.requestTypes.ws_v2": "WS",
  "admin.schedulerObservability.requestTypes.stream": "流式",
  "admin.schedulerObservability.requestTypes.sync": "同步",
  "admin.schedulerObservability.requestTypes.cyber": "安全策略",
  "admin.schedulerObservability.status.pending": "请求中",
  "admin.schedulerObservability.groupFilter": "筛选分组",
  "admin.schedulerObservability.filterOptions.model": "模型",
  "admin.schedulerObservability.filterOptions.account": "账号",
  "admin.schedulerObservability.filterOptions.group": "分组",
  "admin.schedulerObservability.filterOptions.apiKey": "密钥",
  "admin.schedulerObservability.filterOptions.allModels": "全部模型",
  "admin.schedulerObservability.filterOptions.allAccounts": "全部账号",
  "admin.schedulerObservability.filterOptions.allGroups": "全部分组",
  "admin.schedulerObservability.filterOptions.allApiKeys": "全部密钥",
  "admin.schedulerObservability.tabs.sessions": "会话分析",
  "admin.schedulerObservability.sessions.accountJourney": "账号轨迹",
  "admin.schedulerObservability.sessions.turnJourneyLabel": "共 {count} 轮账号轨迹",
  "admin.schedulerObservability.sessions.turnAccountTitle": "第 {turn} 轮：{account}（#{id}）",
  "admin.schedulerObservability.sessions.searchTraces": "查询会话 {session} 的请求轨迹",
  "admin.schedulerObservability.sessions.howToReadDescription":
    "每个圆点是一轮请求，颜色代表账号。颜色变化说明会话发生了账号漂移。",
  "admin.schedulerObservability.drawer.whyTitle": "为什么最终选择这个账号？",
  "admin.schedulerObservability.summaryDetails.sticky_escaped_consecutive_errors":
    "账号 {first} 命中 session_hash，但连续两次失败触发粘性逃逸；排除故障账号后，{final} 在剩余候选中评分最高并完成请求。",
  "admin.schedulerObservability.drawer.candidates": "候选评分",
  "admin.schedulerObservability.attempts.account_reselected": "本地重新选择账号 {account}",
  "admin.schedulerObservability.attempts.admission_rejected": "账号 {account} 本地准入未通过",
  "admin.schedulerObservability.attempts.request_success": "账号 {account} 请求成功",
  "admin.schedulerObservability.reasons.profit_veto": "当前定价下未达到分组利润准入要求，未发起上游请求",
  "admin.schedulerObservability.candidateStates.rejected": "本地否决",
}));

vi.mock("vue-i18n", async (importOriginal) => {
  const actual = await importOriginal<typeof import("vue-i18n")>();

  return {
    ...actual,
    useI18n: () => ({
      locale: { value: "zh-CN" },
      t: (key: string, params?: Record<string, unknown>) => {
        let value = messages[key] ?? key;
        for (const [name, replacement] of Object.entries(params ?? {})) {
          value = value.replaceAll(`{${name}}`, String(replacement));
        }
        return value;
      },
    }),
  };
});

const mountedWrappers: Array<ReturnType<typeof mount>> = [];

function mountView() {
  const wrapper = mount(SchedulerObservabilityView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: {
          template: "<div><slot /></div>",
        },
        SchedulerProbePanel: {
          template: '<section data-testid="scheduler-probe-panel">号池主动探测</section>',
        },
      },
    },
  });
  mountedWrappers.push(wrapper);
  return wrapper;
}

afterEach(() => {
  mountedWrappers.splice(0).forEach((wrapper) => wrapper.unmount());
  document.body.innerHTML = "";
});

beforeEach(() => {
  getSnapshotMock.mockReset();
  getSnapshotMock.mockImplementation(async (query: { view: "requests" | "sessions"; page: number; pageSize: number; traceFilter?: string; requestType?: string; search?: string }) => {
    const traces = query.view === "requests"
      ? schedulerTraces.filter((trace) =>
        (query.traceFilter !== "switch" || trace.switchCount > 0)
        && (query.requestType === "all" || trace.requestType === query.requestType))
      : [];
    const sessions = query.view === "sessions" ? schedulerSessions : [];
    return {
    enabled: true,
    generatedAt: "2026-08-10T17:00:00+08:00",
    timeRange: "1h",
    view: query.view,
    retentionMode: "memory",
    retentionMax: 1_000,
    pagination: {
      page: query.page,
      pageSize: query.pageSize,
      total: query.view === "requests" ? traces.length : sessions.length,
      pages: 1,
    },
    traceCounts: {
      all: schedulerTraces.length,
      sticky: schedulerTraces.filter((trace) => trace.stickyHit).length,
      switch: schedulerTraces.filter((trace) => trace.switchCount > 0).length,
      failed: schedulerTraces.filter((trace) => trace.status === "failed").length,
    },
    metrics: {
      requests: schedulerTraces.length,
      stickyRequests: 5,
      stickyHitRate: 5 / schedulerTraces.length,
      switchedRequests: 2,
      switches: 2,
      switchRate: 2 / schedulerTraces.length,
      stableSessions: 2,
      sessions: schedulerSessions.length,
      sessionStability: 0.5,
      cacheReadTokens: 100,
      cacheEligibleTokens: 200,
      followUpCacheRate: 0.5,
    },
    switchReasons: [{ key: "rate_limit", count: 2 }],
    groups: [
      { id: 5, name: "OpenAI 主池" },
      { id: 8, name: "低成本池" },
    ],
    models: ["gpt-5.6", "gpt-5.6-sol", "gpt-5.6-terra"],
    accounts: [
      { id: 9, name: "codex-oauth-09" },
      { id: 12, name: "codex-oauth-12" },
      { id: 15, name: "codex-oauth-15" },
    ],
    apiKeys: [{ id: 7, name: "Codex Team" }],
    traces,
    sessions,
  };
  });
});

describe("SchedulerObservabilityView", () => {
  it("hosts account-pool probes as a dedicated scheduler observability view", async () => {
    const wrapper = mountView();
    await flushPromises();

    const probeTab = wrapper.findAll('[role="tab"]')
      .find((tab) => tab.text().trim() === "渠道探测");
    expect(probeTab).toBeDefined();

    await probeTab?.trigger("click");

    expect(wrapper.find('[data-testid="scheduler-probe-panel"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("号池主动探测");
    expect(wrapper.find('[data-testid="scheduler-trace-desktop-table"]').exists()).toBe(false);
  });

  it("uses the shared custom select for the group filter", async () => {
    const wrapper = mountView();
    await flushPromises();

    expect(wrapper.find("select").exists()).toBe(false);
    expect(wrapper.find('button[aria-label="筛选分组"]').exists()).toBe(true);
    expect(wrapper.find('button[aria-label="模型"]').exists()).toBe(true);
    expect(wrapper.find('button[aria-label="账号"]').exists()).toBe(true);
    expect(wrapper.find('button[aria-label="密钥"]').exists()).toBe(true);
    expect(getSnapshotMock).toHaveBeenCalledWith(
      expect.objectContaining({ view: "requests", page: 1, pageSize: 20, timeRange: "1h" }),
      expect.any(Object),
    );
  });

  it("filters switched traces and explains a selected request in the detail drawer", async () => {
    const wrapper = mountView();
    await flushPromises();

    expect(wrapper.text()).toContain("调度观测");
    expect(wrapper.text()).toContain("已连接当前节点的实时调度轨迹");

    const switchFilter = wrapper
      .findAll("button")
      .find((button) => button.text().includes("发生切号"));
    expect(switchFilter).toBeDefined();
    await switchFilter?.trigger("click");

    const traceRows = wrapper.findAll("tbody tr");
    expect(traceRows).toHaveLength(2);
    await traceRows[0].trigger("click");

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]');
    expect(dialog).not.toBeNull();
    expect(dialog?.textContent).toContain("为什么最终选择这个账号");
    expect(dialog?.textContent).toContain("连续两次失败触发粘性逃逸");
    expect(dialog?.textContent).toContain("候选评分");
  });

  it("provides mobile lists while keeping wide tables desktop-only", async () => {
    const wrapper = mountView();
    await flushPromises();

    const mobileTraces = wrapper.get('[data-testid="scheduler-trace-mobile-list"]');
    const desktopTraces = wrapper.get('[data-testid="scheduler-trace-desktop-table"]');
    expect(mobileTraces.classes()).toContain("md:hidden");
    expect(desktopTraces.classes()).toContain("hidden");
    expect(desktopTraces.classes()).toContain("md:block");
    expect(mobileTraces.findAll(":scope > button")).toHaveLength(schedulerTraces.length);

    await mobileTraces.find("button").trigger("click");
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull();
  });

  it("uses outcome colors before latency colors and does not present pending zero as duration", async () => {
    const failedTrace = schedulerTraces.find((trace) => trace.status === "failed");
    expect(failedTrace).toBeDefined();
    const pendingTrace = {
      ...schedulerTraces[0],
      id: "trace-pending",
      requestId: "req-pending",
      status: "pending" as const,
      durationMs: 0,
      endToEndFirstTokenMs: null,
    };
    getSnapshotMock.mockResolvedValueOnce({
      enabled: true,
      generatedAt: "2026-08-10T17:00:00+08:00",
      timeRange: "1h",
      view: "requests",
      retentionMode: "memory",
      retentionMax: 1_000,
      pagination: { page: 1, pageSize: 20, total: 2, pages: 1 },
      traceCounts: { all: 2, sticky: 0, switch: 0, failed: 1 },
      metrics: {
        requests: 2, stickyRequests: 0, stickyHitRate: 0, switchedRequests: 0, switches: 0,
        switchRate: 0, stableSessions: 0, sessions: 0, sessionStability: 0,
        cacheReadTokens: 0, cacheEligibleTokens: 0, followUpCacheRate: 0,
      },
      switchReasons: [],
      groups: [],
      traces: [failedTrace!, pendingTrace],
      sessions: [],
    });

    const wrapper = mountView();
    await flushPromises();
    const mobileRows = wrapper.get('[data-testid="scheduler-trace-mobile-list"]').findAll(":scope > button");
    expect(mobileRows[0].find(".bg-rose-500").exists()).toBe(true);
    expect(mobileRows[0].find(".text-rose-600").exists()).toBe(true);
    expect(mobileRows[1].text()).toContain("请求中");
    expect(mobileRows[1].text()).not.toContain("0ms");
    expect(mobileRows[1].find(".bg-sky-500").exists()).toBe(true);

    const desktopAccountPath = wrapper
      .get('[data-testid="scheduler-trace-desktop-table"]')
      .find("tbody tr td:nth-child(6) > div");
    expect(desktopAccountPath.classes()).toContain("flex-wrap");
    expect(desktopAccountPath.classes()).not.toContain("overflow-hidden");
  });

  it("switches to the session analysis view", async () => {
    const wrapper = mountView();
    await flushPromises();
    const sessionTab = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === "会话分析");

    expect(sessionTab).toBeDefined();
    await sessionTab?.trigger("click");
    await flushPromises();

    expect(getSnapshotMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ view: "sessions", page: 1, pageSize: 20 }),
      expect.any(Object),
    );

    expect(wrapper.text()).toContain("账号轨迹");
    expect(wrapper.text()).toContain("每个圆点是一轮请求");
    expect(wrapper.findAll("tbody tr")).toHaveLength(4);
    const mobileSessions = wrapper.get('[data-testid="scheduler-session-mobile-list"]');
    const desktopSessions = wrapper.get('[data-testid="scheduler-session-desktop-table"]');
    expect(mobileSessions.classes()).toContain("md:hidden");
    expect(desktopSessions.classes()).toContain("hidden");
    expect(desktopSessions.classes()).toContain("md:block");
    expect(mobileSessions.findAll(":scope > article")).toHaveLength(schedulerSessions.length);

    const accountJourney = desktopSessions.find('[aria-label$="轮账号轨迹"]');
    expect(accountJourney.classes()).toContain("flex-wrap");
    expect(accountJourney.classes()).toContain("w-[228px]");
    const firstJourneyDot = accountJourney.find('[aria-label*="codex-oauth-09"]');
    expect(firstJourneyDot.exists()).toBe(true);
    await firstJourneyDot.trigger("mouseenter");
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain("codex-oauth-09");
    await firstJourneyDot.trigger("mouseleave");

    const accountId = wrapper.find('[aria-label="codex-oauth-09 (#9)"]');
    expect(accountId.exists()).toBe(true);
    await accountId.trigger("mouseenter");
    expect(document.body.querySelector('[role="tooltip"]')?.textContent).toContain("codex-oauth-09 (#9)");

    const sessionLink = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === schedulerSessions[0].fingerprint);
    expect(sessionLink).toBeDefined();
    await sessionLink?.trigger("click");
    await flushPromises();

    expect(getSnapshotMock).toHaveBeenLastCalledWith(
      expect.objectContaining({
        view: "requests",
        search: schedulerSessions[0].fingerprint,
        page: 1,
      }),
      expect.any(Object),
    );
  });

  it("filters request traces by transport type and shows result badges", async () => {
    const wrapper = mountView();
    await flushPromises();

    const wsFilter = wrapper.findAll("button").find((button) => button.text().trim() === "WS");
    expect(wsFilter).toBeDefined();
    await wsFilter?.trigger("click");
    await flushPromises();

    expect(getSnapshotMock).toHaveBeenLastCalledWith(
      expect.objectContaining({ view: "requests", requestType: "ws_v2", page: 1 }),
      expect.any(Object),
    );
    expect(wrapper.text()).toContain("WS");
    expect(wrapper.text()).not.toContain("安全策略");
  });

  it("explains local admission rejection without presenting it as an upstream switch", async () => {
    const rejectedTrace = {
      ...schedulerTraces[0],
      status: "success" as const,
      switchCount: 0,
      accountPath: [{ id: 29, name: "account-29" }],
      attempts: [
        { id: "local-1", kind: "candidate_selected" as const, accountId: 32, accountName: "account-32", offsetMs: 0 },
        { id: "local-2", kind: "admission_rejected" as const, accountId: 32, accountName: "account-32", offsetMs: 1_904, reason: "profit_veto" },
        { id: "local-3", kind: "account_reselected" as const, accountId: 29, accountName: "account-29", offsetMs: 2_117 },
        { id: "local-4", kind: "request_success" as const, accountId: 29, accountName: "account-29", offsetMs: 8_420 },
      ],
      candidates: schedulerTraces[0].candidates.map((candidate, index) => index === 0
        ? { ...candidate, accountId: 32, state: "rejected" as const, reason: "profit_veto" }
        : candidate),
    };
    getSnapshotMock.mockResolvedValueOnce({
      enabled: true,
      generatedAt: "2026-08-10T17:00:00+08:00",
      timeRange: "1h",
      view: "requests",
      retentionMode: "memory",
      retentionMax: 1_000,
      pagination: { page: 1, pageSize: 20, total: 1, pages: 1 },
      traceCounts: { all: 1, sticky: 0, switch: 0, failed: 0 },
      metrics: {
        requests: 1, stickyRequests: 0, stickyHitRate: 0, switchedRequests: 0, switches: 0,
        switchRate: 0, stableSessions: 0, sessions: 0, sessionStability: 0,
        cacheReadTokens: 0, cacheEligibleTokens: 0, followUpCacheRate: 0,
      },
      switchReasons: [],
      groups: [],
      traces: [rejectedTrace],
      sessions: [],
    });

    const wrapper = mountView();
    await flushPromises();
    await wrapper.find("tbody tr").trigger("click");

    const dialogText = document.body.querySelector<HTMLElement>('[role="dialog"]')?.textContent ?? "";
    expect(dialogText).toContain("账号 #32 本地准入未通过");
    expect(dialogText).toContain("当前定价下未达到分组利润准入要求，未发起上游请求");
    expect(dialogText).toContain("本地重新选择账号 #29");
    expect(dialogText).toContain("本地否决");
    expect(dialogText).not.toContain("切换到账号 #29");
  });

  it("refreshes an open pending trace with its success event and first-token timing", async () => {
    const pendingTrace = {
      ...schedulerTraces[0],
      id: "request-pending",
      requestId: "request-pending",
      status: "pending" as const,
      firstTokenMs: null,
      endToEndFirstTokenMs: null,
      attempts: schedulerTraces[0].attempts.filter((attempt) => attempt.kind !== "request_success"),
    };
    const completedTrace = {
      ...pendingTrace,
      status: "success" as const,
      firstTokenMs: 2_813,
      endToEndFirstTokenMs: 2_819,
      attempts: [
        ...pendingTrace.attempts,
        {
          id: "request-pending-success",
          kind: "request_success" as const,
          accountId: pendingTrace.accountPath.at(-1)?.id,
          accountName: pendingTrace.accountPath.at(-1)?.name,
          offsetMs: 24_599,
        },
      ],
    };
    const snapshot = (trace: typeof pendingTrace | typeof completedTrace) => ({
      enabled: true,
      generatedAt: "2026-08-11T21:31:43+08:00",
      timeRange: "1h" as const,
      view: "requests" as const,
      retentionMode: "memory" as const,
      retentionMax: 1_000,
      retentionDays: 7,
      pagination: { page: 1, pageSize: 20, total: 1, pages: 1 },
      traceCounts: { all: 1, sticky: 0, switch: 0, failed: 0 },
      metrics: {
        requests: 1, stickyRequests: 0, stickyHitRate: 0, switchedRequests: 0, switches: 0,
        switchRate: 0, stableSessions: 0, sessions: 0, sessionStability: 0,
        cacheReadTokens: 0, cacheEligibleTokens: 0, followUpCacheRate: 0,
      },
      switchReasons: [],
      groups: [],
      models: [trace.model],
      accounts: [],
      apiKeys: [],
      traces: [trace],
      sessions: [],
    });
    getSnapshotMock.mockReset();
    getSnapshotMock
      .mockResolvedValueOnce(snapshot(pendingTrace))
      .mockResolvedValueOnce(snapshot(completedTrace));

    const wrapper = mountView();
    await flushPromises();
    await wrapper.find("tbody tr").trigger("click");
    await flushPromises();

    const dialogText = document.body.querySelector<HTMLElement>('[role="dialog"]')?.textContent ?? "";
    expect(getSnapshotMock).toHaveBeenCalledTimes(2);
    expect(dialogText).toContain("账号 #15 请求成功");
    expect(dialogText).toContain("2813ms");
    expect(dialogText).toContain("2819ms");
  });
});
