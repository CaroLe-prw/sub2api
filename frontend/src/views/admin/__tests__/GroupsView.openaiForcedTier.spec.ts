import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { AdminGroup } from "@/types";
import GroupsView from "@/views/admin/GroupsView.vue";

const {
  listGroups,
  getModelAllowlistCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  createGroup,
  updateGroup,
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  getModelAllowlistCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  createGroup: vi.fn(),
  updateGroup: vi.fn(),
}));

vi.mock("@/api/admin", () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      getAll: vi.fn(),
      getModelAllowlistCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      create: createGroup,
      update: updateGroup,
      delete: vi.fn(),
      duplicate: vi.fn(),
      updateSortOrder: vi.fn(),
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn(),
    },
  },
}));

vi.mock("@/stores/app", () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}));

vi.mock("@/stores/onboarding", () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}));

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

const sourceGroup = {
  id: 42,
  name: "OpenAI",
  description: null,
  platform: "openai",
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: "active",
  subscription_type: "standard",
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  long_context_pricing_enabled: true,
  force_openai_fast: false,
  force_openai_ultrafast: false,
  free_openai_fast: false,
  model_pricing: [],
  profit_control_enabled: false,
  profit_min_margin: 0,
  profit_safety_buffer: 0,
  allow_image_generation: false,
  allow_batch_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  search_price_per_1k: null,
  audio_realtime_price_per_min: null,
  audio_tts_price_per_million_chars: null,
  audio_stt_price_per_hour: null,
  peak_rate_enabled: false,
  peak_start: "",
  peak_end: "",
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  allow_live: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: "2026-09-05T00:00:00Z",
  updated_at: "2026-09-05T00:00:00Z",
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: true,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  models_list_config: undefined,
  codex_models_manifest_config: {
    enabled: false,
    account_ids: [],
    fallback_to_scheduler: false,
  },
  sort_order: 10,
} satisfies AdminGroup;

const AppLayoutStub = defineComponent({
  template: "<main><slot /></main>",
});

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
});

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
  },
  template: '<div><div v-if="data.length"><slot name="cell-actions" :row="data[0]" /></div></div>',
});

const BaseDialogStub = defineComponent({
  props: {
    show: { type: Boolean, default: false },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
});

// 表单提交前会调用这些子组件的 validate()，自动桩不提供该方法。
const ValidatingFieldStub = defineComponent({
  setup(_, { expose }) {
    expose({ validate: () => true, resetValidation: () => undefined });
    return () => h("div");
  },
});

const mountView = () =>
  mount(GroupsView, {
    global: {
      plugins: [createPinia()],
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        ReasoningEffortPolicyFields: ValidatingFieldStub,
        CodexManifestAccountsField: ValidatingFieldStub,
        PricingEntryCard: true,
        VueDraggable: true,
      },
    },
  });

const openEditDialog = async (group: AdminGroup) => {
  listGroups.mockResolvedValue({
    items: [group],
    total: 1,
    page: 1,
    page_size: 20,
    pages: 1,
  });
  const wrapper = mountView();
  await flushPromises();
  const editButton = wrapper
    .findAll("button")
    .find((button) => button.text().includes("common.edit"));
  expect(editButton).toBeTruthy();
  await editButton!.trigger("click");
  await flushPromises();
  return wrapper;
};

const toggle = (
  wrapper: ReturnType<typeof mountView>,
  name: "fast" | "ultrafast",
) => wrapper.get(`[data-testid="edit-force-openai-${name}"]`);

const isOn = (wrapper: ReturnType<typeof mountView>, name: "fast" | "ultrafast") =>
  toggle(wrapper, name).attributes("aria-checked") === "true";

const submit = async (wrapper: ReturnType<typeof mountView>) => {
  await wrapper.get("#edit-group-form").trigger("submit");
  await flushPromises();
  expect(updateGroup).toHaveBeenCalledTimes(1);
  expect(updateGroup.mock.calls[0][0]).toBe(sourceGroup.id);
  return updateGroup.mock.calls[0][1];
};

