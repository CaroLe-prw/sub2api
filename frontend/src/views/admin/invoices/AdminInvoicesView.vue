<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="card flex flex-wrap items-center justify-between gap-4 p-5">
        <div><h2 class="font-semibold">{{ t('invoice.manual.management') }}</h2><p class="mt-1 text-sm text-gray-500">{{ t('invoice.manual.adminHint') }}</p></div>
        <div class="flex items-center gap-3">
          <label class="flex items-center gap-2 text-sm"><Toggle v-model="enabled" :disabled="!configLoaded || configSaving" />{{ t('invoice.manual.enabled') }}</label>
          <button class="btn btn-secondary" :disabled="!configLoaded || configSaving" @click="saveConfig">{{ t('common.save') }}</button>
          <a href="https://bill.aitokengw.com" target="_blank" rel="noopener noreferrer" class="btn btn-primary">{{ t('invoice.manual.openPlatform') }}</a>
        </div>
      </div>
      <details class="card p-5" open>
        <summary class="cursor-pointer font-semibold">{{ t('invoice.fees.settings') }}</summary>
        <p class="mt-3 text-sm text-gray-500">{{ t('invoice.fees.rules') }}</p>
        <label class="input-label mt-4 max-w-xs">{{ t('invoice.fees.minimum') }}<input v-model.number="minimum" type="number" min="0" step="0.01" class="input mt-1" @change="feeTiers[0] && (feeTiers[0].min = minimum)" /></label>
        <div class="mt-4 space-y-3">
          <div v-for="(tier, index) in feeTiers" :key="index" class="grid grid-cols-[1fr_1fr_auto] items-end gap-3">
            <label class="input-label">{{ t('invoice.fees.tierStart') }}<input v-model.number="tier.min" type="number" min="0" step="0.01" :readonly="index === 0" class="input mt-1" /></label>
            <label class="input-label">{{ t('invoice.fees.rate') }}<input v-model.number="tier.percent" type="number" min="0" max="100" step="0.01" class="input mt-1" /></label>
            <button class="btn btn-secondary" :disabled="index === 0" @click="feeTiers.splice(index, 1)">{{ t('common.delete') }}</button>
          </div>
        </div>
        <div class="mt-4 flex justify-between gap-3"><button class="btn btn-secondary" @click="feeTiers.push({ min: (feeTiers.at(-1)?.min || minimum) + 100, percent: feeTiers.at(-1)?.percent || 0 })">{{ t('invoice.fees.addTier') }}</button><button class="btn btn-primary" :disabled="!configLoaded || configSaving" @click="saveConfig">{{ t('common.save') }}</button></div>
      </details>
      <div class="card flex flex-wrap items-center gap-3 p-4">
        <SearchInput v-model="search" class="w-64" :placeholder="t('invoice.searchPlaceholder')" @search="changePage(1)" />
        <Select v-model="status" class="w-44" :options="statusOptions" @change="changePage(1)" />
        <button class="btn btn-secondary ml-auto" :disabled="loading" @click="loadList">{{ t('common.refresh') }}</button>
      </div>
      <DataTable :columns="columns" :data="records" :loading="loading" row-key="id">
        <template #cell-company_name="{ row }"><p class="font-medium">{{ row.company_name }}</p><p class="text-xs text-gray-500">{{ row.tax_number }} · #{{ row.user_id }}</p></template>
        <template #cell-amount_cent="{ value }">¥{{ formatInvoiceCent(value) }}</template>
        <template #cell-service_fee_cent="{ row }"><span>¥{{ formatInvoiceCent(row.service_fee_cent || 0) }}</span><p v-if="row.fee_refund_required" class="text-xs text-amber-600">{{ t('invoice.fees.refundPending') }}</p></template>
        <template #cell-status="{ value }"><span :class="['badge', invoiceStatusBadgeClass(value)]">{{ t(`invoice.status.${value}`) }}</span></template>
        <template #cell-created_at="{ value }">{{ formatOrderDateTime(value) }}</template>
        <template #cell-actions="{ row }">
          <div class="flex flex-wrap gap-2">
            <button class="btn btn-secondary btn-sm" @click="detailId = row.id">{{ t('common.view') }}</button>
            <template v-if="row.status === 'PENDING_REVIEW'">
              <button class="btn btn-primary btn-sm" @click="openAction(row, 'approve')">{{ t('admin.invoice.actions.approve') }}</button>
              <button class="btn btn-secondary btn-sm" @click="openAction(row, 'reject')">{{ t('admin.invoice.actions.reject') }}</button>
            </template>
            <button v-if="canUpload(row)" class="btn btn-primary btn-sm" @click="openAction(row, 'upload')">{{ t('invoice.manual.upload') }}</button>
            <button v-if="canCancel(row)" class="btn btn-secondary btn-sm" @click="openAction(row, 'cancel')">{{ t('admin.invoice.actions.cancel') }}</button>
            <button v-if="row.fee_refund_required" class="btn btn-secondary btn-sm" @click="openAction(row, 'refund')">{{ t('invoice.fees.refund') }}</button>
            <button v-if="row.downloadable" class="btn btn-secondary btn-sm" @click="detailId = row.id">{{ t('invoice.actions.download') }}</button>
          </div>
        </template>
      </DataTable>
      <Pagination v-if="total" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:pageSize="changeSize" />
    </div>
    <InvoiceDetailDialog :show="!!detailId" :record-id="detailId" admin @close="detailId = null" />
    <BaseDialog :show="!!target" :title="actionTitle" @close="closeAction">
      <form v-if="target" id="manual-invoice-action" class="space-y-4" @submit.prevent="submitAction">
        <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800"><p class="font-semibold">{{ target.company_name }}</p><p class="font-mono text-sm">{{ target.tax_number }}</p><p class="mt-2 font-semibold">¥{{ formatInvoiceCent(target.amount_cent) }}</p></div>
        <p v-if="action === 'approve'" class="text-sm text-gray-500">{{ t('invoice.manual.approveHint') }}</p>
        <template v-if="action === 'upload'">
          <label class="input-label">{{ t('invoice.manual.invoiceNumber') }}<input v-model="invoiceNumber" class="input mt-1" maxlength="100" /></label>
          <label class="input-label">{{ t('invoice.manual.uploadHint') }}<input data-testid="invoice-file" class="input mt-1" type="file" accept=".pdf,.png,.jpg,.jpeg" required @change="chooseFile" /></label>
          <label v-if="target.requires_manual_check" class="flex items-start gap-2 text-sm text-amber-700"><input v-model="confirmed" type="checkbox" required />{{ t('invoice.manual.externalChecked') }}</label>
        </template>
        <p v-else-if="action === 'refund'" class="text-sm">{{ t('invoice.fees.refundHint') }}</p>
        <label v-else class="input-label">{{ t('invoice.detail.reviewNote') }}<textarea v-model="note" class="input mt-1" rows="3" maxlength="500" :required="action !== 'approve'" /></label>
        <label v-if="action === 'cancel'" class="flex items-start gap-2 text-sm"><input v-model="confirmed" type="checkbox" required />{{ t('invoice.manual.notIssued') }}</label>
      </form>
      <template #footer><div class="flex justify-end gap-3"><button class="btn btn-secondary" :disabled="submitting" @click="closeAction">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="manual-invoice-action" :disabled="submitting">{{ submitting ? t('common.processing') : t('common.confirm') }}</button></div></template>
    </BaseDialog>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import InvoiceDetailDialog from '@/components/invoice/InvoiceDetailDialog.vue'
