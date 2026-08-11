<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import Icon from "@/components/icons/Icon.vue";
import type {
  SchedulerDecisionLayer,
  SchedulerDecisionSummary,
  SchedulerTrace,
  SchedulerTraceStatus,
} from "./types";

const props = defineProps<{
  traces: SchedulerTrace[];
}>();

const emit = defineEmits<{
  select: [trace: SchedulerTrace];
}>();

const { t, locale } = useI18n();

const statusClasses: Record<SchedulerTraceStatus, string> = {
  pending: "bg-sky-50 text-sky-700 ring-sky-200 dark:bg-sky-500/10 dark:text-sky-300 dark:ring-sky-500/20",
  success: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/20",
  switched: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/20",
  failed: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-500/10 dark:text-rose-300 dark:ring-rose-500/20",
  canceled: "bg-gray-100 text-gray-600 ring-gray-200 dark:bg-dark-700 dark:text-dark-300 dark:ring-dark-600",
};

const layerClasses: Record<SchedulerDecisionLayer, string> = {
  previous_response_id: "bg-violet-50 text-violet-700 dark:bg-violet-500/10 dark:text-violet-300",
  session_hash: "bg-sky-50 text-sky-700 dark:bg-sky-500/10 dark:text-sky-300",
  load_balance: "bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-dark-200",
};

const empty = computed(() => props.traces.length === 0);