describe("GroupsView OpenAI forced service tier", () => {
  beforeEach(() => {
    localStorage.clear();
    listGroups.mockReset();
    getModelAllowlistCandidates.mockReset();
    getUsageSummary.mockReset();
    getCapacitySummary.mockReset();
    getLiveCapability.mockReset();
    createGroup.mockReset();
    updateGroup.mockReset();

    getModelAllowlistCandidates.mockResolvedValue([]);
    getUsageSummary.mockResolvedValue([]);
    getCapacitySummary.mockResolvedValue([]);
    getLiveCapability.mockResolvedValue({ supported: false });
    updateGroup.mockResolvedValue({});
  });

  it("shows both switches off for a group that follows the client", async () => {
    const wrapper = await openEditDialog(sourceGroup);

    expect(isOn(wrapper, "fast")).toBe(false);
    expect(isOn(wrapper, "ultrafast")).toBe(false);

    wrapper.unmount();
  });

  it.each([
    [{ force_openai_fast: true, force_openai_ultrafast: false }],
    [{ force_openai_fast: false, force_openai_ultrafast: true }],
    [{ force_openai_fast: true, force_openai_ultrafast: true }],
  ])("loads and resaves the stored flags %o unchanged", async (flags) => {
    const wrapper = await openEditDialog({ ...sourceGroup, ...flags });

    expect(isOn(wrapper, "fast")).toBe(flags.force_openai_fast);
    expect(isOn(wrapper, "ultrafast")).toBe(flags.force_openai_ultrafast);
    expect(await submit(wrapper)).toMatchObject(flags);

    wrapper.unmount();
  });

  it("turning on Ultra Fast keeps Force Fast as the fallback for other models", async () => {
    const wrapper = await openEditDialog({
      ...sourceGroup,
      force_openai_fast: true,
    });

    await toggle(wrapper, "ultrafast").trigger("click");
    expect(isOn(wrapper, "fast")).toBe(true);
    expect(isOn(wrapper, "ultrafast")).toBe(true);

    expect(await submit(wrapper)).toMatchObject({
      force_openai_fast: true,
      force_openai_ultrafast: true,
    });

    wrapper.unmount();
  });

  it("the two switches toggle independently", async () => {
    const wrapper = await openEditDialog({
      ...sourceGroup,
      force_openai_fast: true,
      force_openai_ultrafast: true,
    });

    await toggle(wrapper, "fast").trigger("click");
    expect(isOn(wrapper, "fast")).toBe(false);
    expect(isOn(wrapper, "ultrafast")).toBe(true);

    expect(await submit(wrapper)).toMatchObject({
      force_openai_fast: false,
      force_openai_ultrafast: true,
    });

    wrapper.unmount();
  });

  it("clears both flags when both switches are turned off", async () => {
    const wrapper = await openEditDialog({
      ...sourceGroup,
      force_openai_fast: true,
      force_openai_ultrafast: true,
    });

    await toggle(wrapper, "fast").trigger("click");
    await toggle(wrapper, "ultrafast").trigger("click");

    expect(await submit(wrapper)).toMatchObject({
      force_openai_fast: false,
      force_openai_ultrafast: false,
    });

    wrapper.unmount();
  });
  it("edits ordered candy model candidates without changing priority", async () => {
    const saved = "gpt-6.2-sol\ngpt-6.1-sol\ngpt-6-sol";
    const wrapper = await openEditDialog({ ...sourceGroup, candy_test_model: saved });
    const input = wrapper.get('textarea[data-testid="candy-test-model"]');
    expect((input.element as HTMLTextAreaElement).value).toBe(saved);
    expect(input.attributes("maxlength")).toBe("2048");
    const reordered = "gpt-6.1-sol\ngpt-6.2-sol\ngpt-6-sol";
    await input.setValue(reordered);
    expect(await submit(wrapper)).toMatchObject({ candy_test_model: reordered });
    wrapper.unmount();
  });

  it("defaults the OpenAI candy test model and saves an edited model", async () => {
    const wrapper = await openEditDialog(sourceGroup);
    const input = wrapper.get('[data-testid="candy-test-model"]');
    expect((input.element as HTMLInputElement).value).toBe("gpt-6.2-sol");
    await input.setValue("gpt-6.1-sol");
    expect(await submit(wrapper)).toMatchObject({ candy_test_model: "gpt-6.1-sol" });
    wrapper.unmount();
  });

  it("hydrates the saved candy model and allows clearing to the default", async () => {
    const wrapper = await openEditDialog({ ...sourceGroup, candy_test_model: "gpt-6.1-sol" });
    const input = wrapper.get('[data-testid="candy-test-model"]');
    expect((input.element as HTMLInputElement).value).toBe("gpt-6.1-sol");
    await input.setValue("");
    expect(await submit(wrapper)).toMatchObject({ candy_test_model: "" });
    wrapper.unmount();
  });

  it("hides candy settings outside OpenAI and clears stale model configuration", async () => {
    const wrapper = await openEditDialog({ ...sourceGroup, platform: "anthropic", candy_test_model: "gpt-6.1-sol" });
    expect(wrapper.find('[data-testid="candy-test-model"]').exists()).toBe(false);
    expect(await submit(wrapper)).toMatchObject({ candy_test_model: "" });
    wrapper.unmount();
  });

  it("defaults automatic candy checks to disabled and saves whole seconds", async () => {
    const wrapper = await openEditDialog(sourceGroup);
    const input = wrapper.get('[data-testid="candy-test-interval"]');
    expect((input.element as HTMLInputElement).value).toBe("0");
    expect(input.attributes()).toMatchObject({ min: "0", max: "86400", step: "1" });
    await input.setValue("60");
    expect(await submit(wrapper)).toMatchObject({ candy_test_interval_seconds: 60 });
    wrapper.unmount();
  });

  it("hydrates a saved interval and allows disabling automatic checks", async () => {
    const wrapper = await openEditDialog({ ...sourceGroup, candy_test_interval_seconds: 300 });
    const input = wrapper.get('[data-testid="candy-test-interval"]');
    expect((input.element as HTMLInputElement).value).toBe("300");
    await input.setValue("0");
    expect(await submit(wrapper)).toMatchObject({ candy_test_interval_seconds: 0 });
    wrapper.unmount();
  });

  it.each(["-1", "1.5", "86401", ""])("rejects invalid interval %s", async (value) => {
    const wrapper = await openEditDialog(sourceGroup);
    await wrapper.get('[data-testid="candy-test-interval"]').setValue(value);
    await wrapper.get("form").trigger("submit");
    await flushPromises();
    expect(updateGroup).not.toHaveBeenCalled();
    wrapper.unmount();
  });

  it("hides and clears stale automatic checks on non-OpenAI groups", async () => {
    const wrapper = await openEditDialog({ ...sourceGroup, platform: "anthropic", candy_test_interval_seconds: 60 });
    expect(wrapper.find('[data-testid="candy-test-interval"]').exists()).toBe(false);
    expect(await submit(wrapper)).toMatchObject({ candy_test_interval_seconds: 0 });
    wrapper.unmount();
  });

  it("defaults candy effort to medium and offers all five strengths", async () => {
    const wrapper = await openEditDialog(sourceGroup);
    const select = wrapper.get('[data-testid="candy-test-reasoning-effort"]');
    expect((select.element as HTMLSelectElement).value).toBe("medium");
    expect(select.findAll("option").map(option => option.attributes("value"))).toEqual(["low", "medium", "high", "xhigh", "max"]);
    expect(await submit(wrapper)).toMatchObject({ candy_test_reasoning_effort: "medium" });
    wrapper.unmount();
  });

  it.each(["low", "medium", "high", "xhigh", "max"])("hydrates and saves candy effort %s", async (effort) => {
    const wrapper = await openEditDialog({ ...sourceGroup, candy_test_reasoning_effort: "high" });
    const select = wrapper.get('[data-testid="candy-test-reasoning-effort"]');
    expect((select.element as HTMLSelectElement).value).toBe("high");
    await select.setValue(effort);
    expect(await submit(wrapper)).toMatchObject({ candy_test_reasoning_effort: effort });
    wrapper.unmount();
  });

  it("hides and clears stale candy effort on non-OpenAI groups", async () => {
    const wrapper = await openEditDialog({ ...sourceGroup, platform: "anthropic", candy_test_reasoning_effort: "high" });
    expect(wrapper.find('[data-testid="candy-test-reasoning-effort"]').exists()).toBe(false);
    expect(await submit(wrapper)).toMatchObject({ candy_test_reasoning_effort: "" });
    wrapper.unmount();
  });

});
