import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { getUserBalanceHistoryMock } = vi.hoisted(() => ({
  getUserBalanceHistoryMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      getUserBalanceHistory: getUserBalanceHistoryMock,
    },
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import UserBalanceHistoryModal from '../UserBalanceHistoryModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /></div>',
})

describe('UserBalanceHistoryModal', () => {
  beforeEach(() => {
    getUserBalanceHistoryMock.mockReset().mockResolvedValue({
      items: [
        {
          id: 91,
          code: 'LOTTERY-7-91',
          type: 'lottery_balance',
          value: 0.5,
          status: 'used',
          used_by: 42,
          used_at: '2026-08-31T12:00:00Z',
          created_at: '2026-08-31T10:00:00Z',
          group_id: null,
          validity_days: 0,
          notes: '鸿运锦鲤',
        },
        {
          id: 19,
          code: 'CHECKIN-2026-08-31-19',
          type: 'check_in_balance',
          value: 0.08,
          status: 'used',
          used_by: 42,
          used_at: '2026-08-31T08:30:00Z',
          created_at: '2026-08-31T08:30:00Z',
          group_id: null,
          validity_days: 0,
          notes: '2026-08-31',
        },
      ],
      total: 2,
      page: 1,
      page_size: 15,
      pages: 1,
      total_recharged: 0,
    })
  })

  it('renders lottery and check-in rewards as balance-history sources', async () => {
    const wrapper = mount(UserBalanceHistoryModal, {
      props: {
		show: false,
        user: {
          id: 42,
          email: 'winner@example.com',
          balance: 4.5,
          created_at: '2026-08-01T00:00:00Z',
        } as never,
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Select: true,
          Icon: true,
        },
      },
    })

	await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getUserBalanceHistoryMock).toHaveBeenCalledWith(42, 1, 15, undefined)
    expect(wrapper.text()).toContain('redeem.balanceAddedLottery')
    expect(wrapper.text()).toContain('+$0.50')
    expect(wrapper.text()).toContain('redeem.balanceAddedCheckIn')
    expect(wrapper.text()).toContain('+$0.08')
  })
})
