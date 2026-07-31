import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

import QuotaLimitCard from '../QuotaLimitCard.vue'

function mountCard(quotaUsageMultiplier = 1) {
  return mount(QuotaLimitCard, {
    props: {
      totalLimit: null,
      dailyLimit: 500,
      weeklyLimit: null,
      quotaUsageMultiplier,
      dailyResetMode: 'rolling',
      dailyResetHour: null,
      weeklyResetMode: 'rolling',
      weeklyResetDay: null,
      weeklyResetHour: null,
      resetTimezone: null,
    },
    global: {
      stubs: {
        QuotaDimensionRow: true,
      },
    },
  })
}

describe('QuotaLimitCard', () => {
  it('emits an independent quota usage multiplier', async () => {
    const wrapper = mountCard()
    const multiplierInput = wrapper.get('input[type="number"]')

    await multiplierInput.setValue('1.25')

    expect(wrapper.emitted('update:quotaUsageMultiplier')).toEqual([[1.25]])
  })

  it('normalizes an invalid negative multiplier to one', async () => {
    const wrapper = mountCard(0.033)
    const multiplierInput = wrapper.get('input[type="number"]')

    await multiplierInput.setValue('-1')

    expect(wrapper.emitted('update:quotaUsageMultiplier')).toEqual([[1]])
  })
})
