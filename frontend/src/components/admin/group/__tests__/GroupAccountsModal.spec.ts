import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import GroupAccountsModal from '../GroupAccountsModal.vue'

const { listAccounts, updateAccount, checkMixedChannelRisk, showError, showSuccess } = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  updateAccount: vi.fn(),
  checkMixedChannelRisk: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      update: updateAccount,
      checkMixedChannelRisk
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
}

const accounts = [
  {
    id: 1,
    name: 'Existing account',
    platform: 'openai',
    status: 'active',
    priority: 5,
    group_ids: [7, 9],
    account_groups: [
      { account_id: 1, group_id: 7, priority: 3, created_at: '' },
      { account_id: 1, group_id: 9, priority: 12, created_at: '' }
    ]
  },
  {
    id: 2,
    name: 'New account',
    platform: 'openai',
    status: 'active',
    priority: 6,
    group_ids: [8]
  },
  {
    id: 3,
    name: 'Unchanged account',
    platform: 'anthropic',
    status: 'inactive',
    priority: 7,
    groups: [{ id: 7, name: 'Target group' }]
  }
]

describe('GroupAccountsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listAccounts.mockResolvedValue({
      items: accounts,
      total: accounts.length,
      page: 1,
      page_size: 1000,
      pages: 1
    })
    updateAccount.mockResolvedValue(undefined)
    checkMixedChannelRisk.mockResolvedValue({ has_risk: false })
  })

  it('loads links and saves only changed accounts while preserving other groups', async () => {
    const wrapper = mount(GroupAccountsModal, {
      props: {
        show: true,
        group: { id: 7, name: 'Target group', platform: 'openai' } as any
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Icon: true,
          PlatformIcon: true
        }
      }
    })
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledWith(1, 1000, {
      lite: 'true',
      sort_by: 'name',
      sort_order: 'asc'
    })

    const checkboxes = wrapper.findAll<HTMLInputElement>('input[type="checkbox"]')
    expect(checkboxes.map((checkbox) => checkbox.element.checked)).toEqual([true, false, true])

    await checkboxes[0].setValue(false)
    await checkboxes[1].setValue(true)
    const saveButton = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.groups.manageAccountsSave')
    )
    expect(saveButton).toBeTruthy()
    await saveButton!.trigger('click')
    await flushPromises()

    expect(updateAccount.mock.calls).toEqual([
      [1, { group_ids: [9], group_priorities: { 9: 12 } }],
      [2, { group_ids: [8, 7], group_priorities: {} }]
    ])
    expect(updateAccount).not.toHaveBeenCalledWith(3, expect.anything())
    expect(showSuccess).toHaveBeenCalledWith('admin.groups.manageAccountsSaved')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('requires confirmation before saving a mixed-channel link', async () => {
    listAccounts.mockResolvedValue({
      items: [{
        id: 4,
        name: 'Anthropic account',
        platform: 'anthropic',
        status: 'active',
        priority: 2,
        group_ids: [9],
        account_groups: [{ account_id: 4, group_id: 9, priority: 6, created_at: '' }]
      }],
      total: 1,
      page: 1,
      page_size: 1000,
      pages: 1
    })
    checkMixedChannelRisk.mockResolvedValue({
      has_risk: true,
      details: {
        group_id: 7,
        group_name: 'Target group',
        current_platform: 'Anthropic',
        other_platform: 'Antigravity'
      }
    })

    const wrapper = mount(GroupAccountsModal, {
      props: {
        show: true,
        group: { id: 7, name: 'Target group', platform: 'anthropic' } as any
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Icon: true,
          PlatformIcon: true
        }
      }
    })
    await flushPromises()

    await wrapper.get('input[type="checkbox"]').setValue(true)
    const saveButton = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.groups.manageAccountsSave')
    )
    await saveButton!.trigger('click')
    await flushPromises()

    expect(checkMixedChannelRisk).toHaveBeenCalledWith({
      platform: 'anthropic',
      group_ids: [9, 7],
      account_id: 4
    })
    expect(updateAccount).not.toHaveBeenCalled()

    const confirmButton = wrapper.findAll('button').find((button) => button.text() === 'common.confirm')
    expect(confirmButton).toBeTruthy()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(updateAccount).toHaveBeenCalledWith(4, {
      group_ids: [9, 7],
      group_priorities: { 9: 6 },
      confirm_mixed_channel_risk: true
    })
    expect(wrapper.emitted('success')).toHaveLength(1)
  })
})
