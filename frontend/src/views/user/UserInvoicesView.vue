<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <Select
            v-model="statusFilter"
            :options="statusFilterOptions"
            class="w-44"
            @change="handlePageChange(1)"
          />
          <SearchInput
            v-model="searchQuery"
            :placeholder="t('invoice.searchPlaceholder')"
            class="w-full sm:w-64"
            @search="handleSearch"
          />
        </div>
      </template>

      <template #actions>
        <div class="flex justify-end gap-3">
          <button
            @click="fetchInvoices"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button
            class="btn btn-primary"
            :disabled="!configLoaded || !invoiceEnabled"
            :title="configLoaded && !invoiceEnabled ? t('invoice.empty.notEnabled') : undefined"
            @click="showApplyDialog = true"
          >
            <Icon name="plus" size="md" class="mr-2" />
            {{ t('invoice.actions.apply') }}
          </button>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="records" :loading="loading" row-key="id">
          <template #cell-status="{ value }">
            <span class="badge" :class="invoiceStatusBadgeClass(value)">
              {{ t(`invoice.status.${value}`) }}
            </span>
          </template>
          <template #cell-company_name="{ value }">
            <span class="text-sm text-gray-900 dark:text-white">{{ value }}</span>
          </template>
          <template #cell-amount_cent="{ value }">
            <span class="text-sm font-medium text-gray-900 dark:text-white">
              ¥{{ formatInvoiceCent(value) }}
            </span>
          </template>
          <template #cell-created_at="{ value }">
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ formatOrderDateTime(value) }}</span>
          </template>
          <template #cell-service_fee_cent="{ value }">¥{{ formatInvoiceCent(value || 0) }}</template>
          <template #cell-actions="{ row }">
            <div class="flex items-center gap-2">
              <button v-if="row.status === 'PENDING_PAYMENT'" class="btn btn-primary btn-sm" @click="paymentTarget = row">{{ t('invoice.fees.payTitle') }}</button>
              <button
                @click="openDetail(row)"
                class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-primary-600 hover:bg-primary-50 dark:text-primary-400 dark:hover:bg-primary-900/20"
              >
                <Icon name="eye" size="sm" />
                <span>{{ t('invoice.actions.detail') }}</span>
              </button>
              <button
                v-if="row.downloadable"
                @click="handleDownload(row)"
                :disabled="downloadingId === row.id"
                class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-green-600 hover:bg-green-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-green-400 dark:hover:bg-green-900/20"
              >
                <Icon name="download" size="sm" />
                <span>{{ t('invoice.actions.download') }}</span>
              </button>
              <button
                v-if="['PENDING_REVIEW', 'PENDING_PAYMENT'].includes(row.status)"
                @click="withdrawTarget = row"
                class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-yellow-600 hover:bg-yellow-50 dark:text-yellow-400 dark:hover:bg-yellow-900/20"
              >
                <Icon name="x" size="sm" />
                <span>{{ t('invoice.actions.withdraw') }}</span>
              </button>
            </div>
          </template>
          <template #empty>
            <div class="flex flex-col items-center">
              <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
              <p class="text-lg font-medium text-gray-900 dark:text-gray-100">
                {{ invoiceDisabled ? t('invoice.empty.notEnabled') : t('invoice.empty.noRecords') }}
              </p>
              <p v-if="invoiceDisabled" class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('invoice.empty.notEnabledHint') }}
              </p>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <InvoiceFeePaymentDialog :show="!!paymentTarget" :record="paymentTarget" @close="paymentTarget = null" @paid="fetchInvoices" />
    <!-- Apply Invoice Dialog -->
    <InvoiceApplyDialog
      :show="showApplyDialog"
      @close="showApplyDialog = false"
      @created="handleCreated"
    />

    <!-- Invoice Detail Dialog -->
    <InvoiceDetailDialog
      :show="!!detailRecordId"
      :record-id="detailRecordId"
      @close="detailRecordId = null"
      @updated="fetchInvoices"
    />

    <!-- Withdraw Confirm Dialog -->
    <BaseDialog
      :show="!!withdrawTarget"
      :title="t('invoice.actions.withdraw')"
      width="narrow"
      @close="withdrawTarget = null"
    >
      <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('invoice.confirmWithdraw') }}</p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button class="btn btn-secondary" @click="withdrawTarget = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-danger" :disabled="actionLoading" @click="confirmWithdraw">
            {{ actionLoading ? t('common.processing') : t('common.confirm') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import { invoiceAPI } from '@/api/invoice'
import type { InvoiceRecord, InvoiceStatus } from '@/types/invoice'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import InvoiceFeePaymentDialog from '@/components/invoice/InvoiceFeePaymentDialog.vue'
import InvoiceApplyDialog from '@/components/invoice/InvoiceApplyDialog.vue'
import InvoiceDetailDialog from '@/components/invoice/InvoiceDetailDialog.vue'
import type { Column } from '@/components/common/types'
import { invoiceStatusBadgeClass, formatInvoiceCent } from '@/components/invoice/invoiceUtils'
import { formatOrderDateTime } from '@/components/payment/orderUtils'

const { t } = useI18n()
const appStore = useAppStore()

const INVOICE_STATUSES: InvoiceStatus[] = [
  'PENDING_PAYMENT',
  'PENDING_REVIEW',
  'APPROVED',
  'SUBMITTING',
  'PROCESSING',
  'UNKNOWN',
  'NEEDS_ATTENTION',
  'CANCELLING',
  'COMPLETED',
  'CANCELLED',
  'FAILED',
  'REJECTED',
  'WITHDRAWN',
]

const loading = ref(false)
const actionLoading = ref(false)
const records = ref<InvoiceRecord[]>([])
const invoiceEnabled = ref(false)
const configLoaded = ref(false)
const statusFilter = ref('')
const searchQuery = ref('')
const showApplyDialog = ref(false)
const paymentTarget = ref<InvoiceRecord | null>(null)
const detailRecordId = ref<string | null>(null)
const withdrawTarget = ref<InvoiceRecord | null>(null)
const downloadingId = ref<string | null>(null)
const pagination = reactive({ page: 1, page_size: 20, total: 0 })

const statusFilterOptions = computed(() => [
  { value: '', label: t('common.all') },
  ...INVOICE_STATUSES.map((status) => ({ value: status, label: t(`invoice.status.${status}`) })),
])

const columns = computed((): Column[] => [
  { key: 'status', label: t('invoice.columns.status') },
  { key: 'company_name', label: t('invoice.columns.companyName') },
  { key: 'amount_cent', label: t('invoice.columns.amount') },
  { key: 'service_fee_cent', label: t('invoice.fees.fee') },
  { key: 'created_at', label: t('invoice.columns.createdAt') },
  { key: 'actions', label: t('common.actions') },
])

const invoiceDisabled = computed(
  () => configLoaded.value && !invoiceEnabled.value && records.value.length === 0
)

function errorMessage(err: unknown): string {
  if (err && typeof err === 'object' && 'message' in err) {
    const msg = (err as { message?: unknown }).message
    if (typeof msg === 'string' && msg) return msg
  }
  return t('common.error')
}

async function fetchInvoices() {
  loading.value = true
  try {
    const res = await invoiceAPI.list({
      page: pagination.page,
      page_size: pagination.page_size,
      status: statusFilter.value || undefined,
      q: searchQuery.value.trim() || undefined,
    })
    records.value = res.data.items || []
    pagination.total = res.data.total || 0
  } catch (err: unknown) {
    appStore.showError(errorMessage(err))
  } finally {
    loading.value = false
  }
}

async function loadOptions() {
  try { invoiceEnabled.value = (await invoiceAPI.getConfig()).data.enabled }
  catch { invoiceEnabled.value = false }
  finally { configLoaded.value = true }
}

function handlePageChange(page: number) {
  pagination.page = page
  fetchInvoices()
}

function handlePageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  fetchInvoices()
}

function handleSearch() {
  pagination.page = 1
  fetchInvoices()
}

function openDetail(record: InvoiceRecord) {
  detailRecordId.value = record.id
}

async function handleDownload(record: InvoiceRecord) {
  downloadingId.value = record.id
  try {
    const { blob, filename } = await invoiceAPI.download(
      record.id,
      `invoice-${record.application_no || record.id}`
    )
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = filename
    link.click()
    window.URL.revokeObjectURL(url)
  } catch (err: unknown) {
    appStore.showError(errorMessage(err))
  } finally {
    downloadingId.value = null
  }
}

async function confirmWithdraw() {
  if (!withdrawTarget.value) return
  actionLoading.value = true
  try {
    await invoiceAPI.withdraw(withdrawTarget.value.id)
    appStore.showSuccess(t('invoice.withdrawSuccess'))
    withdrawTarget.value = null
    await fetchInvoices()
  } catch (err: unknown) {
    appStore.showError(errorMessage(err))
  } finally {
    actionLoading.value = false
  }
}

function handleCreated(record: InvoiceRecord) {
  if (record.status === 'PENDING_PAYMENT') paymentTarget.value = record
  pagination.page = 1
  fetchInvoices()
}

onMounted(() => {
  fetchInvoices()
  loadOptions()
})
</script>
