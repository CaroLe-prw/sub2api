import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { post },
}))

import { create } from '@/api/keys'

describe('API key group rate protection', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { id: 1 } })
  })

  it('is disabled by default', async () => {
    await create('default-key', 42)

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'default-key',
      group_id: 42,
    })
  })

  it('sends the configured maximum group rate multiplier', async () => {
    await create('guarded-key', 42, undefined, undefined, undefined, undefined, undefined, undefined, 0.08)

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'guarded-key',
      group_id: 42,
      max_group_rate_multiplier: 0.08,
    })
  })
})
