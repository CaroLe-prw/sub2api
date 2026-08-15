import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({ apiClient: { get, put, post } }))

import {
  getAccountModelPolicy,
  getPolicy,
  listOverview,
  listResults,
  runNow,
  updateAccountModelPolicy,
  updatePolicy,
} from '@/api/admin/schedulerProbes'

describe('admin scheduler probe API', () => {
  beforeEach(() => {
    get.mockReset().mockResolvedValue({ data: {} })
    put.mockReset().mockResolvedValue({ data: {} })
    post.mockReset().mockResolvedValue({ data: {} })
  })

  it('uses scheduler-observability routes instead of channel-monitor routes', async () => {
    await getPolicy()
    await updatePolicy({ enabled: false, whitelist: [] })
    await updatePolicy({ enabled: true, mode: 'adaptive', whitelist: [] })
    await updatePolicy({ enabled: true, mode: 'fixed', fixed_interval_minutes: 15, whitelist: [] })
    await listOverview()
    await listOverview([35, 31])
    await getAccountModelPolicy(35)
    await updateAccountModelPolicy(35, ['gpt-5.*'])
    await listResults(81, 60)
    await runNow(81)

    expect(get).toHaveBeenCalledWith('/admin/scheduler-observability/probes/policy')
    expect(put).toHaveBeenCalledWith('/admin/scheduler-observability/probes/policy', { enabled: false, whitelist: [] })
    expect(put).toHaveBeenCalledWith('/admin/scheduler-observability/probes/policy', { enabled: true, mode: 'adaptive', whitelist: [] })
    expect(put).toHaveBeenCalledWith('/admin/scheduler-observability/probes/policy', {
      enabled: true,
      mode: 'fixed',
      fixed_interval_minutes: 15,
      whitelist: [],
    })
    expect(get).toHaveBeenCalledWith('/admin/scheduler-observability/probes/overview')
    expect(get).toHaveBeenCalledWith('/admin/scheduler-observability/probes/overview', {
      params: { account_ids: '35,31' },
    })
    expect(get).toHaveBeenCalledWith('/admin/scheduler-observability/probes/accounts/35/model-policy')
    expect(put).toHaveBeenCalledWith('/admin/scheduler-observability/probes/accounts/35/model-policy', { whitelist: ['gpt-5.*'] })
    expect(get).toHaveBeenCalledWith('/admin/scheduler-observability/probes/plans/81/results', { params: { limit: 60 } })
    expect(post).toHaveBeenCalledWith('/admin/scheduler-observability/probes/plans/81/run')

    for (const call of [...get.mock.calls, ...put.mock.calls, ...post.mock.calls]) {
      expect(call[0]).not.toContain('/channel-monitors')
    }
  })
})
