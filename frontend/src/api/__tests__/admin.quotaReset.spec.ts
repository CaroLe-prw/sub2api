import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, post }
}))

import {
  QUOTA_RESET_RUN_TIMEOUT_MS,
  get as getSettings,
  run,
  update
} from '@/api/admin/quotaReset'

const config = {
  enabled: true,
  interval_hours: 12,
  group_ids: [7, 8],
  daily: true,
  weekly: false,
  monthly: true,
  window_start_mode: 'natural_day' as const
}

const runRequest = {
  group_ids: [9],
  daily: false,
  weekly: true,
  monthly: true,
  window_start_mode: 'preserve' as const,
  restart_schedule: false
}

describe('admin subscription quota reset API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
    const data = { config, state: { status: 'idle' } }
    get.mockResolvedValue({ data })
    put.mockResolvedValue({ data })
    post.mockResolvedValue({ data })
  })

  it('uses the standalone settings endpoints and flat config schema', async () => {
    await getSettings()
    await update(config)

    expect(get).toHaveBeenCalledWith('/admin/subscription-quota-reset')
    expect(put).toHaveBeenCalledWith('/admin/subscription-quota-reset', config)
  })

  it('allows the synchronous run request to outlive the global timeout', async () => {
    await run(runRequest)

    expect(post).toHaveBeenCalledWith(
      '/admin/subscription-quota-reset/run',
      runRequest,
      { timeout: QUOTA_RESET_RUN_TIMEOUT_MS }
    )
  })
})
