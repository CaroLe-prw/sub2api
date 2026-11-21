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
    expect(wrapper.get('[data-testid="candy-test-counts"]').text()).toContain('1,3,1')
    expect(wrapper.findAll('[data-status]').map(node => node.attributes('data-status'))).toEqual(['passed', 'failed', 'error'])
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
})
