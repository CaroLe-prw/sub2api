import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import type { ApiKey } from '@/types'
import KeySchedulingStatusCell from '../KeySchedulingStatusCell.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'keys.groupRateGuardExceededList') {
        return `Current ${params?.rate}x > limit ${params?.threshold}x`
      }
      return key
    },
  }),
}))

const createApiKey = (overrides: Partial<ApiKey> = {}): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test',
  name: 'test',
  group_id: 1,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-08-07T00:00:00Z',
  updated_at: '2026-08-07T00:00:00Z',
  current_concurrency: 0,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
  ...overrides,
})

describe('KeySchedulingStatusCell', () => {
  it('shows the effective rate and threshold when the key exceeds its guard', () => {
    const wrapper = mount(KeySchedulingStatusCell, {
      props: {
        apiKey: createApiKey({
          scheduling_status: 'temporarily_unavailable',
          effective_group_rate_multiplier: 0.065,
          max_group_rate_multiplier: 0.05,
        }),
      },
    })

    expect(wrapper.text()).toContain('keys.status.temporarily_unavailable')
    expect(wrapper.get('[data-testid="group-rate-exceeded"]').text()).toBe(
      'Current 0.065x > limit 0.05x'
    )
  })

  it('does not show a range warning for an available key', () => {
    const wrapper = mount(KeySchedulingStatusCell, {
      props: {
        apiKey: createApiKey({
          scheduling_status: 'active',
          effective_group_rate_multiplier: 0.05,
          max_group_rate_multiplier: 0.08,
        }),
      },
    })

    expect(wrapper.text()).toContain('keys.status.active')
    expect(wrapper.find('[data-testid="group-rate-exceeded"]').exists()).toBe(false)
  })
})
