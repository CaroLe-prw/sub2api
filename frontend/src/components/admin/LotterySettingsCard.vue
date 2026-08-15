<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('lottery.admin.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.admin.description') }}</p>
        </div>
        <span v-if="config?.current_round" class="rounded-full bg-violet-50 px-3 py-1 text-xs font-semibold text-violet-700 dark:bg-violet-950/40 dark:text-violet-300">
          {{ t('lottery.admin.participants', { count: config.current_round.participant_count }) }}
        </span>
      </div>
    </div>

    <div v-if="loading" class="flex min-h-[180px] items-center justify-center"><LoadingSpinner /></div>
    <div v-else class="space-y-5 p-6">
      <div class="flex items-center justify-between gap-4">
        <div>
          <label class="input-label">{{ t('lottery.admin.enabled') }}</label>
          <p class="input-hint">{{ t('lottery.admin.enabledHint') }}</p>
        </div>
        <Toggle v-model="form.enabled" />
      </div>

      <div v-if="!form.enabled && config?.current_round" class="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-xs leading-5 text-red-800 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
        {{ t('lottery.admin.cancelWarning') }}
      </div>

      <div v-if="form.enabled" class="space-y-5">
        <div v-if="locked" class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
          {{ t('lottery.admin.locked') }}
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('lottery.admin.startsAt') }}</label>
            <input v-model="form.startsAt" type="datetime-local" class="input mt-1" :disabled="locked" />
          </div>
          <div>
            <label class="input-label">{{ t('lottery.admin.endsAt') }}</label>
            <input v-model="form.endsAt" type="datetime-local" class="input mt-1" :disabled="locked" />
          </div>
        </div>

        <div class="grid gap-4 lg:grid-cols-3">
          <article v-for="(prize, index) in form.prizes" :key="prize.tier" class="rounded-2xl border border-gray-100 p-4 dark:border-dark-700">
            <div class="flex items-center justify-between gap-2">
              <h3 class="font-semibold text-gray-900 dark:text-white">{{ prize.name }}</h3>
              <span class="rounded-full bg-violet-50 px-2 py-1 text-xs font-semibold text-violet-700 dark:bg-violet-950/50 dark:text-violet-300">{{ t('lottery.admin.chance', { chance: chance(index) }) }}</span>
            </div>
            <div class="mt-4 grid grid-cols-2 gap-3">
              <div>
                <label class="input-label">{{ t('lottery.admin.reward') }}</label>
                <input v-model.number="prize.reward" type="number" min="0.00000001" max="10000" step="0.00000001" class="input mt-1" :disabled="locked" />
              </div>
              <div>
                <label class="input-label">{{ t('lottery.admin.weight') }}</label>
                <input v-model.number="prize.weight" type="number" min="1" max="1000000" step="1" class="input mt-1" :disabled="locked" />
              </div>
            </div>
          </article>
        </div>
      </div>

      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">
          {{ saving ? t('lottery.admin.saving') : t('lottery.admin.save') }}
        </button>
      </div>
    </div>

    <div class="border-t border-gray-100 px-6 py-5 dark:border-dark-700">
      <h3 class="font-semibold text-gray-900 dark:text-white">{{ t('lottery.admin.resultsTitle') }}</h3>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.admin.resultsDescription') }}</p>

      <div v-if="resultsLoading" class="flex min-h-[140px] items-center justify-center"><LoadingSpinner /></div>
      <div v-else class="mt-4">
        <p v-if="resultsError" class="text-sm text-red-600 dark:text-red-400">{{ resultsError }}</p>
        <div v-else-if="results.length" class="overflow-x-auto rounded-xl border border-gray-100 dark:border-dark-700">
          <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 text-left text-xs font-semibold text-gray-500 dark:bg-dark-800 dark:text-gray-400">
              <tr>
                <th class="px-4 py-3">{{ t('lottery.admin.round') }}</th>
                <th class="px-4 py-3">{{ t('lottery.admin.winner') }}</th>
                <th class="px-4 py-3">{{ t('lottery.admin.prize') }}</th>
                <th class="px-4 py-3">{{ t('lottery.admin.rewardAmount') }}</th>
                <th class="px-4 py-3">{{ t('lottery.admin.settledAt') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
              <tr v-for="result in results" :key="result.entry_id">
                <td class="whitespace-nowrap px-4 py-3 font-medium text-gray-900 dark:text-white">#{{ result.round_id }}</td>
                <td class="px-4 py-3">
                  <p class="font-medium text-gray-900 dark:text-white">{{ result.username || `#${result.user_id}` }}</p>
                  <p class="mt-0.5 text-xs text-gray-500">{{ result.email }}</p>
                </td>
                <td class="whitespace-nowrap px-4 py-3"><span :class="prizeBadgeClass(result.prize_tier)">{{ result.prize_name }}</span></td>
                <td class="whitespace-nowrap px-4 py-3 font-semibold text-emerald-600 dark:text-emerald-400">+{{ formatReward(result.reward) }}</td>
                <td class="whitespace-nowrap px-4 py-3 text-gray-500 dark:text-gray-400">{{ result.settled_at ? formatDate(result.settled_at) : '-' }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('lottery.admin.noResults') }}</p>

        <div v-if="!resultsError && resultsTotal > 0" class="mt-4 flex flex-wrap items-center justify-between gap-3">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('lottery.admin.pageSummary', { page: resultsPage, pages: resultsPages, total: resultsTotal }) }}</p>
          <div class="flex gap-2">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="resultsPage <= 1" @click="changeResultsPage(resultsPage - 1)">{{ t('lottery.admin.previous') }}</button>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="resultsPage >= resultsPages" @click="changeResultsPage(resultsPage + 1)">{{ t('lottery.admin.next') }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import lotteryAdminAPI, { type LotteryAdminConfig, type LotteryAdminResult } from '@/api/admin/lottery'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'

interface PrizeForm {
  tier: number
  name: string
  reward: number
  weight: number
}

const { t, locale } = useI18n()
const appStore = useAppStore()
const config = ref<LotteryAdminConfig | null>(null)
const loading = ref(true)
const saving = ref(false)
const errorMessage = ref('')
const results = ref<LotteryAdminResult[]>([])
const resultsLoading = ref(true)
const resultsError = ref('')
const resultsPage = ref(1)
const resultsPages = ref(1)
const resultsTotal = ref(0)
const resultsPageSize = 20
const form = reactive({ enabled: true, startsAt: '', endsAt: '', prizes: [] as PrizeForm[] })
const locked = computed(() => (config.value?.current_round?.participant_count || 0) > 0)

function toLocalInput(value: Date | string): string {
  const date = value instanceof Date ? value : new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}
function applyConfig(value: LotteryAdminConfig) {
  config.value = value
  form.enabled = value.enabled
  const now = new Date()
  const round = value.current_round
  form.startsAt = toLocalInput(round?.starts_at || now)
  form.endsAt = toLocalInput(round?.ends_at || new Date(now.getTime() + 7 * 24 * 60 * 60_000))
  form.prizes = (round?.prizes || value.defaults).map(prize => ({ ...prize }))
}
function chance(index: number): string {
  const total = form.prizes.reduce((sum, prize) => sum + Number(prize.weight || 0), 0)
  return total > 0 ? ((Number(form.prizes[index]?.weight || 0) / total) * 100).toFixed(1).replace('.0', '') : '0'
}
function formatReward(value: number): string {
  return Number(value || 0).toFixed(8).replace(/0+$/, '').replace(/\.$/, '') || '0'
}
function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
function prizeBadgeClass(tier: number): string {
  const base = 'inline-flex rounded-full px-2.5 py-1 text-xs font-semibold'
  if (tier === 1) return `${base} bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300`
  if (tier === 2) return `${base} bg-violet-50 text-violet-700 dark:bg-violet-950/40 dark:text-violet-300`
  return `${base} bg-cyan-50 text-cyan-700 dark:bg-cyan-950/40 dark:text-cyan-300`
}
async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    applyConfig(await lotteryAdminAPI.getConfig())
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, t('lottery.admin.loadFailed'))
  } finally {
    loading.value = false
  }
}
async function save() {
  saving.value = true
  errorMessage.value = ''
  try {
    const [first, second, third] = form.prizes
    const updated = await lotteryAdminAPI.updateConfig({
      enabled: form.enabled,
      starts_at: form.startsAt ? new Date(form.startsAt).toISOString() : '',
      ends_at: form.endsAt ? new Date(form.endsAt).toISOString() : '',
      first_prize_reward: Number(first?.reward || 0),
      first_prize_weight: Number(first?.weight || 0),
      second_prize_reward: Number(second?.reward || 0),
      second_prize_weight: Number(second?.weight || 0),
      third_prize_reward: Number(third?.reward || 0),
      third_prize_weight: Number(third?.weight || 0),
    })
    applyConfig(updated)
    await appStore.fetchPublicSettings(true)
    appStore.showSuccess(t('lottery.admin.saved'))
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, t('lottery.admin.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function loadResults() {
  resultsLoading.value = true
  resultsError.value = ''
  try {
    const response = await lotteryAdminAPI.listResults(resultsPage.value, resultsPageSize)
    results.value = response.items
    resultsTotal.value = response.total
    resultsPages.value = Math.max(1, response.pages)
  } catch (error) {
    resultsError.value = extractApiErrorMessage(error, t('lottery.admin.resultsLoadFailed'))
  } finally {
    resultsLoading.value = false
  }
}
async function changeResultsPage(page: number) {
  resultsPage.value = page
  await loadResults()
}

onMounted(() => {
  void load()
  void loadResults()
})
</script>
