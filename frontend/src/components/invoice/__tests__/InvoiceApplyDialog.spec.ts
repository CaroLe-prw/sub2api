import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import InvoiceApplyDialog from '../InvoiceApplyDialog.vue'

const api = vi.hoisted(() => ({ getEligibleOrders: vi.fn(), getTitles: vi.fn(), getConfig: vi.fn(), quote: vi.fn(), create: vi.fn() }))
vi.mock('@/api/invoice', () => ({ invoiceAPI: api }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const order = (id: number) => ({ id, out_trade_no: `PAY-${id}`, amount_cent: 10000, pay_amount_cent: 10200, payment_fee_cent: 200, created_at: '2026-10-01T00:00:00Z' })
async function openDialog() {
  const wrapper = mount(InvoiceApplyDialog, { props: { show: false }, global: { stubs: {
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' }, Icon: true, Select: true
  } } })
  await wrapper.setProps({ show: true }); await flushPromises(); return wrapper
}
beforeEach(() => { vi.resetAllMocks(); api.getTitles.mockResolvedValue({ data: [] }); api.getConfig.mockResolvedValue({ data: { enabled: true, min_amount_cent: 10000, fee_tiers: [{ min_amount_cent: 10000, rate_bps: 0 }], pricing_version: 1 } }); api.quote.mockResolvedValue({ data: { amount_cent: 10000, fee_cent: 0, rate_bps: 0, pricing_version: 1 } }) })
describe('invoice order selection', () => {
  it('loads the next cursor and retains selected orders', async () => {
    api.getEligibleOrders.mockResolvedValueOnce({ data: { items: Array.from({ length: 200 }, (_, i) => order(i + 1)), next_after: 200, has_more: true } })
      .mockResolvedValueOnce({ data: { items: [order(201)], next_after: 201, has_more: false } })
    const wrapper = await openDialog()
    await wrapper.findAll('input[type="checkbox"]')[0].setValue(true)
    await wrapper.get('[data-testid="invoice-load-more"]').trigger('click'); await flushPromises()
    expect(api.getEligibleOrders).toHaveBeenLastCalledWith(200)
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(201)
    expect((wrapper.findAll('input[type="checkbox"]')[0].element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.find('[data-testid="invoice-load-more"]').exists()).toBe(false)
  })
  it('can advance past an empty page of invalid historical amounts', async () => {
    api.getEligibleOrders.mockResolvedValueOnce({ data: { items: [], next_after: 200, has_more: true } })
      .mockResolvedValueOnce({ data: { items: [order(201)], next_after: 201, has_more: false } })
    const wrapper = await openDialog()
    await wrapper.get('[data-testid="invoice-load-more"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('PAY-201')
  })
  it('keeps the reference form, optional tax ID, details drawer and confirmation', async () => {
    api.getEligibleOrders.mockResolvedValue({ data: { items: [order(1)], next_after: 1, has_more: false } })
    api.create.mockResolvedValue({ data: { id: 'manual-invoice' } })
    const wrapper = await openDialog()
    await wrapper.findAll('input[type="checkbox"]')[0].setValue(true)
    await wrapper.get('[data-testid="invoice-next"]').trigger('click')
    await wrapper.get('#invoice-buyer').setValue('Example Ltd')
    expect(wrapper.get('#invoice-tax').attributes('required')).toBeUndefined()
    expect(wrapper.get('#invoice-amount').attributes('readonly')).toBeDefined()
    expect(wrapper.findAll('#invoice-type option')).toHaveLength(1)
    expect(wrapper.findAll('#invoice-item option')).toHaveLength(3)
    await wrapper.get('#invoice-item').setValue('AI订阅服务费')
    await wrapper.get('[data-testid="invoice-details-open"]').trigger('click')
    await wrapper.get('#invoice-quantity').setValue('2.5')
    await wrapper.get('#invoice-unit-price').setValue('3.20')
    await wrapper.get('#invoice-remark').setValue('Optional billing detail')
    await wrapper.get('#invoice-details-form').trigger('submit')
    expect(wrapper.find('#invoice-details-form').exists()).toBe(false)
    await wrapper.get('#invoice-apply-form').trigger('submit'); await flushPromises()
    expect(api.create).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="invoice-submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="invoice-confirmed"]').setValue(true)
    await wrapper.get('[data-testid="invoice-submit"]').trigger('click'); await flushPromises()
    expect(api.create).toHaveBeenCalledWith(expect.objectContaining({ order_ids: [1], company_name: 'Example Ltd', tax_number: '', invoice_type: 'NORMAL', quantity: '2.5', unit_price_cent: 320, remark: 'Optional billing detail' }))
    expect(api.create.mock.calls[0][0]).not.toHaveProperty('amount_cent')
    expect(api.create.mock.calls[0][0]).toHaveProperty('expected_fee_cent', 0)
    expect(api.create.mock.calls[0][0]).not.toHaveProperty('option_id')
    expect(wrapper.emitted('created')).toHaveLength(1)
  })
})
