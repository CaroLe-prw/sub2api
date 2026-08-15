<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <section class="relative overflow-hidden rounded-3xl border border-violet-200/80 bg-gradient-to-br from-violet-50 via-fuchsia-50/70 to-rose-50 p-6 shadow-card dark:border-violet-900/60 dark:from-violet-950/70 dark:via-dark-900 dark:to-rose-950/40 md:p-8">
        <div class="pointer-events-none absolute -right-14 -top-20 h-64 w-64 rounded-full border-[34px] border-rose-100/70 dark:border-rose-900/20"></div>
        <div class="pointer-events-none absolute -bottom-24 right-[20%] h-52 w-52 rounded-full border-[28px] border-violet-100/70 dark:border-violet-900/25"></div>
        <div class="relative flex items-center gap-5">
          <div class="flex h-16 w-16 flex-none items-center justify-center rounded-2xl bg-white text-violet-600 shadow-glass-sm dark:bg-dark-800 dark:text-violet-400 md:h-20 md:w-20">
            <Icon name="gift" size="xl" :stroke-width="1.7" />
          </div>
          <div>
            <span class="inline-flex rounded-full border border-violet-200 bg-violet-100/80 px-3 py-1 text-xs font-semibold text-violet-700 dark:border-violet-800 dark:bg-violet-900/50 dark:text-violet-300">{{ t('lottery.badge') }}</span>
            <h1 class="mt-3 text-3xl font-bold tracking-tight text-gray-950 dark:text-white md:text-4xl">{{ t('lottery.title') }}</h1>
            <p class="mt-2 max-w-2xl text-sm text-gray-600 dark:text-gray-300 md:text-base">{{ t('lottery.subtitle') }}</p>
          </div>
        </div>
      </section>

      <div v-if="loading" class="card flex min-h-[340px] items-center justify-center"><LoadingSpinner /></div>
      <div v-else-if="loadError" class="card flex min-h-[280px] flex-col items-center justify-center p-8 text-center">
        <Icon name="exclamationCircle" size="lg" class="text-red-500" />
        <p class="mt-4 text-sm text-gray-600 dark:text-gray-300">{{ loadError }}</p>
        <button class="btn btn-primary mt-5" type="button" @click="loadOverview">{{ t('lottery.retry') }}</button>
      </div>

      <template v-else-if="overview">
        <transition name="slide-down">
          <div v-if="successMessage" class="flex items-center gap-3 rounded-2xl border border-emerald-200 bg-emerald-50 px-5 py-4 text-sm font-semibold text-emerald-800 shadow-card dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">
            <Icon name="checkCircle" size="md" />{{ successMessage }}
          </div>
        </transition>

        <section class="card overflow-hidden">
          <div class="border-b border-gray-100 px-5 py-5 dark:border-dark-700 md:px-6">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p class="text-xs font-semibold uppercase tracking-[0.18em] text-violet-600 dark:text-violet-400">{{ t('lottery.currentTitle') }}</p>
                <h2 class="mt-2 text-xl font-bold text-gray-950 dark:text-white">{{ currentTitle }}</h2>
              </div>
              <span v-if="overview.current_round" :class="statusBadgeClass">{{ t(`lottery.${overview.current_round.status}`) }}</span>
            </div>
          </div>

          <div v-if="!overview.current_round" class="flex min-h-[240px] flex-col items-center justify-center p-8 text-center">
            <div class="flex h-14 w-14 items-center justify-center rounded-2xl bg-violet-50 text-violet-500 dark:bg-violet-950/50"><Icon name="sparkles" size="lg" /></div>
            <h3 class="mt-4 font-semibold text-gray-900 dark:text-white">{{ overview.enabled ? t('lottery.noRound') : t('lottery.disabled') }}</h3>
            <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.noRoundHint') }}</p>
          </div>

          <div v-else class="grid lg:grid-cols-[1fr_320px]">
            <div class="p-5 md:p-6">
              <div class="grid gap-3 sm:grid-cols-3">
                <div v-for="item in roundStats" :key="item.label" class="rounded-2xl border border-gray-100 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-800/60">
                  <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400"><Icon :name="item.icon" size="sm" />{{ item.label }}</div>
                  <p class="mt-2 text-sm font-semibold text-gray-950 dark:text-white">{{ item.value }}</p>
                </div>
              </div>

              <h3 class="mt-6 text-sm font-semibold text-gray-900 dark:text-white">{{ t('lottery.prizePool') }}</h3>
              <div class="mt-3 grid gap-3 sm:grid-cols-3">
                <article v-for="prize in overview.current_round.prizes" :key="prize.tier" :class="['rounded-2xl border p-4', prizeCardClass(prize.tier)]">
                  <div class="flex items-center justify-between gap-2">
                    <span class="text-xs font-bold uppercase tracking-wider">NO.{{ prize.tier }}</span>
                    <Icon :name="prize.tier === 1 ? 'trophy' : 'sparkles'" size="sm" />
                  </div>
                  <h4 class="mt-3 font-bold text-gray-950 dark:text-white">{{ prize.name }}</h4>
                  <p class="mt-2 text-2xl font-bold">+{{ formatReward(prize.reward) }}</p>
                  <p class="mt-1 text-xs opacity-75">{{ t('lottery.balanceReward') }} · {{ chance(prize) }}%</p>
                </article>
              </div>
            </div>

            <div class="flex flex-col justify-center border-t border-gray-100 p-6 dark:border-dark-700 lg:border-l lg:border-t-0">
              <div v-if="currentEntry" class="text-center">
                <div class="mx-auto flex h-14 w-14 items-center justify-center rounded-2xl bg-emerald-50 text-emerald-600 dark:bg-emerald-950/40 dark:text-emerald-400"><Icon name="checkCircle" size="lg" /></div>
                <h3 class="mt-4 font-bold text-gray-950 dark:text-white">{{ t('lottery.entered') }}</h3>
                <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('lottery.enteredHint') }}</p>
              </div>
              <template v-else>
                <button type="button" class="btn w-full bg-gradient-to-r from-violet-600 to-fuchsia-600 py-3.5 text-base text-white hover:from-violet-700 hover:to-fuchsia-700" :disabled="!canEnter || entering" @click="enterLottery">
                  <Icon :name="entering ? 'clock' : 'sparkles'" size="md" class="mr-2" />
                  {{ entering ? t('lottery.entering') : t('lottery.enter') }}
                </button>
                <p v-if="enterError" class="mt-3 text-center text-xs text-red-600 dark:text-red-400">{{ enterError }}</p>
              </template>
            </div>
          </div>
        </section>

        <section class="card p-5 md:p-6">
          <h2 class="text-xl font-bold text-gray-950 dark:text-white">{{ t('lottery.myEntries') }}</h2>
          <div v-if="overview.my_entries.length" class="mt-5 space-y-3">
            <article v-for="entry in overview.my_entries" :key="entry.id" class="flex flex-col gap-3 rounded-2xl border border-gray-100 p-4 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <p class="text-xs text-gray-500 dark:text-gray-400">#{{ entry.round_id }} · {{ formatDate(entry.entered_at) }}</p>
                <p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ entryTitle(entry) }}</p>
                <p v-if="entry.cancelled_at" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('lottery.cancelledHint') }}</p>
              </div>
              <div v-if="entry.prize_name" class="text-left sm:text-right">
                <p class="text-xl font-bold text-violet-600 dark:text-violet-400">+{{ formatReward(entry.reward) }}</p>
                <p class="text-xs text-emerald-600 dark:text-emerald-400">{{ t('lottery.credited') }}</p>
              </div>
              <span v-else-if="entry.cancelled_at" class="inline-flex w-fit rounded-full bg-gray-100 px-3 py-1 text-xs font-semibold text-gray-700 dark:bg-dark-700 dark:text-gray-300">{{ t('lottery.cancelled') }}</span>
              <span v-else class="inline-flex w-fit rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">{{ t('lottery.pending') }}</span>
            </article>
          </div>
          <p v-else class="mt-4 text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.noEntries') }}</p>
        </section>

        <section class="card p-5 md:p-6">
          <h2 class="text-xl font-bold text-gray-950 dark:text-white">{{ t('lottery.rulesTitle') }}</h2>
          <div class="mt-5 grid gap-4 lg:grid-cols-3">
            <article v-for="(rule, index) in rules" :key="rule.title" class="flex gap-4 rounded-2xl border border-gray-100 p-4 dark:border-dark-700">
              <div class="flex h-9 w-9 flex-none items-center justify-center rounded-xl border border-violet-200 bg-violet-50 text-sm font-bold text-violet-700 dark:border-violet-800 dark:bg-violet-950/50 dark:text-violet-300">{{ index + 1 }}</div>
              <div><h3 class="font-semibold text-gray-900 dark:text-white">{{ rule.title }}</h3><p class="mt-1 text-sm leading-6 text-gray-500 dark:text-gray-400">{{ rule.description }}</p></div>
            </article>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { lotteryAPI, type LotteryEntry, type LotteryOverview, type LotteryPrize } from '@/api/lottery'

