<script setup lang="ts">
import { useI18n } from "vue-i18n";

const enabled = defineModel<boolean>("enabled", { required: true });
const minMarginPercent = defineModel<number | string | null>("minMarginPercent", {
  required: true,
});
const safetyBufferPercent = defineModel<number | string | null>(
  "safetyBufferPercent",
  { required: true },
);

const { t } = useI18n();
</script>

<template>
  <div class="border-t pt-4" data-testid="group-profit-control-field">
    <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
      <input
        v-model="enabled"
        type="checkbox"
        class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
      />
      <span>{{ t("admin.groups.profitControl.enable") }}</span>
    </label>
    <p class="mb-3 mt-1.5 text-xs text-gray-500 dark:text-gray-400">
      {{
        enabled
          ? t("admin.groups.profitControl.enabledHint")
          : t("admin.groups.profitControl.disabledHint")
      }}
    </p>
    <div v-if="enabled" class="mb-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t("admin.groups.profitControl.minMargin") }}</label>
        <input
          v-model.number="minMarginPercent"
          type="number"
          step="0.1"
          min="0"
          max="99.99"
          class="input"
          placeholder="0"
          :title="t('admin.groups.profitControl.minMarginHint')"
        />
      </div>
      <div>
        <label class="input-label">{{ t("admin.groups.profitControl.safetyBuffer") }}</label>
        <input
          v-model.number="safetyBufferPercent"
          type="number"
          step="0.1"
          min="0"
          max="99.99"
          class="input"
          placeholder="0"
          :title="t('admin.groups.profitControl.safetyBufferHint')"
        />
      </div>
    </div>
  </div>
</template>
