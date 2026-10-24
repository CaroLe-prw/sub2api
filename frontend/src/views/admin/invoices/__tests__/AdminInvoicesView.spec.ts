import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AdminInvoicesView from '../AdminInvoicesView.vue'
const api = vi.hoisted(() => ({ list: vi.fn(), getConfig: vi.fn(), updateConfig: vi.fn(), action: vi.fn(), upload: vi.fn() }))
vi.mock('@/api/admin/invoice', () => ({ adminInvoiceAPI: api, default: api }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, te: () => true }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: { q: 'INV-from-mail' } }) }))
enableAutoUnmount(afterEach)
const record = { id: 'manual-1', status: 'APPROVED', company_name: 'Example', tax_number: '913100000000000000', amount_cent: 10000, requires_manual_check: false, downloadable: false }
function openPage(row = record) {
  api.list.mockResolvedValue({ data: { items: [row], total: 1 } })
  return mount(AdminInvoicesView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    DataTable: { props: ['data'], template: '<div><slot v-for="row in data" name="cell-actions" :row="row" /></div>' },
    BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
    Select: true, Toggle: true, Pagination: true, SearchInput: true, InvoiceDetailDialog: true
  } } })
}
beforeEach(() => { vi.resetAllMocks(); api.getConfig.mockResolvedValue({ data: { enabled: true } }); api.upload.mockResolvedValue({ data: { ...record, status: 'COMPLETED', downloadable: true } }) })
async function selectFile(wrapper: ReturnType<typeof openPage>) {
  await wrapper.findAll('button').find(b => b.text() === 'invoice.manual.upload')!.trigger('click')
  const input = wrapper.get('[data-testid="invoice-file"]')
  const file = new File(['%PDF-1.7\n%%EOF'], 'invoice.pdf', { type: 'application/pdf' })
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change'); return file
}
describe('manual invoice administration', () => {
  it('shows a simple manual page and uploads the approved invoice', async () => {
    const wrapper = openPage(); await flushPromises()
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(api.list).toHaveBeenCalledWith(expect.objectContaining({ q: 'INV-from-mail' }))
    const file = await selectFile(wrapper)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.upload).toHaveBeenCalledWith('manual-1', file, '', false)
    expect(api.list).toHaveBeenCalledTimes(2)
  })
  it('requires explicit checking before completing a legacy request', async () => {
    const wrapper = openPage({ ...record, requires_manual_check: true }); await flushPromises()
    const file = await selectFile(wrapper)
    await wrapper.get('form').trigger('submit'); await flushPromises(); expect(api.upload).not.toHaveBeenCalled()
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.upload).toHaveBeenCalledWith('manual-1', file, '', true)
  })
})
