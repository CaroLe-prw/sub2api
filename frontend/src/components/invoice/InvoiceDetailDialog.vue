<template>
  <BaseDialog :show="show" :title="t('invoice.detail.title')" width="wide" @close="emit('close')">
    <p v-if="loading" class="py-8 text-center">{{ t('common.loading') }}</p>
    <div v-else-if="detail" class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span :class="['badge', invoiceStatusBadgeClass(detail.status)]">{{ t(`invoice.status.${detail.status}`) }}</span>
        <button v-if="admin" class="btn btn-secondary" @click="copyDetails">{{ t('invoice.manual.copyDetails') }}</button>
      </div>
      <p v-if="admin && detail.requires_manual_check" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20">{{ t('invoice.manual.legacyHint') }}</p>
      <dl class="grid gap-4 sm:grid-cols-2">
        <div v-for="field in fields" :key="field.key">
          <dt class="text-xs text-gray-500">{{ t(field.label) }}</dt>
          <dd class="mt-1 break-all whitespace-pre-wrap text-sm">{{ field.value || '-' }}</dd>
        </div>
      </dl>
      <div>
        <h3 class="mb-2 text-sm font-semibold">{{ t('invoice.detail.orders') }}</h3>
        <div v-for="order in detail.orders" :key="order.id" class="flex justify-between gap-3 border-t border-gray-100 py-2 text-sm dark:border-dark-700">
          <span class="break-all font-mono">{{ order.out_trade_no || `#${order.id}` }}</span><span>¥{{ formatInvoiceCent(order.amount_cent) }}</span>
        </div>
      </div>
      <div v-if="detail.events.length">
        <h3 class="mb-2 text-sm font-semibold">{{ t('invoice.detail.events') }}</h3>
        <div class="max-h-48 space-y-2 overflow-auto text-xs text-gray-500">
          <p v-for="(event, index) in detail.events" :key="index">{{ formatOrderDateTime(event.created_at) }} · {{ eventLabel(event.action) }} <span v-if="event.message">— {{ event.message }}</span></p>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
        <button v-if="detail?.downloadable" class="btn btn-primary" :disabled="downloading" @click="download">{{ t('invoice.actions.download') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { invoiceAPI } from '@/api/invoice'
import { adminInvoiceAPI } from '@/api/admin/invoice'
import type { InvoiceDetail } from '@/types/invoice'
import { formatInvoiceCent, invoiceStatusBadgeClass } from './invoiceUtils'
import { formatOrderDateTime } from '@/components/payment/orderUtils'
const props = withDefaults(defineProps<{ show: boolean; recordId: string | null; admin?: boolean }>(), { admin: false })
const emit = defineEmits<{ (e: 'close'): void; (e: 'updated'): void }>()
const { t, te } = useI18n()
const app = useAppStore()
const detail = ref<InvoiceDetail | null>(null)
const loading = ref(false)
const downloading = ref(false)
let generation = 0
const fields = computed(() => {
  const d = detail.value
  if (!d) return []
  return [
    { key: 'id', label: 'invoice.detail.applicationNo', value: d.id },
    { key: 'amount', label: 'invoice.detail.amount', value: `¥${formatInvoiceCent(d.amount_cent)}` },
    { key: 'type', label: 'invoice.apply.invoiceType', value: d.invoice_type === 'NORMAL' ? t('invoice.form.normalLabel') : '' },
    { key: 'category', label: 'invoice.apply.category', value: d.invoice_category },
    { key: 'quantity', label: 'invoice.form.quantity', value: d.quantity },
    { key: 'unitPrice', label: 'invoice.form.unitPrice', value: d.unit_price_cent ? `¥${formatInvoiceCent(d.unit_price_cent)}` : '' },
    ...(['companyName', 'taxNumber', 'deliveryEmail', 'invoiceItem', 'specification', 'unit', 'remark', 'reviewNote'] as const).map((label, i) => ({
      key: label, label: `invoice.detail.${label}`, value: [d.company_name, d.tax_number, d.delivery_email, d.invoice_item, d.specification, d.unit, d.remark, d.review_note][i]
    })),
    { key: 'number', label: 'invoice.manual.invoiceNumber', value: d.invoice_number },
    { key: 'created', label: 'invoice.detail.createdAt', value: formatOrderDateTime(d.created_at) }
  ]
})
function eventLabel(action: string) { const key = `invoice.manual.events.${action.toLowerCase()}`; return te(key) ? t(key) : t('invoice.manual.events.update') }
function report(err: unknown) { app.showError((err as { message?: string })?.message || t('common.error')) }
async function copyDetails() {
  try { await navigator.clipboard.writeText(fields.value.map(f => `${t(f.label)}：${f.value || '-'}`).join('\n')); app.showSuccess(t('invoice.manual.copied')) } catch (err) { report(err) }
}
async function download() {
  if (!detail.value || downloading.value) return
  downloading.value = true
  try {
    const { blob, filename } = await (props.admin ? adminInvoiceAPI : invoiceAPI).download(detail.value.id)
    const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = url; a.download = filename; a.click(); URL.revokeObjectURL(url)
  } catch (err) { report(err) } finally { downloading.value = false }
}
watch(() => [props.show, props.recordId] as const, async ([show, id]) => {
  const current = ++generation
  detail.value = null
  if (!show || !id) return
  loading.value = true
  try { const { data } = await (props.admin ? adminInvoiceAPI : invoiceAPI).detail(id); if (current === generation) detail.value = data }
  catch (err) { if (current === generation) report(err) }
  finally { if (current === generation) loading.value = false }
}, { immediate: true })
</script>
