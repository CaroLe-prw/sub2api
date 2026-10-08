import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { PaymentVerificationAlert } from '@/api/admin/paymentVerification'
import PaymentVerificationSettings from '../PaymentVerificationSettings.vue'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(), updateConfig: vi.fn(), getAlerts: vi.fn(), scan: vi.fn(), connectTelegram: vi.fn(), resolve: vi.fn(),
  showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api/admin/paymentVerification', () => ({ paymentVerificationAPI: mocks }))
vi.mock('@/stores', () => ({ useAppStore: () => mocks }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, values?: object) => values ? `${key} ${JSON.stringify(values)}` : key }) }))

function config() {
  return { enabled: false, template_id: 'payments', lookback_days: 7, interval_minutes: 5,
    admin_telegram_ids: [123], webhook_url: 'https://example.com/api/v1/payment/verification/telegram', webhook_configured: false }
}

function alert(overrides: Partial<PaymentVerificationAlert> = {}): PaymentVerificationAlert {
  return { id: 1, order_id: 949, user_id: 462, email: 'payer@example.com', provider_name: 'EasyPay', out_trade_no: 'sub2_test',
    amount: 10, pay_amount: 7, currency: 'CNY', order_status: 'COMPLETED', result: 'unpaid', reason: 'Payment not confirmed',
    upstream_amount: null, state: 'pending', checked_at: '2026-10-08T10:00:00Z', created_at: '2026-10-08T09:00:00Z', handled_by: '', ...overrides }
}

