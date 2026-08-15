<template>
  <div class="card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('checkIn.admin.recordsTitle') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('checkIn.admin.recordsDescription') }}</p>
    </div>

    <div v-if="loading" class="flex min-h-[160px] items-center justify-center"><LoadingSpinner /></div>
    <div v-else class="p-6">
      <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
      <div v-else-if="records.length" class="overflow-x-auto rounded-xl border border-gray-100 dark:border-dark-700">
        <table class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700">
          <thead class="bg-gray-50 text-left text-xs font-semibold text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3">{{ t('checkIn.admin.user') }}</th>
              <th class="px-4 py-3">{{ t('checkIn.admin.date') }}</th>
              <th class="px-4 py-3">{{ t('checkIn.admin.reward') }}</th>
              <th class="px-4 py-3">{{ t('checkIn.admin.time') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
            <tr v-for="record in records" :key="record.id">
              <td class="px-4 py-3">
                <p class="font-medium text-gray-900 dark:text-white">{{ record.username || `#${record.user_id}` }}</p>
                <p class="mt-0.5 text-xs text-gray-500">{{ record.email }}</p>
              </td>
              <td class="whitespace-nowrap px-4 py-3 text-gray-700 dark:text-gray-300">{{ record.date }}</td>
              <td class="whitespace-nowrap px-4 py-3 font-semibold text-emerald-600 dark:text-emerald-400">+{{ formatReward(record.reward) }}</td>
              <td class="whitespace-nowrap px-4 py-3 text-gray-500 dark:text-gray-400">{{ formatDate(record.created_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-else class="text-sm text-gray-500 dark:text-gray-400">{{ t('checkIn.admin.noRecords') }}</p>

      <div v-if="!errorMessage && total > 0" class="mt-4 flex flex-wrap items-center justify-between gap-3">
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkIn.admin.pageSummary', { page, pages, total }) }}</p>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1" @click="changePage(page - 1)">{{ t('checkIn.admin.previous') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="page >= pages" @click="changePage(page + 1)">{{ t('checkIn.admin.next') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import checkInAdminAPI, { type CheckInAdminRecord } from '@/api/admin/checkIn'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t, locale } = useI18n()
const records = ref<CheckInAdminRecord[]>([])
const loading = ref(true)
const errorMessage = ref('')
const page = ref(1)
const pages = ref(1)
const total = ref(0)
const pageSize = 20

function formatReward(value: number): string {
  return Number(value || 0).toFixed(8).replace(/0+$/, '').replace(/\.$/, '') || '0'
}
function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
async function loadRecords() {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await checkInAdminAPI.listRecords(page.value, pageSize)
    records.value = response.items
    total.value = response.total
    pages.value = Math.max(1, response.pages)
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, t('checkIn.admin.loadFailed'))
  } finally {
    loading.value = false
  }
}
async function changePage(nextPage: number) {
  page.value = nextPage
  await loadRecords()
}

onMounted(loadRecords)
</script>
