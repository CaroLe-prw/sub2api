<script setup lang="ts">
import { computed, onMounted, onUnmounted } from "vue";
import { useI18n } from "vue-i18n";

import Icon from "@/components/icons/Icon.vue";
import type {
  SchedulerAttempt,
  SchedulerCandidate,
  SchedulerCandidateState,
  SchedulerTrace,
} from "./types";

const props = defineProps<{
  trace: SchedulerTrace | null;
}>();

const emit = defineEmits<{
  close: [];
}>();

const { t, locale } = useI18n();

const cacheRate = computed(() => {
  if (!props.trace || props.trace.cacheEligibleTokens <= 0) return 0;
  return props.trace.cacheReadTokens / props.trace.cacheEligibleTokens;
});

const stateClasses: Record<SchedulerCandidateState, string> = {
  selected: "bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-500/10 dark:text-emerald-300 dark:ring-emerald-500/20",
  tried: "bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/20",
  eligible: "bg-gray-100 text-gray-600 ring-gray-200 dark:bg-dark-700 dark:text-dark-300 dark:ring-dark-600",
  excluded: "bg-rose-50 text-rose-700 ring-rose-200 dark:bg-rose-500/10 dark:text-rose-300 dark:ring-rose-500/20",
  rejected: "bg-orange-50 text-orange-700 ring-orange-200 dark:bg-orange-500/10 dark:text-orange-300 dark:ring-orange-500/20",
};

function close() {
  emit("close");
}

function handleEscape(event: KeyboardEvent) {
  if (event.key === "Escape" && props.trace) close();
}

onMounted(() => window.addEventListener("keydown", handleEscape));
onUnmounted(() => window.removeEventListener("keydown", handleEscape));

function formatDateTime(value: string): string {
  return new Intl.DateTimeFormat(locale.value, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(new Date(value));
}

function reasonLabel(reason?: string): string {
  if (!reason) return "—";
  const key = `admin.schedulerObservability.reasons.${reason}`;
  const translated = t(key);
  return translated === key ? reason : translated;
}

function attemptTitle(attempt: SchedulerAttempt): string {
  if (attempt.kind === "same_account_retry" && !attempt.retryLimit) {
    return t("admin.schedulerObservability.attempts.same_account_retry_without_limit", {
      account: attempt.accountId ? `#${attempt.accountId}` : "",
      retry: attempt.retryCount ?? "",
    });
  }
  if (attempt.kind === "retry_continued") {
    return t("admin.schedulerObservability.attempts.retry_continued", {
      elapsed: formatCompactDuration(attempt.offsetMs),
      budget: formatCompactDuration(attempt.budgetMs ?? 0),
      remaining: attempt.remainingCandidates == null || attempt.remainingCandidates < 0
        ? "—"
        : attempt.remainingCandidates,
    });
  }
  return t(`admin.schedulerObservability.attempts.${attempt.kind}`, {
    account: attempt.accountId ? `#${attempt.accountId}` : "",
    status: attempt.upstreamStatus ?? "",
    retry: attempt.retryCount ?? "",
    limit: attempt.retryLimit ?? "",
  });
}

function formatCompactDuration(valueMs: number): string {
  if (valueMs <= 0) return "—";
  const seconds = valueMs / 1000;
  return `${seconds >= 10 ? seconds.toFixed(0) : seconds.toFixed(1)}s`;
}

function attemptTone(attempt: SchedulerAttempt): string {
  if (attempt.kind === "request_success") return "bg-emerald-500 text-white";
  if (attempt.kind === "retry_stopped") return "bg-rose-500 text-white";
  if (attempt.kind === "retry_continued") return "bg-sky-500 text-white";
  if (["upstream_failure", "sticky_escape", "admission_rejected"].includes(attempt.kind)) {
    return "bg-amber-500 text-white";
  }
  return "bg-gray-200 text-gray-600 dark:bg-dark-700 dark:text-dark-200";
}

function attemptIcon(attempt: SchedulerAttempt): "check" | "exclamationCircle" | "swap" | "refresh" | "link" {
  if (attempt.kind === "request_success") return "check";
  if (["upstream_failure", "admission_rejected", "retry_stopped"].includes(attempt.kind)) {
    return "exclamationCircle";
  }
  if (["account_switch", "account_reselected"].includes(attempt.kind)) return "swap";
  if (attempt.kind === "retry_continued") return "refresh";
  return "link";
}

function attemptDescription(attempt: SchedulerAttempt): string {
  if (attempt.reason) return reasonLabel(attempt.reason);
  if (attempt.accountName) return attempt.accountName;
  return t("admin.schedulerObservability.attempts.noExtraDetail");
}

function candidateStateLabel(candidate: SchedulerCandidate): string {
  return t(`admin.schedulerObservability.candidateStates.${candidate.state}`);
}

function percent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}
</script>

