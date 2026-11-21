import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountCandyTestCell from '../AccountCandyTestCell.vue'
import type { Account } from '@/types'
import type { CandyTestSnapshot } from '@/types/candyTest'

const { runCandyTest, showError } = vi.hoisted(() => ({ runCandyTest: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ runCandyTest }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${Object.values(params).join(',')}` : key
}) }))
const account = (overrides: Partial<Account> = {}) => ({ id: 42, name: 'Codex', platform: 'openai', type: 'oauth', extra: {}, ...overrides } as Account)
const result = (): CandyTestSnapshot => ({ total: 3, passed: 1, failed: 1, errors: 1, recent: [
  { status: 'passed', model: 'gpt-6.2-sol', checked_at: '2026-11-21T01:00:00Z', latency_ms: 2000 },
  { status: 'failed', model: 'gpt-6.2-sol', checked_at: '2026-11-21T02:00:00Z', latency_ms: 2000 },
  { status: 'error', model: 'gpt-6.2-sol', checked_at: '2026-11-21T03:00:00Z', latency_ms: 2000 }
] })

beforeEach(() => { vi.clearAllMocks(); runCandyTest.mockResolvedValue(result()) })
describe('AccountCandyTestCell', () => {
  it.each([
    { platform: 'openai', type: 'apikey' },
    { platform: 'openai', type: 'setup-token' },
    { platform: 'anthropic', type: 'oauth' },
    { platform: 'gemini', type: 'oauth' }
  ])('does not expose testing for %o', (fields) => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account(fields as Partial<Account>) } })
    expect(wrapper.find('button').exists()).toBe(false)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('does not run on mount or fabricate a passing history', () => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account() } })
    expect(wrapper.text()).toContain('admin.accounts.candyTest.untested')
    expect(wrapper.findAll('[data-status]')).toHaveLength(0)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('runs only on click, sends the selected group and emits persisted results', async () => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account(), groupId: 7 } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(runCandyTest).toHaveBeenCalledWith(42, 7, expect.any(AbortSignal))
    expect(wrapper.emitted('updated')?.[0]).toEqual([result()])
    expect(wrapper.get('[data-testid="candy-test-counts"]').text()).toContain('1,2,1')
    expect(wrapper.findAll('[data-status]').map(node => node.attributes('data-status'))).toEqual(['passed', 'failed', 'error'])
    wrapper.unmount()
  })
  it.each([
    { passed: 12, failed: 0, errors: 1, expected: '12,12,1' },
    { passed: 12, failed: 1, errors: 1, expected: '12,13,1' },
    { passed: 0, failed: 0, errors: 3, expected: '0,0,3' }
  ])('counts only conclusive answers in the pass denominator: %o', ({ passed, failed, errors, expected }) => {
    const saved: CandyTestSnapshot = {
      total: passed + failed + errors, passed, failed, errors,
      last_verdict: failed > 0 ? 'failed' : passed > 0 ? 'passed' : undefined,
      // The retained history is not the denominator: cumulative counts survive rollover.
      recent: [result().recent[2]]
    }
    const original = account({ schedulable: true, extra: { candy_test: saved } })
    const wrapper = mount(AccountCandyTestCell, { props: { account: original } })
    expect(wrapper.get('[data-testid="candy-test-counts"]').text()).toBe(`admin.accounts.candyTest.counts:${expected}`)
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(failed > 0)
    expect(wrapper.findAll('[data-status="error"]')).toHaveLength(1)
    expect(saved.total).toBe(passed + failed + errors)
    expect(original.schedulable).toBe(true)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('disables duplicate requests while the first test is pending', async () => {
    let resolve!: (snapshot: CandyTestSnapshot) => void
    runCandyTest.mockReturnValue(new Promise<CandyTestSnapshot>(r => { resolve = r }))
    const wrapper = mount(AccountCandyTestCell, { props: { account: account() } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.accounts.candyTest.running')
    await wrapper.get('button').trigger('click')
    expect(runCandyTest).toHaveBeenCalledTimes(1)
    resolve(result()); await flushPromises()
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it('loads saved results after remount without making a request', () => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: result() } }) } })
    expect(wrapper.findAll('[data-status]')).toHaveLength(3)
    expect(wrapper.text()).toContain('admin.accounts.candyTest.error')
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps prior history when the request fails and does not emit fake success', async () => {
    runCandyTest.mockRejectedValue({ message: 'failed' })
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: result() } }) } })
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.candyTest.requestFailed')
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.findAll('[data-status]')).toHaveLength(3)
    wrapper.unmount()
  })
  it('explains how to resolve conflicting group models', async () => {
    runCandyTest.mockRejectedValue({ reason: 'CANDY_TEST_GROUP_REQUIRED' })
    const wrapper = mount(AccountCandyTestCell, { props: { account: account() } })
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.candyTest.groupRequired')
    wrapper.unmount()
  })
  it('cancels on unmount without showing an error', async () => {
    runCandyTest.mockImplementation((_id, _group, signal: AbortSignal) => new Promise((_resolve, reject) => {
      signal.addEventListener('abort', () => reject(new Error('aborted')))
    }))
    const wrapper = mount(AccountCandyTestCell, { props: { account: account() } })
    await wrapper.get('button').trigger('click'); wrapper.unmount(); await flushPromises()
    expect(runCandyTest.mock.calls[0][2].aborted).toBe(true)
    expect(showError).not.toHaveBeenCalled()
  })
  it('shows a scheduling block after a wrong answer even when the latest test is an API error', () => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: result() } }) } })
    expect(wrapper.text()).toContain('admin.accounts.candyTest.error')
    expect(wrapper.get('[data-testid="candy-scheduling-blocked"]').text()).toBe('admin.accounts.candyTest.blocked')
    wrapper.unmount()
  })
  it.each([undefined, 'passed', 'failed'] as const)('uses the persisted verdict %s when recent history contains only errors', (verdict) => {
    const snapshot: CandyTestSnapshot = {
      last_verdict: verdict, total: 14, passed: verdict === 'passed' ? 1 : 0,
      failed: verdict === 'failed' ? 1 : 0, errors: verdict ? 13 : 14,
      recent: Array.from({ length: 12 }, () => result().recent[2])
    }
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: snapshot } }) } })
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(verdict === 'failed')
    wrapper.unmount()
  })
  it('removes the scheduling block after a passing retest without changing the manual switch', async () => {
    const original = account({ schedulable: false, extra: { candy_test: result() } })
    const wrapper = mount(AccountCandyTestCell, { props: { account: original } })
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(true)
    const passing: CandyTestSnapshot = {
      ...result(), last_verdict: 'passed', total: 4, passed: 2,
      recent: [...result().recent, { status: 'passed', model: 'gpt-6.2-sol', checked_at: '2026-11-21T04:00:00Z', latency_ms: 1000 }]
    }
    runCandyTest.mockResolvedValue(passing)
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(false)
    expect(wrapper.emitted('updated')?.[0]).toEqual([passing])
    expect(original.schedulable).toBe(false)
    wrapper.unmount()
  })

})
