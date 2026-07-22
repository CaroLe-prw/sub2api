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
  getTelegramNotificationConfig,
  testTelegramNotification,
  updateTelegramNotificationConfig,
  type OpsTelegramNotificationConfig,
  type OpsTelegramNotificationTestRequest,
  type OpsTelegramNotificationUpdateRequest
} from '@/api/admin/ops'

const request: OpsTelegramNotificationUpdateRequest = {
  enabled: true,
  bot_token: '',
  chat_id: '-1001234567890',
  topic_id: 42,
  base_url: 'https://api.telegram.org',
  disable_notification: true,
  protect_content: false
}

const testRequest: OpsTelegramNotificationTestRequest = {
  bot_token: request.bot_token,
  chat_id: request.chat_id,
  topic_id: request.topic_id,
  base_url: request.base_url,
  disable_notification: request.disable_notification,
  protect_content: request.protect_content
}

const response: OpsTelegramNotificationConfig = {
  enabled: request.enabled,
  bot_token_configured: true,
  chat_id: request.chat_id,
  topic_id: request.topic_id,
  base_url: request.base_url,
  disable_notification: request.disable_notification,
  protect_content: request.protect_content
}

describe('admin Ops Telegram notification API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: response })
    put.mockResolvedValue({ data: response })
    post.mockResolvedValue({ data: { sent: true } })
  })

  it('uses the standalone config and test endpoints', async () => {
    await getTelegramNotificationConfig()
    await updateTelegramNotificationConfig(request)
    await testTelegramNotification(testRequest)

    expect(get).toHaveBeenCalledWith('/admin/ops/telegram-notification/config')
    expect(put).toHaveBeenCalledWith('/admin/ops/telegram-notification/config', request)
    expect(post).toHaveBeenCalledWith('/admin/ops/telegram-notification/test', testRequest)
  })
})
