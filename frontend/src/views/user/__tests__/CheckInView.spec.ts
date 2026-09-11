import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import CheckInView from '../CheckInView.vue'

const { getOverview, claim, refreshUser } = vi.hoisted(() => ({
  getOverview: vi.fn(), claim: vi.fn(), refreshUser: vi.fn(),
}))
vi.mock('@/api/checkIn', () => ({ checkInAPI: { getOverview, claim } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
  locale: ref('zh'),
  t: (key: string, params?: Record<string, unknown>) => params ? `${key} ${JSON.stringify(params)}` : key,
}) }))

function overview(overrides = {}) {
  return {
    today: '2026-09-11', timezone: 'Asia/Shanghai', year: 2026, month: 9,
    checked_in_today: false, today_reward: 0, current_streak: 0, total_days: 0,
    month_days: 0, month_reward: 0, total_reward: 0, balance: 100,
    reward_min: 0.01, reward_max: 0.15, records: [],
    min_recharge: 0, recharged_amount: 0, recharge_remaining: 0, eligible: true,
    ...overrides,
  }
}
function renderView() {
  return mount(CheckInView, { global: { stubs: {
    AppLayout: { template: '<main><slot /></main>' }, Icon: true, LoadingSpinner: true,
  } } })
}

describe('Check-in recharge eligibility', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getOverview.mockResolvedValue(overview())
    claim.mockResolvedValue({ created: true, record: { reward: 0.05 } })
  })

  it('allows a user with no recharge to claim when no threshold is configured', async () => {
    const wrapper = renderView()
    await flushPromises()
    const button = wrapper.get('[data-testid="check-in-claim"]')
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click')
    await flushPromises()
    expect(claim).toHaveBeenCalledTimes(1)
  })

  it('blocks claiming and shows the shortfall until recharge reaches the threshold', async () => {
    getOverview.mockResolvedValue(overview({ min_recharge: 10, recharged_amount: 3, recharge_remaining: 7, eligible: false }))
    const wrapper = renderView()
    await flushPromises()
    expect(wrapper.text()).toContain('checkIn.rechargeRequiredTitle')
    expect(wrapper.text()).toContain('"remaining":"7"')
    const button = wrapper.get('[data-testid="check-in-claim"]')
    expect(button.attributes('disabled')).toBeDefined()
    await button.trigger('click')
    expect(claim).not.toHaveBeenCalled()

    getOverview.mockResolvedValue(overview({ min_recharge: 10, recharged_amount: 10 }))
    await wrapper.findAll('button').find(b => b.text().includes('checkIn.rechargeRefresh'))!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="check-in-claim"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="check-in-claim"]').trigger('click')
    await flushPromises()
    expect(claim).toHaveBeenCalledTimes(1)
  })

  it('refreshes eligibility when an administrator raises the requirement before a claim', async () => {
    const wrapper = renderView()
    await flushPromises()
    claim.mockRejectedValue({ reason: 'CHECK_IN_RECHARGE_REQUIRED' })
    getOverview.mockResolvedValue(overview({ min_recharge: 10, recharge_remaining: 10, eligible: false }))
    await wrapper.get('[data-testid="check-in-claim"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('checkIn.rechargeChanged')
    expect(wrapper.get('[data-testid="check-in-claim"]').attributes('disabled')).toBeDefined()
  })

  it('keeps completed check-ins visible even if the requirement changes', async () => {
    getOverview.mockResolvedValue(overview({ checked_in_today: true, today_reward: 0.05, min_recharge: 10, eligible: false }))
    const wrapper = renderView()
    await flushPromises()
    expect(wrapper.text()).toContain('checkIn.doneTitle')
    expect(wrapper.get('[data-testid="check-in-claim"]').text()).toContain('checkIn.claimed')
    expect(wrapper.get('[data-testid="check-in-claim"]').attributes('disabled')).toBeDefined()
  })
})
