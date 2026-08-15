<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <section class="relative overflow-hidden rounded-3xl border border-primary-200/80 bg-gradient-to-br from-primary-50 via-white to-cyan-50 p-6 shadow-card dark:border-primary-900/60 dark:from-primary-950/70 dark:via-dark-900 dark:to-cyan-950/40 md:p-8">
        <div class="pointer-events-none absolute -right-16 -top-20 h-64 w-64 rounded-full border-[34px] border-primary-100/70 dark:border-primary-900/30"></div>
        <div class="pointer-events-none absolute bottom-[-90px] right-[22%] h-48 w-48 rounded-full border-[26px] border-cyan-100/60 dark:border-cyan-900/20"></div>
        <div class="relative flex flex-col gap-6 md:flex-row md:items-center md:justify-between">
          <div class="flex items-start gap-4 md:gap-5">
            <div class="flex h-16 w-16 flex-none items-center justify-center rounded-2xl bg-white text-primary-600 shadow-glass-sm dark:bg-dark-800 dark:text-primary-400 md:h-20 md:w-20">
              <Icon name="calendar" size="xl" :stroke-width="1.7" />
            </div>
            <div>
              <span class="inline-flex rounded-full border border-primary-200 bg-primary-100/70 px-3 py-1 text-xs font-semibold text-primary-700 dark:border-primary-800 dark:bg-primary-900/50 dark:text-primary-300">
                {{ t('checkIn.badge') }}
              </span>
              <h1 class="mt-3 text-3xl font-bold tracking-tight text-gray-950 dark:text-white md:text-4xl">{{ t('checkIn.title') }}</h1>
              <p class="mt-2 max-w-2xl text-sm text-gray-600 dark:text-gray-300 md:text-base">{{ t('checkIn.subtitle') }}</p>
            </div>
          </div>
          <div v-if="overview" class="grid grid-cols-2 gap-3 sm:min-w-[320px]">
            <div class="rounded-2xl border border-white/80 bg-white/75 p-4 backdrop-blur dark:border-dark-700 dark:bg-dark-800/75">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkIn.streak') }}</p>
              <p class="mt-1 text-2xl font-bold text-gray-950 dark:text-white">{{ overview.current_streak }} <span class="text-sm font-medium text-primary-600">{{ t('checkIn.daysUnit') }}</span></p>
            </div>
            <div class="rounded-2xl border border-white/80 bg-white/75 p-4 backdrop-blur dark:border-dark-700 dark:bg-dark-800/75">
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkIn.currentBalance') }}</p>
              <p class="mt-1 truncate text-2xl font-bold text-gray-950 dark:text-white">{{ formatReward(overview.balance) }}</p>
            </div>
          </div>
        </div>
      </section>

      <div v-if="loading" class="card flex min-h-[360px] items-center justify-center">
        <LoadingSpinner />
      </div>

      <div v-else-if="loadError" class="card flex min-h-[300px] flex-col items-center justify-center p-8 text-center">
        <div class="flex h-12 w-12 items-center justify-center rounded-2xl bg-red-50 text-red-500 dark:bg-red-950/40"><Icon name="exclamationCircle" size="lg" /></div>
        <p class="mt-4 text-sm text-gray-600 dark:text-gray-300">{{ loadError }}</p>
        <button type="button" class="btn btn-primary mt-5" @click="loadOverview()">{{ t('checkIn.retry') }}</button>
      </div>

      <template v-else-if="overview">
        <transition name="slide-down">
          <div v-if="successMessage" class="flex items-center gap-3 rounded-2xl border border-emerald-200 bg-emerald-50 px-5 py-4 text-sm font-semibold text-emerald-800 shadow-card dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-300">
            <Icon name="checkCircle" size="md" />
            {{ successMessage }}
          </div>
        </transition>

        <section class="card overflow-hidden">
          <div class="grid lg:grid-cols-[1fr_320px]">
            <div class="relative overflow-hidden bg-gradient-to-br from-primary-50 via-white to-cyan-50 p-6 dark:from-primary-950/60 dark:via-dark-900 dark:to-cyan-950/30 md:p-8">
              <div class="relative flex flex-col gap-6 sm:flex-row sm:items-center">
                <div class="flex h-20 w-20 flex-none items-center justify-center rounded-3xl bg-gradient-primary text-white shadow-glow">
                  <Icon :name="overview.checked_in_today ? 'checkCircle' : 'gift'" size="xl" :stroke-width="1.8" />
                </div>
                <div class="min-w-0 flex-1">
                  <p class="text-xs font-semibold uppercase tracking-[0.18em] text-primary-600 dark:text-primary-400">{{ t('checkIn.todayTitle') }}</p>
                  <h2 class="mt-2 text-2xl font-bold text-gray-950 dark:text-white">{{ overview.checked_in_today ? t('checkIn.doneTitle') : t('checkIn.readyTitle') }}</h2>
                  <p class="mt-2 text-sm text-gray-600 dark:text-gray-300">{{ overview.checked_in_today ? t('checkIn.doneDescription') : t('checkIn.readyDescription') }}</p>
                  <p v-if="overview.checked_in_today" class="mt-4 text-3xl font-bold text-primary-600 dark:text-primary-400">+{{ formatReward(overview.today_reward) }} <span class="text-sm font-medium">{{ t('checkIn.currentBalance') }}</span></p>
                  <p v-else class="mt-4 text-sm font-medium text-gray-700 dark:text-gray-200">{{ t('checkIn.rewardRange') }}：<span class="font-bold text-primary-600 dark:text-primary-400">{{ formatReward(overview.reward_min) }}–{{ formatReward(overview.reward_max) }}</span></p>
                </div>
              </div>
            </div>
            <div class="flex flex-col justify-center border-t border-gray-100 p-6 dark:border-dark-700 lg:border-l lg:border-t-0">
              <button type="button" class="btn btn-primary w-full py-3.5 text-base" :disabled="overview.checked_in_today || claiming" @click="claimReward">
                <svg v-if="claiming" class="mr-2 h-5 w-5 animate-spin" viewBox="0 0 24 24" fill="none"><circle class="opacity-25" cx="12" cy="12" r="9" stroke="currentColor" stroke-width="4"/><path class="opacity-80" fill="currentColor" d="M21 12a9 9 0 00-9-9v4a5 5 0 015 5h4z"/></svg>
                <Icon v-else :name="overview.checked_in_today ? 'checkCircle' : 'sparkles'" size="md" class="mr-2" />
                {{ claiming ? t('checkIn.claiming') : overview.checked_in_today ? t('checkIn.claimed') : t('checkIn.claim') }}
              </button>
              <p class="mt-3 text-center text-xs text-gray-500 dark:text-gray-400">{{ overview.today }} · {{ overview.timezone }}</p>
              <p v-if="claimError" class="mt-3 text-center text-xs text-red-600 dark:text-red-400">{{ claimError }}</p>
            </div>
          </div>
        </section>

        <section class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <article v-for="stat in stats" :key="stat.label" class="card relative overflow-hidden p-5">
            <div :class="['absolute inset-x-0 top-0 h-1 bg-gradient-to-r', stat.accent]"></div>
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ stat.label }}</p>
            <p class="mt-3 text-3xl font-bold text-gray-950 dark:text-white">{{ stat.value }} <span v-if="stat.unit" class="text-sm font-semibold text-primary-600 dark:text-primary-400">{{ stat.unit }}</span></p>
          </article>
        </section>

        <section class="card overflow-hidden">
          <div class="flex flex-col gap-4 border-b border-gray-100 px-5 py-5 dark:border-dark-700 sm:flex-row sm:items-center sm:justify-between md:px-6">
            <div>
              <h2 class="text-xl font-bold text-gray-950 dark:text-white">{{ t('checkIn.calendarTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('checkIn.calendarDescription') }}</p>
            </div>
            <div class="flex items-center gap-2">
              <button type="button" class="btn btn-secondary btn-sm" :aria-label="t('checkIn.previousMonth')" @click="changeMonth(-1)"><Icon name="chevronLeft" size="sm" /></button>
              <span class="min-w-[116px] text-center text-sm font-bold text-gray-900 dark:text-white">{{ monthLabel }}</span>
              <button type="button" class="btn btn-secondary btn-sm" :aria-label="t('checkIn.nextMonth')" @click="changeMonth(1)"><Icon name="chevronRight" size="sm" /></button>
            </div>
          </div>
          <div class="overflow-x-auto">
            <div class="min-w-[700px] p-4 md:p-6">
              <div class="grid grid-cols-7 border-b border-gray-100 dark:border-dark-700">
                <div v-for="weekday in weekdays" :key="weekday" class="px-2 pb-3 text-center text-xs font-semibold text-gray-500 dark:text-gray-400">{{ weekday }}</div>
              </div>
              <div class="mt-2 grid grid-cols-7 overflow-hidden rounded-2xl border border-gray-100 bg-gray-100 dark:border-dark-700 dark:bg-dark-700">
                <div v-for="(cell, index) in calendarCells" :key="`${cell?.date || 'empty'}-${index}`" :class="calendarCellClass(cell)">
                  <template v-if="cell">
                    <div class="flex items-center justify-between">
                      <span class="text-sm font-bold">{{ cell.day }}</span>
                      <span v-if="cell.isToday" class="rounded-full bg-primary-600 px-2 py-0.5 text-[10px] font-bold text-white">{{ t('checkIn.today') }}</span>
                    </div>
                    <div v-if="cell.record" class="mt-5">
                      <div class="flex items-center gap-1 text-xs font-semibold text-emerald-600 dark:text-emerald-400"><Icon name="checkCircle" size="xs" />{{ t('checkIn.checked') }}</div>
                      <p class="mt-1 text-sm font-bold text-primary-600 dark:text-primary-400">+{{ formatReward(cell.record.reward) }}</p>
                    </div>
                  </template>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section class="card p-5 md:p-6">
          <h2 class="text-xl font-bold text-gray-950 dark:text-white">{{ t('checkIn.rulesTitle') }}</h2>
          <div class="mt-5 grid gap-4 lg:grid-cols-3">
            <article v-for="(rule, index) in rules" :key="rule.title" class="flex gap-4 rounded-2xl border border-gray-100 p-4 dark:border-dark-700">
              <div class="flex h-9 w-9 flex-none items-center justify-center rounded-xl border border-primary-200 bg-primary-50 text-sm font-bold text-primary-700 dark:border-primary-800 dark:bg-primary-950/50 dark:text-primary-300">{{ index + 1 }}</div>
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
import { checkInAPI, type CheckInOverview, type CheckInRecord } from '@/api/checkIn'
import { useAuthStore } from '@/stores/auth'

interface CalendarCell {
  day: number
  date: string
  isToday: boolean
  record?: CheckInRecord
}

const { t, locale } = useI18n()
const authStore = useAuthStore()
const now = new Date()
const selectedYear = ref(now.getFullYear())
const selectedMonth = ref(now.getMonth() + 1)
const overview = ref<CheckInOverview | null>(null)
const loading = ref(true)
const claiming = ref(false)
const loadError = ref('')
const claimError = ref('')
const successMessage = ref('')

const weekdayKeys = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const
const weekdays = computed(() => weekdayKeys.map(key => t(`checkIn.weekdays.${key}`)))

const recordsByDate = computed(() => new Map((overview.value?.records || []).map(record => [record.date, record])))
const monthLabel = computed(() => new Intl.DateTimeFormat(locale.value, { year: 'numeric', month: 'long' }).format(new Date(selectedYear.value, selectedMonth.value - 1, 1)))

const calendarCells = computed<Array<CalendarCell | null>>(() => {
  const firstDay = new Date(selectedYear.value, selectedMonth.value - 1, 1).getDay()
  const leading = (firstDay + 6) % 7
  const days = new Date(selectedYear.value, selectedMonth.value, 0).getDate()
  const cells: Array<CalendarCell | null> = Array.from({ length: leading }, () => null)
  for (let day = 1; day <= days; day += 1) {
    const date = `${selectedYear.value}-${String(selectedMonth.value).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    cells.push({ day, date, isToday: date === overview.value?.today, record: recordsByDate.value.get(date) })
  }
  while (cells.length % 7 !== 0) cells.push(null)
  return cells
})

const stats = computed(() => overview.value ? [
  { label: t('checkIn.totalDays'), value: overview.value.total_days, unit: t('checkIn.daysUnit'), accent: 'from-primary-500 to-cyan-500' },
  { label: t('checkIn.monthDays'), value: overview.value.month_days, unit: t('checkIn.daysUnit'), accent: 'from-emerald-500 to-teal-500' },
  { label: t('checkIn.monthReward'), value: formatReward(overview.value.month_reward), unit: '', accent: 'from-cyan-500 to-blue-500' },
  { label: t('checkIn.totalReward'), value: formatReward(overview.value.total_reward), unit: '', accent: 'from-violet-500 to-primary-500' },
] : [])

const rules = computed(() => overview.value ? [
  { title: t('checkIn.ruleOnceTitle'), description: t('checkIn.ruleOnceDescription', { timezone: overview.value.timezone }) },
  { title: t('checkIn.ruleRandomTitle'), description: t('checkIn.ruleRandomDescription', { min: formatReward(overview.value.reward_min), max: formatReward(overview.value.reward_max) }) },
  { title: t('checkIn.ruleInstantTitle'), description: t('checkIn.ruleInstantDescription') },
] : [])

function formatReward(value: number): string {
  return Number(value || 0).toFixed(8).replace(/0+$/, '').replace(/\.$/, '') || '0'
}

function calendarCellClass(cell: CalendarCell | null): string[] {
  return [
    'min-h-[112px] border-b border-r border-white p-3 text-gray-900 dark:border-dark-800 dark:text-white',
    !cell ? 'bg-gray-50/80 dark:bg-dark-900/60' : 'bg-white dark:bg-dark-900',
    cell?.record ? 'bg-gradient-to-br from-primary-50 to-cyan-50 dark:from-primary-950/60 dark:to-cyan-950/30' : '',
    cell?.isToday ? 'relative z-10 ring-2 ring-inset ring-primary-500' : '',
  ]
}

async function loadOverview(useServerMonth = false) {
  loading.value = true
  loadError.value = ''
  try {
    overview.value = useServerMonth
      ? await checkInAPI.getOverview()
      : await checkInAPI.getOverview(selectedYear.value, selectedMonth.value)
    selectedYear.value = overview.value.year
    selectedMonth.value = overview.value.month
  } catch (error) {
    loadError.value = (error as { message?: string })?.message || t('checkIn.loadFailed')
  } finally {
    loading.value = false
  }
}

async function claimReward() {
  if (!overview.value || overview.value.checked_in_today || claiming.value) return
  claiming.value = true
  claimError.value = ''
  successMessage.value = ''
  try {
    const result = await checkInAPI.claim()
    successMessage.value = result.created
      ? t('checkIn.success', { reward: formatReward(result.record.reward) })
      : t('checkIn.alreadyClaimed')
    await Promise.all([loadOverview(), authStore.refreshUser()])
  } catch (error) {
    claimError.value = (error as { message?: string })?.message || t('checkIn.claimFailed')
  } finally {
    claiming.value = false
  }
}

async function changeMonth(offset: number) {
  const next = new Date(selectedYear.value, selectedMonth.value - 1 + offset, 1)
  selectedYear.value = next.getFullYear()
  selectedMonth.value = next.getMonth() + 1
  await loadOverview()
}

onMounted(() => loadOverview(true))
</script>
