import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import InvoiceFeePaymentDialog from '../InvoiceFeePaymentDialog.vue'
import type { InvoiceRecord } from '@/types/invoice'
const payment = vi.hoisted(() => ({ getLimits: vi.fn(), createOrder: vi.fn(), getOrder: vi.fn(), verifyOrder: vi.fn(), cancelOrder: vi.fn() }))
const invoice = vi.hoisted(() => ({ feePayment: vi.fn(), detail: vi.fn() }))
vi.mock('@/api/payment', () => ({ paymentAPI: payment }))
vi.mock('@/api/invoice', () => ({ invoiceAPI: invoice }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ resolve: () => ({ href: '/payment/stripe?order_id=11' }), push: vi.fn() }) }))
vi.mock('qrcode', () => ({ default: { toDataURL: async () => 'data:image/png;base64,cXI=' } }))
enableAutoUnmount(afterEach)
const record = { id: 'INV-1', status: 'PENDING_PAYMENT', service_fee_cent: 2100 } as InvoiceRecord
beforeEach(() => {
  vi.clearAllMocks()
  window.localStorage.clear()
  payment.getLimits.mockResolvedValue({ data: { methods: { alipay: { available: true, currency: 'CNY' }, stripe: { available: true, currency: 'USD' } } } })
  invoice.feePayment.mockResolvedValue({ data: null })
  invoice.detail.mockResolvedValue({ data: record })
  payment.getOrder.mockResolvedValue({ data: { status: 'PENDING' } })
  payment.createOrder.mockResolvedValue({ data: { order_id: 11, amount: 21, pay_amount: 21, fee_rate: 0, qr_code: 'https://cashier.invalid/pay', out_trade_no: 'pay-11', expires_at: '2099-01-01T00:00:00Z' } })
})
async function openDialog() {
  const wrapper = mount(InvoiceFeePaymentDialog, { props: { show: true, record }, global: { stubs: { BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' } } } })
  await flushPromises(); return wrapper
}
describe('invoice fee checkout', () => {
  it('uses invoice_fee and the application ID without trusting a client amount', async () => {
    const wrapper = await openDialog()
    expect(wrapper.text()).toContain('21.00')
    expect(wrapper.text()).not.toContain('Stripe')
    await wrapper.findAll('button').find(button => button.text() === '支付宝')!.trigger('click'); await flushPromises()
    expect(payment.createOrder).toHaveBeenCalledWith(expect.objectContaining({ invoice_id: 'INV-1', order_type: 'invoice_fee', amount: 0, payment_type: 'alipay' }))
    expect(wrapper.find('img').exists()).toBe(true)
    expect(wrapper.emitted('paid')).toBeUndefined()
  })
  it('waits for the server invoice transition before reporting successful payment', async () => {
    const wrapper = await openDialog()
    invoice.detail.mockResolvedValueOnce({ data: { ...record, status: 'PENDING_REVIEW', fee_paid: true } })
    await wrapper.findAll('button').find(button => button.text() === 'invoice.fees.checkPaid')!.trigger('click'); await flushPromises()
    expect(wrapper.emitted('paid')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
