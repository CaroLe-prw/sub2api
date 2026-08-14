import { describe, expect, it } from "vitest";
import { createDefaultOpenAISchedulerTemplates } from "@/api/admin/settings";

describe("OpenAI scheduler template defaults", () => {
  it("keeps the SLA, balanced, and cost presets aligned with backend defaults", () => {
    expect(createDefaultOpenAISchedulerTemplates()).toEqual({
      sla: {
        top_k: 2,
        priority: 0.5,
        load: 1.5,
        queue: 1.5,
        error_rate: 5,
        ttft: 3.5,
        reset: 0,
        quota_headroom: 0.5,
        upstream_cost: 0.5,
        previous_response: 0.3,
        session_sticky: 0.1,
        sticky_weighted_enabled: true,
        subscription_priority_enabled: false,
      },
      balanced: {
        top_k: 3,
        priority: 0.5,
        load: 1.5,
        queue: 1.5,
        error_rate: 4,
        ttft: 2.5,
        reset: 0.2,
        quota_headroom: 0.8,
        upstream_cost: 1.5,
        previous_response: 0.3,
        session_sticky: 0.1,
        sticky_weighted_enabled: true,
        subscription_priority_enabled: false,
      },
      cost: {
        top_k: 3,
        priority: 0.3,
        load: 1.2,
        queue: 1.2,
        error_rate: 5,
        ttft: 1.5,
        reset: 0.3,
        quota_headroom: 1,
        upstream_cost: 4,
        previous_response: 0.2,
        session_sticky: 0.1,
        sticky_weighted_enabled: true,
        subscription_priority_enabled: false,
      },
    });
  });
});
