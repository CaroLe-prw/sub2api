<template>
  <BaseDialog :show="show" :title="t('invoice.fees.payTitle')" @close="emit('close')">
    <div v-if="record" class="space-y-4">
      <div class="rounded-xl bg-primary-50 p-4 text-center dark:bg-primary-900/20"><p class="text-sm">{{ t('invoice.fees.amountDue') }}</p><p class="mt-1 text-3xl font-bold text-primary-600">¥{{ formatInvoiceCent(record.service_fee_cent) }}</p><p class="mt-2 text-xs text-gray-500">{{ t('invoice.fees.noCredit') }}</p></div>
      <p v-if="loading" class="text-center">{{ t('common.loading') }}</p>
      <template v-if="checkout && activeCheckout">
        <img v-if="qrImage" :src="qrImage" class="mx-auto h-56 w-56 rounded-lg bg-white p-2" :alt="t('invoice.fees.qr')" />
        <a v-if="safePayURL" :href="safePayURL" target="_blank" rel="noopener noreferrer" class="btn btn-primary block text-center">{{ t('invoice.fees.openPayment') }}</a>
        <p class="text-center text-sm text-gray-500">{{ t('invoice.fees.waiting') }}</p>
        <button class="btn btn-secondary w-full" :disabled="busy" @click="cancelPayment">{{ t('invoice.fees.cancelPayment') }}</button>
      </template>
      <div v-else class="grid grid-cols-2 gap-3">
        <button v-for="method in methods" :key="method.key" class="btn btn-secondary" :disabled="busy" @click="pay(method.key)">{{ method.label }}</button>
        <p v-if="!loading && !methods.length" class="col-span-2 text-sm text-amber-600">{{ t('invoice.fees.noMethods') }}</p>
      </div>
      <p v-if="error" class="text-sm text-red-600">{{ error }}</p>
    </div>
    <template #footer><button class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button><button class="btn btn-primary" :disabled="busy" @click="check(true)">{{ t('invoice.fees.checkPaid') }}</button></template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import QRCode from 'qrcode'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { paymentAPI } from '@/api/payment'
