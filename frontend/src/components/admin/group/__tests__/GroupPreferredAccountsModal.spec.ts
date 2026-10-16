import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/types'
import GroupPreferredAccountsModal from '../GroupPreferredAccountsModal.vue'

const mocks = vi.hoisted(() => ({ getPreferredAccounts: vi.fn(), setPreferredAccount: vi.fn(), deletePreferredAccount: vi.fn(), users: vi.fn(), accounts: vi.fn(), showError: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: mocks, users: { list: mocks.users }, accounts: { list: mocks.accounts } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
const group = (id: number) => ({ id, name: `Group ${id}` } as AdminGroup)
const entry = { user_id: 7, user_name: 'Test', user_email: 'test@example.com', account_id: 1, account_name: 'Upstream', account_status: 'active', in_group: true }
beforeEach(() => {
  vi.resetAllMocks()
  mocks.getPreferredAccounts.mockResolvedValue([entry])
  mocks.setPreferredAccount.mockResolvedValue(undefined)
  mocks.deletePreferredAccount.mockResolvedValue(undefined)
  mocks.accounts.mockResolvedValue({ items: [{ id: 2, name: 'Replacement', status: 'active' }] })
})
afterEach(() => vi.useRealTimers())

function open() {
  return mount(GroupPreferredAccountsModal, {
    props: { show: true, group: group(10) },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } }
  })
}

describe('user preferred accounts', () => {
  it('replaces only the selected user preference with an account from the current group', async () => {
    vi.useFakeTimers()
    const wrapper = open()
    await flushPromises()
    await wrapper.get('tbody button').trigger('click')
    await wrapper.get('#preferred-account').setValue('Replacement')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(250)
    await flushPromises()
    expect(mocks.accounts).toHaveBeenCalledWith(1, 30, { group: '10', search: 'Replacement', lite: 'true' })
    await wrapper.findAll('button').find(button => button.text().includes('#2 Replacement'))!.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.setPreferredAccount).toHaveBeenCalledWith(10, 7, 2)
  })

  it('removes only the selected user in the selected group', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.findAll('tbody button')[1].trigger('click')
    await flushPromises()
    expect(mocks.deletePreferredAccount).toHaveBeenCalledWith(10, 7)
  })

  it('does not display a previous group response after switching groups', async () => {
    let finish!: (value: unknown) => void
    mocks.getPreferredAccounts.mockReturnValueOnce(new Promise(resolve => { finish = resolve })).mockResolvedValueOnce([{ ...entry, user_id: 8, user_name: 'Current' }])
    const wrapper = open()
    await wrapper.setProps({ group: group(20) })
    await flushPromises()
    finish([entry])
    await flushPromises()
    expect(wrapper.get('tbody').text()).toContain('Current')
    expect(wrapper.get('tbody').text()).not.toContain('Test')
  })

  it('requires a new account selection when the existing account was removed from the group', async () => {
    mocks.getPreferredAccounts.mockResolvedValue([{ ...entry, in_group: false }])
    const wrapper = open()
    await flushPromises()
    await wrapper.get('tbody button').trigger('click')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(mocks.setPreferredAccount).not.toHaveBeenCalled()
  })
})
