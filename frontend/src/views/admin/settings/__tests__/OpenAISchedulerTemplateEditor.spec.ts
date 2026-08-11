import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import { createDefaultOpenAISchedulerTemplates } from "@/api/admin/settings";
import OpenAISchedulerTemplateEditor from "../OpenAISchedulerTemplateEditor.vue";

const messages = vi.hoisted<Record<string, string>>(() => ({
  "admin.settings.openaiSchedulerTemplates.resetDefaults": "Restore recommended defaults",
  "admin.settings.openaiSchedulerTemplates.resetDefaultsHint":
    "Restore all three recommended templates. Changes take effect after saving the page.",
}));

vi.mock("vue-i18n", async (importOriginal) => {
  const actual = await importOriginal<typeof import("vue-i18n")>();

  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  };
});

describe("OpenAISchedulerTemplateEditor", () => {
  it("replaces all persisted templates with the current recommended defaults", async () => {
    const persistedTemplates = createDefaultOpenAISchedulerTemplates();
    persistedTemplates.sla.previous_response = 1.5;
    persistedTemplates.balanced.session_sticky = 0.5;
    persistedTemplates.cost.upstream_cost = 8;

    const wrapper = mount(OpenAISchedulerTemplateEditor, {
      props: {
        modelValue: persistedTemplates,
      },
      global: {
        stubs: {
          Toggle: true,
        },
      },
    });

    const resetButton = wrapper.get(
      '[data-testid="openai-scheduler-reset-defaults"]',
    );
    expect(resetButton.attributes("title")).toBe(
      messages["admin.settings.openaiSchedulerTemplates.resetDefaultsHint"],
    );

    await resetButton.trigger("click");

    expect(wrapper.emitted("update:modelValue")).toEqual([
      [createDefaultOpenAISchedulerTemplates()],
    ]);
    expect(persistedTemplates.sla.previous_response).toBe(1.5);
    expect(persistedTemplates.balanced.session_sticky).toBe(0.5);
    expect(persistedTemplates.cost.upstream_cost).toBe(8);
  });
});