function formatTime(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

function cacheRate(trace: SchedulerTrace): string {
  if (trace.cacheEligibleTokens <= 0) return "—";
  return `${((trace.cacheReadTokens / trace.cacheEligibleTokens) * 100).toFixed(1)}%`;
}

function summaryLabel(summary: SchedulerDecisionSummary): string {
  return t(`admin.schedulerObservability.summaries.${summary}`);
}

function layerLabel(layer: SchedulerDecisionLayer): string {
  return t(`admin.schedulerObservability.layers.${layer}`);
}

function statusLabel(status: SchedulerTraceStatus): string {
  return t(`admin.schedulerObservability.status.${status}`);
}
</script>

<template>
  <div class="overflow-x-auto">
    <table class="min-w-[1320px] w-full border-separate border-spacing-0">
      <thead>
        <tr class="text-left text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-300">
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.table.time") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.table.request") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.table.groupModel") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.table.session") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.table.decision") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.table.accountPath") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 text-center dark:border-dark-700">{{ t("admin.schedulerObservability.table.retrySwitch") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 text-right dark:border-dark-700">{{ t("admin.schedulerObservability.table.cache") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 text-right dark:border-dark-700">{{ t("admin.schedulerObservability.table.result") }}</th>
          <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700"><span class="sr-only">{{ t("common.actions") }}</span></th>
        </tr>
      </thead>
      <tbody v-if="!empty">
        <tr
          v-for="trace in traces"
          :key="trace.id"
          tabindex="0"
          class="group cursor-pointer outline-none transition-colors duration-150 hover:bg-gray-50 focus-visible:bg-primary-50 dark:hover:bg-dark-800/70 dark:focus-visible:bg-primary-500/10"
          @click="emit('select', trace)"
          @keydown.enter="emit('select', trace)"
          @keydown.space.prevent="emit('select', trace)"
        >
          <td class="border-b border-gray-100 px-4 py-3.5 align-top text-xs tabular-nums text-gray-600 dark:border-dark-800 dark:text-dark-300">
            {{ formatTime(trace.createdAt) }}
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 align-top dark:border-dark-800">
            <div class="max-w-[180px]">
              <p class="truncate font-mono text-xs font-medium text-gray-900 dark:text-white" :title="trace.requestId">
                {{ trace.requestId }}
              </p>
              <p class="mt-1 truncate text-[11px] text-gray-500 dark:text-dark-400">
                {{ trace.userEmail }} · {{ trace.apiKeyName || (trace.apiKeyId ? `#${trace.apiKeyId}` : "—") }}
              </p>
            </div>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 align-top dark:border-dark-800">
            <div class="max-w-[180px] space-y-1">
              <p class="truncate text-xs font-medium text-gray-800 dark:text-dark-100" :title="trace.groupName">
                {{ trace.groupName || (trace.groupId ? `#${trace.groupId}` : "—") }}
              </p>
              <p class="truncate font-mono text-[11px] text-gray-500 dark:text-dark-400" :title="trace.model">
                {{ trace.model || "—" }}
              </p>
            </div>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 align-top dark:border-dark-800">
            <div v-if="trace.sessionFingerprint" class="flex items-center gap-2">
              <span class="rounded-md bg-gray-100 px-1.5 py-1 font-mono text-[11px] font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                {{ trace.sessionFingerprint }}
              </span>
              <span v-if="trace.sessionTurn" class="text-[11px] text-gray-500 dark:text-dark-400">
                #{{ trace.sessionTurn }}
              </span>
            </div>
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ t("admin.schedulerObservability.noSession") }}</span>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 align-top dark:border-dark-800">
            <div class="max-w-[220px] space-y-1.5">
              <span class="inline-flex rounded-md px-1.5 py-1 text-[10px] font-semibold" :class="layerClasses[trace.decisionLayer]">
                {{ layerLabel(trace.decisionLayer) }}
              </span>
              <p class="line-clamp-2 text-[11px] leading-4 text-gray-600 dark:text-dark-300">
                {{ summaryLabel(trace.summary) }}
              </p>
            </div>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 align-top dark:border-dark-800">
            <div v-if="trace.accountPath.length" class="flex max-w-[230px] items-center gap-1.5 overflow-hidden">
              <template v-for="(account, index) in trace.accountPath" :key="`${trace.id}-${account.id}`">
                <Icon v-if="index > 0" name="arrowRight" size="xs" class="shrink-0 text-amber-500" />
                <span
                  class="max-w-[96px] truncate rounded-md bg-gray-100 px-1.5 py-1 font-mono text-[11px] text-gray-700 dark:bg-dark-700 dark:text-dark-200"
                  :title="`${account.name} (#${account.id})`"
                >
                  #{{ account.id }} {{ account.name }}
                </span>
              </template>
            </div>
            <span v-else class="text-xs text-gray-400 dark:text-dark-500">—</span>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 text-center align-top dark:border-dark-800">
            <div class="inline-flex items-center gap-2 text-[11px] tabular-nums">
              <span class="text-gray-500 dark:text-dark-400">R {{ trace.retryCount }}</span>
              <span :class="trace.switchCount > 0 ? 'font-semibold text-amber-600 dark:text-amber-400' : 'text-gray-500 dark:text-dark-400'">S {{ trace.switchCount }}</span>
            </div>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 text-right align-top dark:border-dark-800">
            <p class="text-xs font-semibold tabular-nums text-sky-700 dark:text-sky-300">{{ cacheRate(trace) }}</p>
            <p class="mt-1 text-[10px] tabular-nums text-gray-500 dark:text-dark-400">{{ trace.cacheReadTokens.toLocaleString() }}</p>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 text-right align-top dark:border-dark-800">
            <span class="inline-flex rounded-full px-2 py-1 text-[10px] font-semibold ring-1 ring-inset" :class="statusClasses[trace.status]">
              {{ statusLabel(trace.status) }}
            </span>
            <p class="mt-1.5 text-[10px] tabular-nums text-gray-500 dark:text-dark-400">
              {{ trace.endToEndFirstTokenMs == null ? "—" : `${trace.endToEndFirstTokenMs}ms` }}
            </p>
          </td>
          <td class="border-b border-gray-100 px-4 py-3.5 align-top dark:border-dark-800">
            <button
              type="button"
              class="rounded-lg p-1.5 text-gray-400 transition-colors hover:bg-white hover:text-primary-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700 dark:hover:text-primary-400"
              :aria-label="t('admin.schedulerObservability.openDetails', { requestId: trace.requestId })"
              @click.stop="emit('select', trace)"
            >
              <Icon name="chevronRight" size="sm" />
            </button>
          </td>
        </tr>
      </tbody>
    </table>
    <div v-if="empty" class="flex min-h-56 flex-col items-center justify-center px-6 text-center">
      <span class="flex h-11 w-11 items-center justify-center rounded-2xl bg-gray-100 text-gray-400 dark:bg-dark-800 dark:text-dark-500">
        <Icon name="search" />
      </span>
      <p class="mt-3 text-sm font-medium text-gray-700 dark:text-dark-200">{{ t("admin.schedulerObservability.empty.title") }}</p>
      <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.empty.description") }}</p>
    </div>
  </div>
</template>