const { t, locale } = useI18n()
const overview = ref<LotteryOverview | null>(null)
const loading = ref(true)
const entering = ref(false)
const loadError = ref('')
const enterError = ref('')
const successMessage = ref('')

const currentEntry = computed<LotteryEntry | undefined>(() => {
  const roundID = overview.value?.current_round?.id
  return overview.value?.my_entries.find(entry => entry.round_id === roundID)
})
const canEnter = computed(() => overview.value?.enabled === true && overview.value.current_round?.status === 'open' && !currentEntry.value)
const currentTitle = computed(() => overview.value?.current_round ? `#${overview.value.current_round.id}` : (overview.value?.enabled ? t('lottery.noRound') : t('lottery.disabled')))
const statusBadgeClass = computed(() => {
  const base = 'inline-flex rounded-full px-3 py-1 text-xs font-semibold'
  const status = overview.value?.current_round?.status
  if (status === 'open') return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300`
  if (status === 'drawing') return `${base} bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300`
  return `${base} bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300`
})
const roundStats = computed(() => {
  const round = overview.value?.current_round
  if (!round) return []
  return [
    { label: t('lottery.startsAt'), value: formatDate(round.starts_at), icon: 'calendar' as const },
    { label: t('lottery.endsAt'), value: formatDate(round.ends_at), icon: 'clock' as const },
    { label: t('lottery.participants'), value: `${round.participant_count} ${t('lottery.people')}`, icon: 'users' as const },
  ]
})
const rules = computed(() => [
  { title: t('lottery.ruleOnceTitle'), description: t('lottery.ruleOnceDescription') },
  { title: t('lottery.ruleRandomTitle'), description: t('lottery.ruleRandomDescription') },
  { title: t('lottery.ruleAutoTitle'), description: t('lottery.ruleAutoDescription') },
])

function formatReward(value: number): string {
  return Number(value || 0).toFixed(8).replace(/0+$/, '').replace(/\.$/, '') || '0'
}
function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
function chance(prize: LotteryPrize): string {
  const prizes = overview.value?.current_round?.prizes || []
  const total = prizes.reduce((sum, item) => sum + item.weight, 0)
  return total > 0 ? ((prize.weight / total) * 100).toFixed(1).replace('.0', '') : '0'
}
function entryTitle(entry: LotteryEntry): string {
  if (entry.prize_name) return t('lottery.won', { name: entry.prize_name })
  if (entry.cancelled_at) return t('lottery.cancelled')
  return t('lottery.pending')
}
function prizeCardClass(tier: number): string {
  if (tier === 1) return 'border-amber-200 bg-gradient-to-br from-amber-50 to-yellow-50 text-amber-700 dark:border-amber-900/60 dark:from-amber-950/40 dark:to-dark-900 dark:text-amber-300'
  if (tier === 2) return 'border-violet-200 bg-gradient-to-br from-violet-50 to-fuchsia-50 text-violet-700 dark:border-violet-900/60 dark:from-violet-950/40 dark:to-dark-900 dark:text-violet-300'
  return 'border-cyan-200 bg-gradient-to-br from-cyan-50 to-sky-50 text-cyan-700 dark:border-cyan-900/60 dark:from-cyan-950/40 dark:to-dark-900 dark:text-cyan-300'
}
async function loadOverview() {
  loading.value = true
  loadError.value = ''
  try {
    overview.value = await lotteryAPI.getOverview()
  } catch (error) {
    loadError.value = (error as { message?: string })?.message || t('lottery.loadFailed')
  } finally {
    loading.value = false
  }
}
async function enterLottery() {
  if (!canEnter.value) return
  entering.value = true
  enterError.value = ''
  successMessage.value = ''
  try {
    await lotteryAPI.enter()
    successMessage.value = t('lottery.enterSuccess')
    await loadOverview()
  } catch (error) {
    enterError.value = (error as { message?: string })?.message || t('lottery.enterFailed')
  } finally {
    entering.value = false
  }
}

onMounted(loadOverview)
</script>
