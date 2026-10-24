<template>
  <BaseDialog :show="show" :title="t('invoice.apply.title')" width="wide" :close-on-escape="!submitting" @close="closeApplication">
    <template #header="{ titleId }">
      <div class="flex items-center gap-4 py-2">
        <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl bg-primary-50 text-primary-600 dark:bg-primary-900/20"><Icon name="document" size="xl" /></div>
        <div><p class="mb-1 text-xs font-semibold tracking-widest text-primary-600">{{ t('invoice.form.newApplication') }}</p><h3 :id="titleId" class="text-2xl font-bold text-gray-900 dark:text-white">{{ step === 3 ? t('invoice.apply.review') : t('invoice.apply.title') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('invoice.form.subtitle') }}</p></div>
      </div>
    </template>

    <div v-if="step === 1" class="space-y-3 py-2">
      <p class="text-sm text-gray-500">{{ t('invoice.manual.applicationHint') }}</p>
      <p v-if="ordersLoading && !orders.length">{{ t('common.loading') }}</p>
      <p v-else-if="!orders.length" class="py-6 text-center text-gray-500">{{ t('invoice.apply.noEligibleOrders') }}</p>
      <div class="max-h-80 space-y-2 overflow-y-auto">
        <label v-for="order in orders" :key="order.id" class="flex items-center gap-3 rounded-xl border border-gray-200 p-3 dark:border-dark-600">
          <input type="checkbox" :checked="selectedOrderIds.includes(order.id)" :disabled="selectedOrderIds.length >= 100 && !selectedOrderIds.includes(order.id)" @change="toggleOrder(order.id)" />
          <div class="min-w-0 flex-1"><p class="break-all font-mono text-sm">{{ order.out_trade_no || `#${order.id}` }}</p><p class="text-xs text-gray-500">{{ formatOrderDateTime(order.created_at) }}</p><p v-if="order.payment_fee_cent" class="text-xs text-amber-600">{{ t('invoice.apply.feeNotInvoiceable', { amount: `¥${formatInvoiceCent(order.payment_fee_cent)}` }) }}</p></div>
          <strong class="text-sm">¥{{ formatInvoiceCent(order.amount_cent) }}</strong>
        </label>
      </div>
      <button v-if="hasMoreOrders" data-testid="invoice-load-more" type="button" class="btn btn-secondary" :disabled="ordersLoading" @click="loadOrders(true)">{{ t('invoice.apply.loadMoreOrders') }}</button>
      <p class="text-xs text-gray-500">{{ t('invoice.apply.orderLimit') }}</p><p v-if="pricing" class="text-sm text-primary-600">{{ t('invoice.fees.minimumHint', { amount: formatInvoiceCent(pricing.min_amount_cent) }) }}</p>
    </div>

    <form v-else-if="step === 2" id="invoice-apply-form" class="space-y-6 py-3 sm:space-y-7" @submit.prevent="reviewApplication">
      <div class="rounded-2xl border border-primary-200 bg-gradient-to-r from-primary-50 to-sky-50/50 p-4 dark:border-primary-800 dark:from-primary-950/40 dark:to-dark-800 sm:p-5">
        <button type="button" class="flex w-full items-center gap-4 text-left" :aria-expanded="pasteOpen" @click="pasteOpen = !pasteOpen">
          <span class="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-white text-primary-600 shadow-sm dark:bg-dark-700"><Icon name="document" size="lg" /></span>
          <span class="flex-1"><span class="text-lg font-semibold text-gray-800 dark:text-gray-100">{{ t('invoice.form.pasteTitle') }}</span><span class="ml-2 rounded-full bg-primary-100 px-2 py-0.5 text-xs text-primary-700 dark:bg-primary-900 dark:text-primary-200">{{ t('invoice.form.optionalTool') }}</span><span class="mt-1 block text-sm text-gray-500">{{ t('invoice.form.pasteHint') }}</span></span>
          <span class="shrink-0 rounded-full border border-primary-200 bg-white px-3 py-1.5 text-sm font-semibold text-primary-600 dark:bg-dark-800">{{ pasteOpen ? t('invoice.form.collapse') : t('invoice.form.tryPaste') }}</span>
        </button>
        <div v-if="pasteOpen" class="mt-4 space-y-2"><textarea v-model="pasteText" class="input min-h-24" :placeholder="t('invoice.form.pastePlaceholder')" /><button class="btn btn-secondary" type="button" @click="applyPaste">{{ t('invoice.form.fillFields') }}</button></div>
      </div>
      <div v-if="titles.length"><label class="input-label" for="invoice-saved-title">{{ t('invoice.apply.savedTitle') }}</label><select id="invoice-saved-title" class="input" @change="applyTitle(($event.target as HTMLSelectElement).value)"><option value="">{{ t('invoice.apply.newTitle') }}</option><option v-for="title in titles" :key="title.id" :value="title.id">{{ title.company_name }}</option></select></div>
      <div class="grid gap-x-6 gap-y-6 sm:grid-cols-2 sm:gap-y-7">
        <div><label class="invoice-form-label" for="invoice-buyer">{{ t('invoice.apply.companyName') }}</label><input id="invoice-buyer" v-model="form.company_name" class="input invoice-form-input" minlength="2" maxlength="200" :placeholder="t('invoice.form.buyerPlaceholder')" required /></div>
        <div><label class="invoice-form-label" for="invoice-tax">{{ t('invoice.apply.taxNumber') }} <span class="invoice-optional">{{ t('invoice.apply.optional') }}</span></label><input id="invoice-tax" v-model="form.tax_number" class="input invoice-form-input uppercase" maxlength="20" :placeholder="t('invoice.apply.taxNumberPlaceholder')" /><p class="invoice-form-hint">{{ t('invoice.form.taxHint') }}</p></div>
        <div><label class="invoice-form-label" for="invoice-amount">{{ t('invoice.apply.amount') }}</label><input id="invoice-amount" class="input invoice-form-input bg-gray-50 dark:bg-dark-900" :value="formatInvoiceCent(totalAmountCent)" readonly /><p class="invoice-form-hint">{{ t('invoice.form.amountHint') }}</p></div>
        <div><label class="invoice-form-label" for="invoice-type">{{ t('invoice.apply.invoiceType') }}</label><select id="invoice-type" v-model="form.invoice_type" class="input invoice-form-input"><optgroup :label="t('invoice.form.normalInvoice')"><option value="NORMAL">{{ t('invoice.form.normalLabel') }}</option></optgroup></select><p class="invoice-form-hint text-primary-600">{{ feeEstimate === null ? t('invoice.fees.minimumHint', { amount: formatInvoiceCent(pricing?.min_amount_cent) }) : t('invoice.fees.estimated', { amount: formatInvoiceCent(feeEstimate) }) }}</p></div>
        <div><label class="invoice-form-label" for="invoice-category">{{ t('invoice.apply.category') }}</label><select id="invoice-category" v-model="form.invoice_category" class="input invoice-form-input"><option :value="MANUAL_INVOICE_CATEGORY">{{ t('invoice.form.categoryLabel') }}</option></select></div>
        <div><label class="invoice-form-label" for="invoice-item">{{ t('invoice.apply.invoiceItem') }}</label><select id="invoice-item" v-model="form.invoice_item" class="input invoice-form-input"><option v-for="(item, index) in MANUAL_INVOICE_ITEMS" :key="item" :value="item">{{ t(`invoice.form.items.${index}`) }}</option></select></div>
        <div><label class="invoice-form-label" for="invoice-email">{{ t('invoice.apply.deliveryEmail') }} <span class="invoice-optional">{{ t('invoice.apply.optional') }}</span></label><input id="invoice-email" v-model="form.delivery_email" class="input invoice-form-input" type="email" maxlength="254" :placeholder="t('invoice.apply.deliveryEmailPlaceholder')" /><p class="invoice-form-hint">{{ t('invoice.form.emailHint') }}</p></div>
        <div><label class="invoice-form-label">{{ t('invoice.apply.details') }} <span class="invoice-optional">{{ t('invoice.apply.optional') }}</span></label><button data-testid="invoice-details-open" type="button" class="input invoice-form-input flex items-center justify-between gap-3 text-left" @click="detailsOpen = true"><span class="truncate text-gray-400">{{ detailsSummary || t('invoice.apply.detailsPlaceholder') }}</span><span class="shrink-0 font-semibold text-primary-600">{{ t('invoice.apply.detailsAdd') }} ›</span></button><p class="invoice-form-hint">{{ t('invoice.form.detailsHint') }}</p></div>
      </div>
      <label class="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-300"><input v-model="form.save_title" type="checkbox" />{{ t('invoice.apply.saveTitle') }}</label>
    </form>

    <div v-else class="space-y-5 py-3">
      <div class="rounded-xl bg-primary-50 p-4 text-sm text-primary-800 dark:bg-primary-900/30 dark:text-primary-200">{{ t('invoice.fees.reviewHint') }}<p class="mt-2">{{ t('invoice.fees.appliedRate', { rate: (feeQuote?.rate_bps || 0) / 100 }) }}</p><p class="mt-2 font-bold">{{ t('invoice.fees.amountDue') }}：¥{{ formatInvoiceCent(feeQuote?.fee_cent) }}</p></div>
      <dl class="grid gap-5 sm:grid-cols-2"><div v-for="field in reviewFields" :key="field.label"><dt class="text-sm text-gray-500">{{ t(field.label) }}</dt><dd class="mt-1 whitespace-pre-wrap break-all font-medium">{{ field.value || t('invoice.confirm.notProvided') }}</dd></div></dl>
      <label class="flex items-start gap-2 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900"><input v-model="confirmed" class="mt-0.5" type="checkbox" data-testid="invoice-confirmed" />{{ t('invoice.form.confirmCorrect') }}</label>
    </div>
    <div class="mt-4 flex justify-between rounded-xl bg-gray-50 p-3 text-sm dark:bg-dark-900"><span>{{ t('invoice.apply.selectedCount', { count: selectedOrderIds.length }) }}</span><strong>{{ t('invoice.apply.totalAmount') }}：¥{{ formatInvoiceCent(step === 3 && feeQuote ? feeQuote.amount_cent : totalAmountCent) }}</strong></div>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" :disabled="submitting || quoting" @click="goBack">{{ step === 3 ? t('invoice.confirm.back') : step === 2 ? t('invoice.manual.back') : t('common.cancel') }}</button>
        <button v-if="step === 1" data-testid="invoice-next" class="btn btn-primary" :disabled="!selectedOrderIds.length || !!pricing && totalAmountCent < pricing.min_amount_cent" @click="step = 2">{{ t('invoice.manual.fillDetails') }}</button>
        <button v-else-if="step === 2" class="btn btn-primary" type="submit" form="invoice-apply-form" data-testid="invoice-review" :disabled="quoting">{{ t('invoice.apply.review') }}</button>
        <button v-else class="btn btn-primary" :disabled="submitting || !confirmed" data-testid="invoice-submit" @click="handleSubmit">{{ submitting ? t('common.processing') : feeQuote && feeQuote.fee_cent > 0 ? t('invoice.fees.submitPay') : t('invoice.confirm.submit') }}</button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog :show="show && detailsOpen" :title="t('invoice.apply.details')" placement="right" :z-index="60" @close="detailsOpen = false">
    <template #header="{ titleId }"><div class="py-2"><p class="mb-1 text-xs font-semibold tracking-widest text-primary-600">{{ t('invoice.form.detailsEyebrow') }}</p><h3 :id="titleId" class="text-xl font-bold">{{ t('invoice.apply.details') }}</h3><p class="mt-2 max-w-md text-xs leading-relaxed text-amber-600">{{ t('invoice.apply.detailsNotice') }}</p></div></template>
    <form id="invoice-details-form" class="space-y-5 py-3" @submit.prevent="finishDetails">
      <div class="grid grid-cols-2 gap-4">
        <div><label class="invoice-form-label" for="invoice-specification">{{ t('invoice.apply.specification') }}</label><input id="invoice-specification" v-model="form.specification" class="input invoice-form-input" maxlength="200" :placeholder="t('invoice.apply.optional')" /></div>
        <div><label class="invoice-form-label" for="invoice-unit">{{ t('invoice.apply.unit') }}</label><input id="invoice-unit" v-model="form.unit" class="input invoice-form-input" maxlength="32" :placeholder="t('invoice.apply.optional')" /></div>
        <div><label class="invoice-form-label" for="invoice-quantity">{{ t('invoice.form.quantity') }}</label><input id="invoice-quantity" v-model="form.quantity" class="input invoice-form-input" inputmode="decimal" :placeholder="t('invoice.apply.optional')" /></div>
        <div><label class="invoice-form-label" for="invoice-unit-price">{{ t('invoice.form.unitPrice') }}</label><input id="invoice-unit-price" v-model="unitPrice" class="input invoice-form-input" inputmode="decimal" :placeholder="t('invoice.apply.optional')" /></div>
      </div>
      <div><label class="invoice-form-label" for="invoice-remark">{{ t('invoice.apply.remark') }}</label><textarea id="invoice-remark" v-model="form.remark" class="input min-h-28" maxlength="300" :placeholder="t('invoice.apply.remarkPlaceholder')" /><p class="invoice-form-hint">{{ t('invoice.form.remarkHint') }}</p></div>
    </form>
    <template #footer><button class="btn btn-primary" type="submit" form="invoice-details-form">{{ t('invoice.apply.done') }}</button></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { invoiceAPI } from '@/api/invoice'
