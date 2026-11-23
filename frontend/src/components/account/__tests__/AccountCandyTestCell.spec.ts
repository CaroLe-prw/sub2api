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
const account = (overrides: Partial<Account> = {}) => ({ id: 42, name: 'Codex', platform: 'openai', type: 'oauth', schedulable: true, extra: {}, ...overrides } as Account)
const result = (): CandyTestSnapshot => ({ total: 3, passed: 1, failed: 1, errors: 1, recent: [
  { status: 'passed', model: 'gpt-6.2-sol', checked_at: '2026-11-21T01:00:00Z', latency_ms: 2000 },
  { status: 'failed', model: 'gpt-6.2-sol', checked_at: '2026-11-21T02:00:00Z', latency_ms: 2000 },
  { status: 'error', model: 'gpt-6.2-sol', checked_at: '2026-11-21T03:00:00Z', latency_ms: 2000 }
] })

beforeEach(() => { vi.clearAllMocks(); runCandyTest.mockResolvedValue(result()) })
describe('AccountCandyTestCell', () => {
  it.each(['oauth', 'apikey'] as const)('runs and displays saved results for OpenAI %s', async (type) => {
    const saved = result()
    const original = account({ type, schedulable: true, extra: { candy_test: saved } })
    const wrapper = mount(AccountCandyTestCell, { props: { account: original, groupId: 7 } })
    expect(wrapper.find('[data-testid="account-candy-test"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-status]')).toHaveLength(3)
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(false)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(runCandyTest).toHaveBeenCalledTimes(1)
    expect(runCandyTest.mock.calls[0].slice(0, 2)).toEqual([42, 7])
    expect(wrapper.emitted('updated')?.[0]).toEqual([result()])
    expect(original.schedulable).toBe(true)
    wrapper.unmount()
  })

  it.each([
    { platform: 'anthropic', type: 'apikey' },
    { platform: 'openai', type: 'setup-token' },
    { platform: 'anthropic', type: 'oauth' },
    { platform: 'gemini', type: 'oauth' }
  ])('does not expose testing for %o', (fields) => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account(fields as Partial<Account>) } })
    expect(wrapper.find('button').exists()).toBe(false)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('opens a historical wrong answer without rerunning the test or changing scheduling', async () => {
    const saved = result()
    saved.recent[1].response_text = '最少需要 34 个糖果。'
    const original = account({ schedulable: true, extra: { candy_test: saved } })
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: original }, global: { stubs: { teleport: true } }
    })
    const record = wrapper.get('button[data-status="failed"]')
    expect(record.attributes('aria-label')).toContain('admin.accounts.candyTest.viewRecord')
    await record.trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('admin.accounts.candyTest.detailsTitle')
    expect(wrapper.get('[data-testid="candy-record-details"]').text()).toContain('gpt-6.2-sol')
    expect(wrapper.get('[data-testid="candy-model-answer"]').text()).toBe('最少需要 34 个糖果。')
    expect(runCandyTest).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(original.schedulable).toBe(true)
    await wrapper.get('button[aria-label="Close modal"]').trigger('click')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('shows the effort actually used in a saved test record', async () => {
    const saved = result()
    saved.recent[1].reasoning_effort = 'high'
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: account({ extra: { candy_test: saved } }) }, global: { stubs: { teleport: true } }
    })
    const record = wrapper.get('button[data-status="failed"]')
    expect(record.attributes('title')).toContain('gpt-6.2-sol · high ·')
    await record.trigger('click')
    expect(wrapper.get('[data-testid="candy-record-details"]').text()).toContain('gpt-6.2-sol · high ·')
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('explains that old answers cannot be reconstructed', async () => {
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: account({ extra: { candy_test: result() } }) }, global: { stubs: { teleport: true } }
    })
    await wrapper.get('button[data-status="failed"]').trigger('click')
    expect(wrapper.get('[data-testid="candy-answer-unavailable"]').text()).toBe('admin.accounts.candyTest.answerNotRecorded')
    expect(wrapper.get('[data-testid="candy-record-details"]').text()).not.toContain('medium')
    expect(wrapper.find('[data-testid="candy-model-answer"]').exists()).toBe(false)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('explains API errors without fabricating a wrong answer', async () => {
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: account({ extra: { candy_test: result() } }) }, global: { stubs: { teleport: true } }
    })
    await wrapper.get('button[data-status="error"]').trigger('click')
    expect(wrapper.get('[data-testid="candy-answer-unavailable"]').text()).toBe('admin.accounts.candyTest.noAnswerOnError')
    expect(wrapper.find('[data-testid="candy-model-answer"]').exists()).toBe(false)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('renders answers as plain text and marks truncated excerpts', async () => {
    const saved = result()
    saved.recent[0].response_text = '<img src=x onerror="alert(1)">21<script>alert(2)</script>'
    saved.recent[0].response_truncated = true
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: account({ extra: { candy_test: saved } }) }, global: { stubs: { teleport: true } }
    })
    await wrapper.get('button[data-status="passed"]').trigger('click')
    const answer = wrapper.get('[data-testid="candy-model-answer"]')
    expect(answer.text()).toBe(saved.recent[0].response_text)
    expect(answer.find('img').exists()).toBe(false)
    expect(answer.find('script').exists()).toBe(false)
    expect(wrapper.get('[data-testid="candy-response-truncated"]').text()).toBe('admin.accounts.candyTest.answerTruncated')
    wrapper.unmount()
  })
  it('keeps an opened record stable across refreshes and closes it when the account changes', async () => {
    const saved = result()
    saved.recent[1].response_text = '答案是34'
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: account({ extra: { candy_test: saved } }) }, global: { stubs: { teleport: true } }
    })
    await wrapper.get('button[data-status="failed"]').trigger('click')
    await wrapper.setProps({ account: account({ extra: { candy_test: { ...result(), recent: [] } } }) })
    expect(wrapper.get('[data-testid="candy-model-answer"]').text()).toBe('答案是34')
    await wrapper.setProps({ account: account({ id: 43 }) })
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
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
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(false)
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
  it.each([
    { reason: 'CANDY_TEST_MODEL_NOT_ALLOWED' },
    { response: { data: { code: 'CANDY_TEST_MODEL_NOT_ALLOWED' } } },
  ])('explains the account model restriction without changing prior results', async (error) => {
    runCandyTest.mockRejectedValue(error)
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: result() } }) } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.candyTest.modelNotAllowed')
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(wrapper.findAll('[data-status]')).toHaveLength(3)
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
  it('retains a two-failure block when the latest test is an API error', () => {
    const saved = { ...result(), total: 4, failed: 2, consecutive_failures: 2 }
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: saved } }) } })
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
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('removes the scheduling block after a passing retest without changing the manual switch', async () => {
    const original = account({ schedulable: true, extra: { candy_test: { ...result(), total: 4, failed: 2, consecutive_failures: 2 } } })
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
    expect(original.schedulable).toBe(true)
    wrapper.unmount()
  })

  it.each(['oauth', 'apikey'] as const)('pauses disabled %s accounts but keeps old records readable', async (type) => {
    const saved = result()
    const original = account({ type, schedulable: false, extra: { candy_test: saved } })
    const wrapper = mount(AccountCandyTestCell, {
      props: { account: original }, global: { stubs: { teleport: true } }
    })
    const button = wrapper.get('button')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('title')).toBe('admin.accounts.candyTest.schedulingDisabled')
    expect(wrapper.get('[data-testid="candy-test-disabled"]').text()).toBe('admin.accounts.candyTest.schedulingDisabled')
    await button.trigger('click')
    await flushPromises()
    expect(runCandyTest).not.toHaveBeenCalled()
    expect(wrapper.emitted('updated')).toBeUndefined()
    expect(original.extra?.candy_test).toEqual(saved)
    await wrapper.get('button[data-status="failed"]').trigger('click')
    expect(wrapper.find('[data-testid="candy-record-details"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(false)
    expect(runCandyTest).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reenables testing when the manual scheduling switch is turned back on', async () => {
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ schedulable: false }) } })
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
    await wrapper.setProps({ account: account({ schedulable: true }) })
    expect(wrapper.find('[data-testid="candy-test-disabled"]').exists()).toBe(false)
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(runCandyTest).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('explains a switch disabled on the server since the last list refresh', async () => {
    runCandyTest.mockRejectedValue({ response: { data: { code: 'CANDY_TEST_SCHEDULING_DISABLED' } } })
    const wrapper = mount(AccountCandyTestCell, { props: { account: account() } })
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('admin.accounts.candyTest.schedulingDisabled')
    expect(wrapper.emitted('updated')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    { statuses: ['failed'], streak: 1 },
    { statuses: ['failed', 'error'], streak: 1 },
    { statuses: ['failed', 'failed'], streak: 2 },
    { statuses: ['failed', 'error', 'failed'], streak: 2 },
    { statuses: ['failed', 'passed', 'failed'], streak: 1 },
    { statuses: ['failed', 'failed', 'passed'], streak: 0 }
  ] as const)('shows the correct gate for legacy sequence $statuses', ({ statuses, streak }) => {
    const saved: CandyTestSnapshot = {
      total: statuses.length,
      passed: statuses.filter(status => status === 'passed').length,
      failed: statuses.filter(status => status === 'failed').length,
      errors: statuses.filter(status => status === 'error').length,
      recent: statuses.map(status => ({ ...result().recent[0], status }))
    }
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: saved } }) } })
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(streak >= 2)
    expect(wrapper.find('[data-testid="candy-first-failure"]').exists()).toBe(streak === 1)
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([1, 2, 3])('uses persisted streak %s after errors replace the entire visible history', (streak) => {
    const saved: CandyTestSnapshot = {
      last_verdict: 'failed', consecutive_failures: streak, total: 14 + streak, passed: 0,
      failed: streak, errors: 14, recent: Array.from({ length: 12 }, () => result().recent[2])
    }
    const wrapper = mount(AccountCandyTestCell, { props: { account: account({ extra: { candy_test: saved } }) } })
    expect(wrapper.find('[data-testid="candy-scheduling-blocked"]').exists()).toBe(streak >= 2)
    expect(wrapper.find('[data-testid="candy-first-failure"]').exists()).toBe(streak === 1)
    wrapper.unmount()
  })

})