<template>
  <Teleport to="body">
    <Transition name="scheduler-drawer">
      <div v-if="trace" class="fixed inset-0 z-[80]" role="dialog" aria-modal="true" :aria-labelledby="`scheduler-trace-title-${trace.id}`">
        <button
          type="button"
          class="absolute inset-0 cursor-default bg-gray-950/35 backdrop-blur-[1px]"
          :aria-label="t('common.close')"
          @click="close"
        ></button>
        <aside class="absolute inset-y-0 right-0 flex w-full max-w-2xl flex-col border-l border-gray-200 bg-white shadow-2xl dark:border-dark-700 dark:bg-dark-900">
          <header class="flex shrink-0 items-start justify-between gap-4 border-b border-gray-200 px-5 py-4 dark:border-dark-700 sm:px-6">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h2 :id="`scheduler-trace-title-${trace.id}`" class="text-base font-semibold text-gray-950 dark:text-white">
                  {{ t("admin.schedulerObservability.drawer.title") }}
                </h2>
                <span class="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-semibold text-amber-700 ring-1 ring-inset ring-amber-200 dark:bg-amber-500/10 dark:text-amber-300 dark:ring-amber-500/20">
                  {{ t(`admin.schedulerObservability.status.${trace.status}`) }}
                </span>
              </div>
              <p class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-dark-400">{{ trace.requestId }}</p>
            </div>
            <button
              type="button"
              class="rounded-lg p-2 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700 dark:hover:text-dark-100"
              :aria-label="t('common.close')"
              @click="close"
            >
              <Icon name="x" size="sm" />
            </button>
          </header>

          <div class="flex-1 overflow-y-auto px-5 py-5 sm:px-6">
            <section class="grid grid-cols-2 gap-3 sm:grid-cols-4" :aria-label="t('admin.schedulerObservability.drawer.requestContext')">
              <div class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800/70">
                <p class="text-[10px] font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.session") }}</p>
                <p class="mt-1.5 font-mono text-xs font-semibold text-gray-800 dark:text-dark-100">{{ trace.sessionFingerprint || "—" }}</p>
                <p class="mt-1 text-[10px] text-gray-500 dark:text-dark-400">{{ t(`admin.schedulerObservability.sessionSources.${trace.sessionSource}`) }}</p>
              </div>
              <div class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800/70">
                <p class="text-[10px] font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.group") }}</p>
                <p class="mt-1.5 truncate text-xs font-semibold text-gray-800 dark:text-dark-100">{{ trace.groupName }}</p>
                <p class="mt-1 text-[10px] text-gray-500 dark:text-dark-400">#{{ trace.groupId }}</p>
              </div>
              <div class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800/70">
                <p class="text-[10px] font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.model") }}</p>
                <p class="mt-1.5 truncate text-xs font-semibold text-gray-800 dark:text-dark-100">{{ trace.model }}</p>
                <p class="mt-1 text-[10px] text-gray-500 dark:text-dark-400">{{ trace.apiKeyName }}</p>
              </div>
              <div class="rounded-xl border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800/70">
                <p class="text-[10px] font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.started") }}</p>
                <p class="mt-1.5 text-xs font-semibold tabular-nums text-gray-800 dark:text-dark-100">{{ formatDateTime(trace.createdAt) }}</p>
                <p class="mt-1 text-[10px] text-gray-500 dark:text-dark-400">{{ trace.durationMs.toLocaleString() }}ms</p>
              </div>
            </section>

            <section class="mt-3 grid grid-cols-3 gap-3" :aria-label="t('admin.schedulerObservability.drawer.timing')">
              <div class="rounded-xl border border-gray-200 px-3 py-2.5 dark:border-dark-700">
                <p class="text-[10px] text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.attemptTtft") }}</p>
                <p class="mt-1 font-mono text-xs font-semibold tabular-nums text-gray-800 dark:text-dark-100">
                  {{ trace.firstTokenMs == null ? "—" : `${trace.firstTokenMs}ms` }}
                </p>
              </div>
              <div class="rounded-xl border border-gray-200 px-3 py-2.5 dark:border-dark-700">
                <p class="text-[10px] text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.endToEndTtft") }}</p>
                <p class="mt-1 font-mono text-xs font-semibold tabular-nums text-gray-800 dark:text-dark-100">
                  {{ trace.endToEndFirstTokenMs == null ? "—" : `${trace.endToEndFirstTokenMs}ms` }}
                </p>
              </div>
              <div class="rounded-xl border border-gray-200 px-3 py-2.5 dark:border-dark-700">
                <p class="text-[10px] text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.completedAt") }}</p>
                <p class="mt-1 font-mono text-xs font-semibold tabular-nums text-gray-800 dark:text-dark-100">+{{ trace.durationMs.toLocaleString() }}ms</p>
              </div>
            </section>

            <section class="mt-5 rounded-2xl border border-primary-100 bg-primary-50/70 p-4 dark:border-primary-500/20 dark:bg-primary-500/5">
              <div class="flex items-start gap-3">
                <span class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-xl bg-white text-primary-600 shadow-sm dark:bg-dark-800 dark:text-primary-300">
                  <Icon name="lightbulb" size="sm" />
                </span>
                <div>
                  <h3 class="text-xs font-semibold text-primary-950 dark:text-primary-100">{{ t("admin.schedulerObservability.drawer.whyTitle") }}</h3>
                  <p class="mt-1.5 text-xs leading-5 text-primary-900/75 dark:text-primary-100/75">
                    {{ t(`admin.schedulerObservability.summaryDetails.${trace.summary}`, {
                      first: trace.accountPath[0]?.id ? `#${trace.accountPath[0].id}` : "—",
                      final: trace.accountPath.at(-1)?.id ? `#${trace.accountPath.at(-1)?.id}` : "—",
                    }) }}
                  </p>
                </div>
              </div>
            </section>

            <section class="mt-6">
              <div class="flex items-center justify-between gap-3">
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t("admin.schedulerObservability.drawer.timeline") }}</h3>
                <span class="text-[11px] text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.attemptCount", { count: trace.attempts.length }) }}</span>
              </div>
              <ol class="mt-4 space-y-0">
                <li v-for="(attempt, index) in trace.attempts" :key="attempt.id" class="relative flex gap-3 pb-5 last:pb-0">
                  <span v-if="index < trace.attempts.length - 1" class="absolute bottom-0 left-[11px] top-6 w-px bg-gray-200 dark:bg-dark-700"></span>
                  <span
                    class="relative z-10 mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full ring-4 ring-white dark:ring-dark-900"
                    :class="attemptTone(attempt)"
                  >
                    <Icon
                      :name="attemptIcon(attempt)"
                      size="xs"
                      :stroke-width="2"
                    />
                  </span>
                  <div class="min-w-0 flex-1">
                    <div class="flex items-baseline justify-between gap-3">
                      <p class="text-xs font-medium text-gray-800 dark:text-dark-100">{{ attemptTitle(attempt) }}</p>
                      <span class="shrink-0 font-mono text-[10px] tabular-nums text-gray-400 dark:text-dark-500">+{{ attempt.offsetMs }}ms</span>
                    </div>
                    <p class="mt-1 text-[11px] leading-4 text-gray-500 dark:text-dark-400">{{ attemptDescription(attempt) }}</p>
                  </div>
                </li>
              </ol>
            </section>

            <section class="mt-7">
              <div class="flex items-center justify-between gap-3">
                <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t("admin.schedulerObservability.drawer.candidates") }}</h3>
                <span class="text-[11px] text-gray-500 dark:text-dark-400">
                  {{ trace.candidateScope === 'sticky_short_circuit'
                    ? t('admin.schedulerObservability.drawer.stickyShortCircuit')
                    : `Top ${trace.candidates.length}` }}
                </span>
              </div>
              <p v-if="trace.candidateScope === 'sticky_short_circuit'" class="mt-2 text-[11px] leading-4 text-gray-500 dark:text-dark-400">
                {{ t('admin.schedulerObservability.drawer.stickyShortCircuitHint') }}
              </p>
              <div class="mt-3 overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700">
                <table class="w-full text-left">
                  <thead class="bg-gray-50 text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-dark-400">
                    <tr>
                      <th class="px-3 py-2.5">{{ t("admin.schedulerObservability.drawer.account") }}</th>
                      <th class="px-3 py-2.5 text-right">{{ t("admin.schedulerObservability.drawer.baseScore") }}</th>
                      <th class="px-3 py-2.5 text-right">{{ t("admin.schedulerObservability.drawer.stickyBonus") }}</th>
                      <th class="px-3 py-2.5 text-right">{{ t("admin.schedulerObservability.drawer.totalScore") }}</th>
                      <th class="px-3 py-2.5 text-right">{{ t("admin.schedulerObservability.drawer.state") }}</th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="candidate in trace.candidates" :key="candidate.accountId" class="border-t border-gray-100 text-xs dark:border-dark-700">
                      <td class="px-3 py-3">
                        <p class="font-medium text-gray-800 dark:text-dark-100">#{{ candidate.accountId }} {{ candidate.accountName }}</p>
                        <p v-if="candidate.reason" class="mt-0.5 text-[10px] text-gray-500 dark:text-dark-400">{{ reasonLabel(candidate.reason) }}</p>
                      </td>
                      <td class="px-3 py-3 text-right font-mono tabular-nums text-gray-600 dark:text-dark-300">{{ candidate.baseScore.toFixed(2) }}</td>
                      <td class="px-3 py-3 text-right font-mono tabular-nums text-primary-600 dark:text-primary-300">+{{ candidate.stickyBonus.toFixed(2) }}</td>
                      <td class="px-3 py-3 text-right font-mono font-semibold tabular-nums text-gray-900 dark:text-white">{{ candidate.totalScore.toFixed(2) }}</td>
                      <td class="px-3 py-3 text-right">
                        <span class="inline-flex rounded-full px-2 py-0.5 text-[10px] font-semibold ring-1 ring-inset" :class="stateClasses[candidate.state]">
                          {{ candidateStateLabel(candidate) }}
                        </span>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </section>

            <section class="mt-7 rounded-2xl border border-gray-200 p-4 dark:border-dark-700">
              <div class="flex items-center justify-between gap-4">
                <div>
                  <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t("admin.schedulerObservability.drawer.cachePerformance") }}</h3>
                  <p class="mt-1 text-[11px] text-gray-500 dark:text-dark-400">{{ t("admin.schedulerObservability.drawer.cacheFormula") }}</p>
                </div>
                <p class="text-xl font-semibold tabular-nums text-sky-700 dark:text-sky-300">{{ percent(cacheRate) }}</p>
              </div>
              <div class="mt-4 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
                <div class="h-full rounded-full bg-sky-500" :style="{ width: percent(cacheRate) }"></div>
              </div>
              <div class="mt-3 flex items-center justify-between text-[11px] tabular-nums text-gray-500 dark:text-dark-400">
                <span>{{ t("admin.schedulerObservability.drawer.cacheRead") }} {{ trace.cacheReadTokens.toLocaleString() }}</span>
                <span>{{ t("admin.schedulerObservability.drawer.eligibleInput") }} {{ trace.cacheEligibleTokens.toLocaleString() }}</span>
              </div>
            </section>
          </div>
        </aside>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.scheduler-drawer-enter-active,
.scheduler-drawer-leave-active {
  transition: opacity 180ms ease;
}

.scheduler-drawer-enter-active aside,
.scheduler-drawer-leave-active aside {
  transition: transform 220ms ease;
}

.scheduler-drawer-enter-from,
.scheduler-drawer-leave-to {
  opacity: 0;
}

.scheduler-drawer-enter-from aside,
.scheduler-drawer-leave-to aside {
  transform: translateX(24px);
}

@media (prefers-reduced-motion: reduce) {
  .scheduler-drawer-enter-active,
  .scheduler-drawer-leave-active,
  .scheduler-drawer-enter-active aside,
  .scheduler-drawer-leave-active aside {
    transition: none;
  }
}
</style>