import { adminInvoiceAPI } from '@/api/admin/invoice'
import type { InvoiceRecord, InvoiceStatus } from '@/types/invoice'
import type { Column } from '@/components/common/types'
import { parseInvoiceUnitPrice } from '@/components/invoice/invoiceForm'
import { formatInvoiceCent, invoiceStatusBadgeClass } from '@/components/invoice/invoiceUtils'
import { formatOrderDateTime } from '@/components/payment/orderUtils'
const { t } = useI18n()
const app = useAppStore()
const route = useRoute()
const enabled = ref(false), configLoaded = ref(false), configSaving = ref(false), loading = ref(false), submitting = ref(false)
const records = ref<InvoiceRecord[]>([])
const minimum = ref(100)
const feeTiers = ref([{ min: 100, percent: 0 }, { min: 200, percent: 6 }, { min: 300, percent: 7 }])
const search = ref(typeof route.query.q === 'string' ? route.query.q : ''), status = ref(''), page = ref(1), pageSize = ref(20), total = ref(0)
const detailId = ref<string | null>(null)
const target = ref<InvoiceRecord | null>(null)
const action = ref<'approve' | 'reject' | 'cancel' | 'upload' | 'refund'>('approve')
const note = ref(''), invoiceNumber = ref(''), confirmed = ref(false), file = ref<File | null>(null)
const activeStatuses: InvoiceStatus[] = ['PENDING_PAYMENT', 'APPROVED', 'SUBMITTING', 'PROCESSING', 'UNKNOWN', 'NEEDS_ATTENTION', 'CANCELLING']
const statuses: InvoiceStatus[] = ['PENDING_REVIEW', ...activeStatuses, 'COMPLETED', 'REJECTED', 'WITHDRAWN', 'CANCELLED', 'FAILED']
const statusOptions = computed(() => [{ value: '', label: t('common.all') }, ...statuses.map(value => ({ value, label: t(`invoice.status.${value}`) }))])
const columns = computed<Column[]>(() => [
  { key: 'company_name', label: t('invoice.detail.companyName') },
  { key: 'amount_cent', label: t('invoice.detail.amount') },
  { key: 'service_fee_cent', label: t('invoice.fees.fee') },
  { key: 'status', label: t('invoice.columns.status') },
  { key: 'invoice_number', label: t('invoice.manual.invoiceNumber') },
  { key: 'created_at', label: t('invoice.detail.createdAt') },
  { key: 'actions', label: t('common.actions') }
])
const actionTitle = computed(() => action.value === 'refund' ? t('invoice.fees.refund') : action.value === 'upload' ? t('invoice.manual.upload') : t(`admin.invoice.actions.${action.value}`))
function canUpload(r: InvoiceRecord) { return !r.downloadable && (r.status === 'APPROVED' || r.requires_manual_check && [...activeStatuses, 'COMPLETED'].includes(r.status)) }
function canCancel(r: InvoiceRecord) { return activeStatuses.includes(r.status) }
function report(err: unknown) { app.showError((err as { message?: string })?.message || t('common.error')) }
let generation = 0
async function loadList() {
  const current = ++generation
  loading.value = true
  try { const { data } = await adminInvoiceAPI.list({ page: page.value, page_size: pageSize.value, status: status.value, q: search.value }); if (current === generation) { records.value = data.items; total.value = data.total } }
  catch (err) { if (current === generation) report(err) } finally { if (current === generation) loading.value = false }
}
function changePage(value: number) { page.value = value; void loadList() }
function changeSize(value: number) { pageSize.value = value; changePage(1) }
function configCent(value: number) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) throw new Error(t('invoice.fees.invalidPricing'))
  try { return value === 0 ? 0 : parseInvoiceUnitPrice(String(value))! } catch { throw new Error(t('invoice.fees.invalidPricing')) }
}
async function saveConfig() {
  configSaving.value = true
  try { enabled.value = (await adminInvoiceAPI.updateConfig({ enabled: enabled.value, min_amount_cent: configCent(minimum.value), fee_tiers: feeTiers.value.map(row => ({ min_amount_cent: configCent(row.min), rate_bps: configCent(row.percent) })) })).data.enabled; app.showSuccess(t('invoice.manual.saved')) }
  catch (err) { report(err) } finally { configSaving.value = false }
}
function openAction(record: InvoiceRecord, value: typeof action.value) { target.value = record; action.value = value; note.value = ''; invoiceNumber.value = ''; confirmed.value = false; file.value = null }
function closeAction() { if (!submitting.value) target.value = null }
function chooseFile(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0] || null
  if (file.value && file.value.size > 20 * 1024 * 1024) { app.showError(t('invoice.manual.fileTooLarge')); file.value = null; (event.target as HTMLInputElement).value = '' }
}
async function submitAction() {
  if (!target.value || submitting.value) return
  if (action.value === 'upload' && (!file.value || target.value.requires_manual_check && !confirmed.value)) return
  if (action.value === 'cancel' && (!confirmed.value || !note.value.trim())) return
  if (action.value === 'reject' && !note.value.trim()) return
  submitting.value = true
  try {
    if (action.value === 'refund') await adminInvoiceAPI.refundFee(target.value.id)
    else if (action.value === 'upload') await adminInvoiceAPI.upload(target.value.id, file.value!, invoiceNumber.value, confirmed.value)
    else await adminInvoiceAPI.action(target.value.id, { action: action.value, note: note.value, confirm_not_issued: confirmed.value })
    app.showSuccess(t('invoice.manual.saved')); target.value = null; await loadList()
  } catch (err) { report(err) } finally { submitting.value = false }
}
watch(() => route.query.q, q => { search.value = typeof q === 'string' ? q : ''; changePage(1) })
onMounted(async () => {
  void loadList()
  try { const { data } = await adminInvoiceAPI.getConfig(); enabled.value = data.enabled; minimum.value = (data.min_amount_cent ?? 10000) / 100; if (data.fee_tiers?.length) feeTiers.value = data.fee_tiers.map(row => ({ min: row.min_amount_cent / 100, percent: row.rate_bps / 100 })); configLoaded.value = true } catch (err) { report(err) }
})
</script>