async function render() {
  const wrapper = mount(PaymentVerificationSettings, {
    props: { templates: [{ id: 'payments', name: 'Payments', enabled: true, bot_token: '', bot_token_configured: true,
      chat_id: '123', topic_id: null, base_url: 'https://api.telegram.org', disable_notification: false, protect_content: false }] },
    global: {
      stubs: {
        Select: { props: ['modelValue', 'options', 'id'], emits: ['update:modelValue'], template: '<select :id="id" :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>' },
        ConfirmDialog: { props: ['show', 'message'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-testid="ban-confirmation">{{ message }}<button data-testid="confirm-ban" @click="$emit(\'confirm\')">Confirm</button><button data-testid="cancel-ban" @click="$emit(\'cancel\')">Cancel</button></div>' }
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('PaymentVerificationSettings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getConfig.mockResolvedValue(config())
    mocks.updateConfig.mockImplementation(async value => ({ ...value, webhook_configured: false }))
    mocks.getAlerts.mockResolvedValue([])
    mocks.connectTelegram.mockResolvedValue(undefined)
    mocks.scan.mockResolvedValue({ checked: 10, anomalies: 1, errors: 2 })
    mocks.resolve.mockImplementation(async (_id, action) => alert({ state: action === 'ban' ? 'banned' : 'ignored', handled_by: 'admin:1' }))
  })

  it('starts with automatic checks disabled and never connects or scans on load', async () => {
    const wrapper = await render()
    expect(wrapper.get('#payment-verification-enabled').attributes('aria-checked')).toBe('false')
    expect(mocks.getAlerts).toHaveBeenCalledWith(50)
    expect(mocks.scan).not.toHaveBeenCalled()
    expect(mocks.connectTelegram).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="connect-payment-bot"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="scan-payment-verification"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('paymentVerification.manualOnly')
    wrapper.unmount()
  })

  it('saves distinct positive administrator IDs without registering a webhook', async () => {
    const wrapper = await render()
    await wrapper.get('#payment-verification-admins').setValue('123, 456\n123，789')
    await wrapper.get('#payment-verification-enabled').trigger('click')
    expect(wrapper.get('[data-testid="connect-payment-bot"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="save-payment-verification"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith({ ...config(), enabled: true, admin_telegram_ids: [123, 456, 789], webhook_configured: undefined })
    expect(mocks.updateConfig.mock.calls[0][0]).not.toHaveProperty('webhook_configured')
    expect(mocks.connectTelegram).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="connect-payment-bot"]').trigger('click')
    await flushPromises()
    expect(mocks.connectTelegram).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('paymentVerification.connected')
    wrapper.unmount()
  })

  it.each(['-123', '0', '1.5', '9007199254740992', '1e3', '@admin'])('rejects invalid Telegram user ID %s', async value => {
    const wrapper = await render()
    await wrapper.get('#payment-verification-admins').setValue(value)
    await wrapper.get('[data-testid="save-payment-verification"]').trigger('click')
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith(expect.stringContaining('invalidAdminIDs'))
    wrapper.unmount()
  })

  it('requires an enabled saved template and authorized administrators when enabling', async () => {
    const wrapper = await render()
    await wrapper.get('#payment-verification-enabled').trigger('click')
    await wrapper.get('#payment-verification-template').setValue('')
    await wrapper.get('[data-testid="save-payment-verification"]').trigger('click')
    expect(mocks.showError).toHaveBeenLastCalledWith(expect.stringContaining('templateRequired'))
    await wrapper.get('#payment-verification-template').setValue('payments')
    await wrapper.get('#payment-verification-admins').setValue('')
    await wrapper.get('[data-testid="save-payment-verification"]').trigger('click')
    expect(mocks.showError).toHaveBeenLastCalledWith(expect.stringContaining('adminsRequired'))
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['http://example.com/callback', 'https://secret@example.com/callback', 'https://example.com/callback?secret=value'])('rejects unsafe callback URL %s', async value => {
    const wrapper = await render()
    await wrapper.get('#payment-verification-webhook').setValue(value)
    await wrapper.get('[data-testid="save-payment-verification"]').trigger('click')
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith(expect.stringContaining('invalidWebhook'))
    wrapper.unmount()
  })

  it('keeps query failures unconfirmed and only permits ignoring them', async () => {
    mocks.getAlerts.mockResolvedValue([alert({ result: 'query_error', upstream_amount: null, reason: 'Gateway timeout' })])
    const wrapper = await render()
    const row = wrapper.get('[data-testid="payment-alert-1"]')
    expect(row.text()).toContain('query_error')
    expect(row.text()).toContain('unknownAmount')
    expect(row.find('[data-testid="ban-payment-user"]').exists()).toBe(false)
    await row.get('[data-testid="ignore-payment-alert"]').trigger('click')
    await flushPromises()
    expect(mocks.resolve).toHaveBeenCalledWith(1, 'ignore')
    expect(wrapper.text()).toContain('states.ignored')
    wrapper.unmount()
  })

  it('requires confirmation before banning and removes actions after resolution', async () => {
    mocks.getAlerts.mockResolvedValue([alert()])
    const wrapper = await render()
    await wrapper.get('[data-testid="ban-payment-user"]').trigger('click')
    expect(mocks.resolve).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="ban-confirmation"]').text()).toContain('462')
    await wrapper.get('[data-testid="cancel-ban"]').trigger('click')
    expect(mocks.resolve).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="ban-payment-user"]').trigger('click')
    await wrapper.get('[data-testid="confirm-ban"]').trigger('click')
    await flushPromises()
    expect(mocks.resolve).toHaveBeenCalledWith(1, 'ban')
    expect(wrapper.find('[data-testid="ban-payment-user"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="ignore-payment-alert"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('checks saved orders on demand and refreshes alert records', async () => {
    mocks.getConfig.mockResolvedValue({ ...config(), enabled: true })
    const wrapper = await render()
    mocks.getAlerts.mockResolvedValue([alert()])
    await wrapper.get('[data-testid="scan-payment-verification"]').trigger('click')
    await flushPromises()
    expect(mocks.scan).toHaveBeenCalledTimes(1)
    expect(mocks.getAlerts).toHaveBeenCalledTimes(2)
    expect(mocks.showSuccess).toHaveBeenCalledWith(expect.stringContaining('"checked":10'))
    expect(wrapper.text()).toContain('payer@example.com')
    expect(wrapper.text()).toContain('10.00 USD')
    expect(wrapper.text()).toContain('7.00 CNY')
    wrapper.unmount()
  })

  it.each([
    ['lookback', 0], ['lookback', 366], ['lookback', 7.5], ['interval', 0], ['interval', 61]
  ])('rejects out-of-range schedule %s=%s', async (field, value) => {
    const wrapper = await render()
    await wrapper.get(`#payment-verification-${field}`).setValue(value)
    await wrapper.get('[data-testid="save-payment-verification"]').trigger('click')
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith(expect.stringContaining('invalidSchedule'))
    wrapper.unmount()
  })

  it('requires saving a suggested callback URL before connecting', async () => {
    mocks.getConfig.mockResolvedValue({ ...config(), enabled: true, webhook_url: '' })
    const wrapper = await render()
    expect((wrapper.get('#payment-verification-webhook').element as HTMLInputElement).value).toBe(`${window.location.origin}/api/v1/payment/verification/telegram`)
    expect(wrapper.get('[data-testid="connect-payment-bot"]').attributes('disabled')).toBeDefined()
    expect(mocks.connectTelegram).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not replace unsaved config when refreshing records', async () => {
    const wrapper = await render()
    await wrapper.get('#payment-verification-lookback').setValue(30)
    await wrapper.get('[data-testid="refresh-payment-verification"]').trigger('click')
    await flushPromises()
    expect((wrapper.get('#payment-verification-lookback').element as HTMLInputElement).value).toBe('30')
    expect(mocks.getConfig).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('prevents saving fallback defaults when loading fails', async () => {
    mocks.getConfig.mockRejectedValue(new Error('offline'))
    const wrapper = await render()
    expect(wrapper.find('[data-testid="save-payment-verification"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="scan-payment-verification"]').attributes('disabled')).toBeDefined()
    expect(mocks.updateConfig).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('shows refresh failures instead of claiming there are no discrepancies', async () => {
    mocks.getAlerts.mockRejectedValue(new Error('offline'))
    const wrapper = await render()
    expect(wrapper.text()).toContain('recordsFailed')
    expect(wrapper.text()).not.toContain('noRecords')
    wrapper.unmount()
  })
})