import type { InvoiceOrder, InvoiceTitle, InvoiceRecord, InvoiceConfig, InvoiceFeeQuote } from '@/types/invoice'
import { formatInvoiceCent } from './invoiceUtils'
import { MANUAL_INVOICE_ITEMS, MANUAL_INVOICE_CATEGORY, parseInvoicePaste, parseInvoiceUnitPrice } from './invoiceForm'
import { formatOrderDateTime } from '@/components/payment/orderUtils'
const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'created', record: InvoiceRecord): void }>()
const { t } = useI18n()
const appStore = useAppStore()
const step = ref(1), detailsOpen = ref(false), pasteOpen = ref(false), pasteText = ref(''), confirmed = ref(false), unitPrice = ref('')
const orders = ref<InvoiceOrder[]>([]), selectedOrderIds = ref<number[]>([]), titles = ref<InvoiceTitle[]>([])
const ordersLoading = ref(false), hasMoreOrders = ref(false), nextOrderCursor = ref(0), submitting = ref(false)
let generation = 0
const emptyForm = () => ({ company_name: '', tax_number: '', delivery_email: '', invoice_type: 'NORMAL' as const, invoice_category: MANUAL_INVOICE_CATEGORY, invoice_item: MANUAL_INVOICE_ITEMS[0] as string, quantity: '', specification: '', unit: '', remark: '', save_title: false })
const form = reactive(emptyForm())
const pricing = ref<InvoiceConfig | null>(null), feeQuote = ref<InvoiceFeeQuote | null>(null), quoting = ref(false)
const feeEstimate = computed(() => {
  if (!pricing.value?.fee_tiers?.length || totalAmountCent.value < pricing.value.min_amount_cent) return null
  let rate = 0
  for (const tier of pricing.value.fee_tiers) if (totalAmountCent.value >= tier.min_amount_cent) rate = tier.rate_bps
  return Number((BigInt(totalAmountCent.value) * BigInt(rate) + 9999n) / 10000n)
})
const totalAmountCent = computed(() => orders.value.filter(o => selectedOrderIds.value.includes(o.id)).reduce((sum, o) => sum + o.amount_cent, 0))
const detailsSummary = computed(() => [form.specification, form.unit, form.quantity, unitPrice.value ? `¥${unitPrice.value}` : '', form.remark].filter(Boolean).join(' / '))
const reviewFields = computed(() => [
  { label: 'invoice.apply.companyName', value: form.company_name }, { label: 'invoice.apply.taxNumber', value: form.tax_number },
  { label: 'invoice.apply.amount', value: `¥${formatInvoiceCent(feeQuote.value?.amount_cent ?? totalAmountCent.value)}` }, { label: 'invoice.apply.invoiceType', value: t('invoice.form.normalLabel') },
  { label: 'invoice.apply.category', value: t('invoice.form.categoryLabel') }, { label: 'invoice.apply.invoiceItem', value: form.invoice_item },
  { label: 'invoice.apply.deliveryEmail', value: form.delivery_email }, { label: 'invoice.apply.specification', value: form.specification },
  { label: 'invoice.apply.unit', value: form.unit }, { label: 'invoice.form.quantity', value: form.quantity },
  { label: 'invoice.form.unitPrice', value: unitPrice.value ? `¥${unitPrice.value}` : '' }, { label: 'invoice.apply.remark', value: form.remark }
])
function toggleOrder(id: number) { const i = selectedOrderIds.value.indexOf(id); if (i >= 0) selectedOrderIds.value.splice(i, 1); else if (selectedOrderIds.value.length < 100) selectedOrderIds.value.push(id) }
function applyTitle(id: string) { const title = titles.value.find(t => t.id === Number(id)); if (title) Object.assign(form, { company_name: title.company_name, tax_number: title.tax_number, delivery_email: title.delivery_email }) }
function report(err: unknown) { appStore.showError((err as { message?: string })?.message || t('common.error')) }
function applyPaste() {
  const parsed = parseInvoicePaste(pasteText.value)
  if (!Object.keys(parsed).length) { appStore.showError(t('invoice.form.pasteEmpty')); return }
  Object.assign(form, parsed)
  appStore.showSuccess(t('invoice.form.pasteDone'))
}
function validateDetails(): boolean {
  if (form.quantity && (!/^\d{1,12}(\.\d{1,6})?$/.test(form.quantity.trim()) || Number(form.quantity) <= 0)) { appStore.showError(t('invoice.form.quantityError')); return false }
  try { parseInvoiceUnitPrice(unitPrice.value) } catch { appStore.showError(t('invoice.form.priceError')); return false }
  return true
}
function finishDetails() { if (validateDetails()) detailsOpen.value = false }
async function reviewApplication() {
 if (quoting.value) return
  form.company_name = form.company_name.trim(); form.tax_number = form.tax_number.trim().toUpperCase()
  if (form.company_name.length < 2 || form.company_name.length > 200) { appStore.showError(t('invoice.apply.errCompanyName')); return }
  if (form.tax_number && !/^[A-Z0-9]{15,20}$/.test(form.tax_number)) { appStore.showError(t('invoice.apply.errTaxNumber')); return }
  if (!validateDetails()) { detailsOpen.value = true; return }
  quoting.value = true
  const current = generation
  try { const { data } = await invoiceAPI.quote([...selectedOrderIds.value]); if (current !== generation) return; feeQuote.value = data; confirmed.value = false; step.value = 3 }
  catch (err) { if (current === generation) report(err) } finally { if (current === generation) quoting.value = false }
}
function closeApplication() { if (!submitting.value) { detailsOpen.value = false; emit('close') } }
function goBack() { if (step.value > 1) { step.value -= 1; confirmed.value = false } else closeApplication() }
async function loadOrders(append = false) {
  if (ordersLoading.value) return
  const current = generation; ordersLoading.value = true
  try {
    const { data } = await invoiceAPI.getEligibleOrders(append ? nextOrderCursor.value : undefined)
    if (current !== generation) return
    const known = new Set(orders.value.map(o => o.id))
    orders.value = append ? [...orders.value, ...data.items.filter(o => !known.has(o.id))] : data.items
    nextOrderCursor.value = data.next_after; hasMoreOrders.value = data.has_more
  } catch (err) { if (current === generation) report(err) } finally { if (current === generation) ordersLoading.value = false }
}
async function handleSubmit() {
  if (submitting.value || step.value !== 3 || !confirmed.value || !feeQuote.value) return
  submitting.value = true
  try {
    const { data } = await invoiceAPI.create({ ...form, unit_price_cent: parseInvoiceUnitPrice(unitPrice.value), order_ids: [...selectedOrderIds.value], expected_amount_cent: feeQuote.value.amount_cent, expected_fee_cent: feeQuote.value.fee_cent, pricing_version: feeQuote.value.pricing_version })
    appStore.showSuccess(t('invoice.apply.submitSuccess')); emit('created', data); emit('close')
  } catch (err) { report(err) } finally { submitting.value = false }
}
watch(() => props.show, async open => {
  generation += 1; ordersLoading.value = false; detailsOpen.value = false
  if (!open) return
  const current = generation
  step.value = 1; orders.value = []; selectedOrderIds.value = []; titles.value = []; nextOrderCursor.value = 0; hasMoreOrders.value = false
  pricing.value = null; feeQuote.value = null; quoting.value = false; pasteText.value = ''; pasteOpen.value = false; unitPrice.value = ''; confirmed.value = false; Object.assign(form, emptyForm())
  void loadOrders()
  const results = await Promise.allSettled([invoiceAPI.getTitles(), invoiceAPI.getConfig()]); if (current !== generation) return
  if (results[0].status === 'fulfilled') titles.value = results[0].value.data
  if (results[1].status === 'fulfilled') pricing.value = results[1].value.data
}, { immediate: true })
</script>
<style scoped>
.invoice-form-label { @apply mb-2 block text-sm font-semibold text-slate-700 dark:text-gray-200; }
.invoice-form-input { @apply min-h-12 rounded-lg border-slate-200 text-base focus:border-primary-400; }
.invoice-form-hint { @apply mt-2 text-xs leading-relaxed text-slate-500 dark:text-gray-400; }
.invoice-optional { @apply ml-1 text-xs font-semibold text-primary-600; }
</style>