import { invoiceAPI } from '@/api/invoice'
import type { InvoiceRecord } from '@/types/invoice'
import type { CreateOrderResult } from '@/types/payment'
import { decidePaymentLaunch, readPaymentRecoverySnapshot, writePaymentRecoverySnapshot, PAYMENT_RECOVERY_STORAGE_KEY } from '@/components/payment/paymentFlow'
import { formatInvoiceCent } from './invoiceUtils'
const props = defineProps<{ show: boolean; record: InvoiceRecord | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'paid'): void }>()
const { t } = useI18n(), router = useRouter()
type Checkout = CreateOrderResult & { status?: string; payment_type?: string }
const checkout = ref<Checkout | null>(null), qrImage = ref(''), error = ref(''), busy = ref(false), loading = ref(false)
const methods = ref<{ key: string; label: string }[]>([])
let generation = 0, polling = false, pollCount = 0, timer: ReturnType<typeof setInterval> | undefined
const activeCheckout = computed(() => checkout.value && !['EXPIRED', 'CANCELLED', 'FAILED', 'REFUNDED'].includes(checkout.value.status || '') && (Date.parse(checkout.value.expires_at) > Date.now() || ['PAID', 'RECHARGING'].includes(checkout.value.status || '')))
const safePayURL = computed(() => { const url = checkout.value?.pay_url; if (!url) return ''; try { const u = new URL(url, window.location.origin); return ['https:', 'http:'].includes(u.protocol) ? u.href : '' } catch { return '' } })
function message(err: unknown) { return (err as { message?: string })?.message || t('common.error') }
async function applyCheckout(value: Checkout | null) {
  const current = generation
  const saved = readPaymentRecoverySnapshot(window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY))
  if (value && saved?.orderType === 'invoice_fee' && saved.orderId === value.order_id && saved.clientSecret) {
    value = { ...value, client_secret: saved.clientSecret, intent_id: saved.intentId, pay_url: saved.payUrl }
  }
  checkout.value = value
  const image = value?.qr_code ? await QRCode.toDataURL(value.qr_code, { width: 256 }).catch(() => '') : ''
  if (current === generation) qrImage.value = image
}
async function pay(method: string) {
  if (!props.record || busy.value) return
  busy.value = true; error.value = ''
  const current = generation
  try {
    const { data } = await paymentAPI.createOrder({ amount: 0, order_type: 'invoice_fee', invoice_id: props.record.id, payment_type: method, is_mobile: false, return_url: `${window.location.origin}/payment/result` })
    if (current !== generation) return
    const stripeURL = router.resolve({ path: '/payment/stripe', query: { order_id: String(data.order_id), method: method === 'stripe' ? undefined : method === 'wxpay' ? 'wechat_pay' : 'alipay', resume_token: data.resume_token || undefined } }).href
    const airwallexURL = router.resolve({ path: '/payment/airwallex', query: { order_id: String(data.order_id), out_trade_no: data.out_trade_no, resume_token: data.resume_token || undefined } }).href
    const decision = decidePaymentLaunch(data, { visibleMethod: method, orderType: 'invoice_fee', isMobile: false, isWechatBrowser: false, forceQRCode: true, stripePopupUrl: stripeURL, stripeRouteUrl: stripeURL, airwallexRouteUrl: airwallexURL })
    writePaymentRecoverySnapshot(window.localStorage, decision.recovery, PAYMENT_RECOVERY_STORAGE_KEY)
    await applyCheckout(data)
    if (data.client_secret) await router.push(data.intent_id ? airwallexURL : stripeURL)
  } catch (err) {
    if (current !== generation) return
    error.value = message(err)
    const existing = await invoiceAPI.feePayment(props.record.id).catch(() => null)
    if (existing && current === generation) await applyCheckout(existing.data)
  } finally { if (current === generation) busy.value = false }
}
async function cancelPayment() {
  if (!checkout.value || busy.value) return
  busy.value = true
  try { await paymentAPI.cancelOrder(checkout.value.order_id); await applyCheckout(null); await check(false) }
  catch (err) { error.value = message(err) } finally { busy.value = false }
}
async function check(force = false) {
  if (!props.show || !props.record || polling) return
  polling = true; const current = generation
  try {
    if (checkout.value?.out_trade_no && (force || ++pollCount % 3 === 0)) await paymentAPI.verifyOrder(checkout.value.out_trade_no)
    const { data } = await invoiceAPI.detail(props.record.id)
    if (current !== generation) return
    if (data.status !== 'PENDING_PAYMENT') { emit('paid'); emit('close'); return }
    if (checkout.value) { const order = await paymentAPI.getOrder(checkout.value.order_id); if (current === generation && checkout.value) checkout.value.status = order.data.status }
    if (checkout.value && !checkout.value.qr_code && !checkout.value.pay_url && !checkout.value.client_secret) {
      const latest = await invoiceAPI.feePayment(props.record.id)
      if (current === generation) await applyCheckout(latest.data)
    }
  } catch (err) { if (force && current === generation) error.value = message(err) } finally { polling = false }
}
watch(() => [props.show, props.record?.id] as const, async ([show, id]) => {
  const current = ++generation; if (timer) clearInterval(timer); checkout.value = null; qrImage.value = ''; error.value = ''; busy.value = false; methods.value = []
  if (!show || !id) return
  loading.value = true
  try {
    const [limits, previous] = await Promise.all([paymentAPI.getLimits(), invoiceAPI.feePayment(id)])
    if (current !== generation) return
    methods.value = Object.entries(limits.data.methods).filter(([, m]) => m.available && (!m.currency || m.currency === 'CNY')).map(([key, m]) => ({ key, label: m.display_name || ({ alipay: '支付宝', wxpay: '微信支付', stripe: 'Stripe', airwallex: 'Airwallex' } as Record<string, string>)[key] || key }))
    await applyCheckout(previous.data)
    timer = setInterval(() => { void check() }, 3000)
    void check()
  } catch (err) { if (current === generation) error.value = message(err) } finally { if (current === generation) loading.value = false }
}, { immediate: true })
onUnmounted(() => { generation += 1; if (timer) clearInterval(timer) })
</script>
