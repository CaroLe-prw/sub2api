<script setup lang="ts">
import { useI18n } from "vue-i18n";

import Icon from "@/components/icons/Icon.vue";
import type { SchedulerSessionSource, SchedulerSessionSummary } from "./types";

defineProps<{
  sessions: SchedulerSessionSummary[];
}>();

const emit = defineEmits<{
  "search-traces": [fingerprint: string];
}>();

const { t, locale } = useI18n();

const accountColors = [
  "bg-primary-500 ring-primary-200 dark:ring-primary-500/30",
  "bg-amber-500 ring-amber-200 dark:ring-amber-500/30",
  "bg-violet-500 ring-violet-200 dark:ring-violet-500/30",
  "bg-emerald-500 ring-emerald-200 dark:ring-emerald-500/30",
];

function formatTime(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

function percent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function sourceLabel(source: SchedulerSessionSource): string {
  return t(`admin.schedulerObservability.sessionSources.${source}`);
}

function accountColor(session: SchedulerSessionSummary, accountId: number): string {
  const index = Math.max(session.accountIds.indexOf(accountId), 0);
  return accountColors[index % accountColors.length];
}

function accountTitle(session: SchedulerSessionSummary, accountId: number, turnIndex: number): string {
  const accountName = session.accountNames?.[String(accountId)] || `#${accountId}`;
  return t("admin.schedulerObservability.sessions.turnAccountTitle", {
    turn: turnIndex + 1,
    account: accountName,
    id: accountId,
  });
}
</script>

<template>
  <div class="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
    <div class="card overflow-hidden">
      <div class="overflow-x-auto">
        <table class="min-w-[960px] w-full border-separate border-spacing-0">
          <thead>
            <tr class="text-left text-[11px] font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-300">
              <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.session") }}</th>
              <th class="border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.user") }}</th>
              <th class="border-b border-gray-200 px-4 py-3 text-center dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.turns") }}</th>
              <th class="w-[260px] border-b border-gray-200 px-4 py-3 dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.accountJourney") }}</th>
              <th class="border-b border-gray-200 px-4 py-3 text-right dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.stickyRate") }}</th>
              <th class="border-b border-gray-200 px-4 py-3 text-right dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.cacheRate") }}</th>
              <th class="border-b border-gray-200 px-4 py-3 text-right dark:border-dark-700">{{ t("admin.schedulerObservability.sessions.lastActive") }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="session in sessions" :key="session.fingerprint" class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60">
              <td class="border-b border-gray-100 px-4 py-4 align-top dark:border-dark-800">
                <div class="flex items-center gap-2">
                  <button
                    type="button"
                    class="cursor-pointer rounded-md bg-gray-100 px-1.5 py-1 font-mono text-xs font-medium text-primary-700 underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:bg-dark-700 dark:text-primary-300"
                    :title="t('admin.schedulerObservability.sessions.searchTraces', { session: session.fingerprint })"
                    @click="emit('search-traces', session.fingerprint)"
                  >
                    {{ session.fingerprint }}
                  </button>
                  <span class="rounded-full bg-sky-50 px-2 py-0.5 text-[10px] font-medium text-sky-700 dark:bg-sky-500/10 dark:text-sky-300">
                    {{ sourceLabel(session.source) }}
                  </span>
                </div>
                <p class="mt-1.5 text-[11px] text-gray-500 dark:text-dark-400">{{ session.groupName }} · {{ session.model }}</p>
              </td>
              <td class="border-b border-gray-100 px-4 py-4 align-top dark:border-dark-800">
                <p class="max-w-[180px] truncate text-xs font-medium text-gray-800 dark:text-dark-100">{{ session.userEmail }}</p>
                <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">#{{ session.userId }} · {{ session.apiKeyName }}</p>
              </td>
              <td class="border-b border-gray-100 px-4 py-4 text-center align-top text-sm font-semibold tabular-nums text-gray-800 dark:border-dark-800 dark:text-dark-100">
                {{ session.turns }}
              </td>
              <td class="w-[260px] max-w-[260px] border-b border-gray-100 px-4 py-4 align-top dark:border-dark-800">
                <div class="flex w-[228px] flex-wrap items-center gap-1.5" :aria-label="t('admin.schedulerObservability.sessions.turnJourneyLabel', { count: session.turns })">
                  <span
                    v-for="(accountId, index) in session.turnAccounts"
                    :key="`${session.fingerprint}-${index}`"
                    class="h-2.5 w-2.5 shrink-0 rounded-full ring-2"
                    :class="accountColor(session, accountId)"
                    :title="accountTitle(session, accountId, index)"
                  ></span>
                </div>
                <div class="mt-2 flex flex-wrap items-center gap-2 text-[11px]">
                  <span class="min-w-0 break-words text-gray-500 dark:text-dark-400">{{ session.accountIds.map((id) => `#${id}`).join(" → ") }}</span>
                  <span
                    class="rounded-full px-1.5 py-0.5 font-medium"
                    :class="session.switchCount > 0 ? 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300'"
                  >
                    {{ t("admin.schedulerObservability.sessions.switchCount", { count: session.switchCount }) }}
                  </span>
                </div>
              </td>
              <td class="border-b border-gray-100 px-4 py-4 text-right align-top text-xs font-semibold tabular-nums text-gray-800 dark:border-dark-800 dark:text-dark-100">
                {{ percent(session.stickyHitRate) }}
              </td>
              <td class="border-b border-gray-100 px-4 py-4 text-right align-top dark:border-dark-800">
                <div class="ml-auto w-24">
                  <p class="text-xs font-semibold tabular-nums text-sky-700 dark:text-sky-300">{{ percent(session.followUpCacheRate) }}</p>
                  <div class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                    <div class="h-full rounded-full bg-sky-500" :style="{ width: percent(session.followUpCacheRate) }"></div>
                  </div>
                </div>
              </td>
              <td class="border-b border-gray-100 px-4 py-4 text-right align-top text-xs tabular-nums text-gray-500 dark:border-dark-800 dark:text-dark-400">
                {{ formatTime(session.lastActiveAt) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <aside class="card h-fit p-5">
      <div class="flex items-start gap-3">
        <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-violet-50 text-violet-600 dark:bg-violet-500/10 dark:text-violet-300">
          <Icon name="lightbulb" size="sm" />
        </span>
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t("admin.schedulerObservability.sessions.howToRead") }}</h3>
          <p class="mt-1 text-xs leading-5 text-gray-600 dark:text-dark-300">{{ t("admin.schedulerObservability.sessions.howToReadDescription") }}</p>
        </div>
      </div>
      <dl class="mt-5 space-y-4 border-t border-gray-100 pt-4 text-xs dark:border-dark-700">
        <div>
          <dt class="font-medium text-gray-800 dark:text-dark-100">{{ t("admin.schedulerObservability.sessions.stabilityTitle") }}</dt>
          <dd class="mt-1 leading-5 text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.sessions.stabilityHint") }}</dd>
        </div>
        <div>
          <dt class="font-medium text-gray-800 dark:text-dark-100">{{ t("admin.schedulerObservability.sessions.cacheTitle") }}</dt>
          <dd class="mt-1 leading-5 text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.sessions.cacheHint") }}</dd>
        </div>
        <div>
          <dt class="font-medium text-gray-800 dark:text-dark-100">{{ t("admin.schedulerObservability.sessions.fingerprintTitle") }}</dt>
          <dd class="mt-1 leading-5 text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.sessions.fingerprintHint") }}</dd>
        </div>
      </dl>
    </aside>
  </div>
</template>
