import { apiClient } from '../client'

export interface QQBotConfig {
  enabled: boolean
  app_id: string
  groups: string[]
  admins: string[]
  monitor_ids: number[]
  group_ids: number[]
  allow_probe: boolean
  allow_unmentioned: boolean
  moderation: { enabled: boolean; observe_only: boolean; scan_images: boolean; trusted_members: string[] }
}

export interface QQModerationRecord {
  at: string
  group: string
  member: string
  message_id: string
  action: 'recorded' | 'recalled' | 'failed'
  verdict: string
  reason: string
  evidence?: string[]
}

export interface QQBotView extends QQBotConfig {
	image_ocr_available?: boolean
  secret_configured: boolean
  monitor_mode: 'v1' | 'v2'
  status: { state: string; detail: string; updated_at: string }
}

export const qqBotAPI = {
  async moderationRecords(): Promise<QQModerationRecord[]> {
    const { data } = await apiClient.get<QQModerationRecord[]>('/admin/qq-bot/moderation-records')
    return data
  },
  async preview(page = 1): Promise<{ image: Blob; pages: number }> {
    const response = await apiClient.get<Blob>('/admin/qq-bot/preview', { params: { page }, responseType: 'blob', timeout: 60000 })
    return { image: response.data, pages: Number(response.headers['x-qq-board-pages'] || 1) }
  },
  async get(): Promise<QQBotView> {
    const { data } = await apiClient.get<QQBotView>('/admin/qq-bot')
    return data
  },
  async update(config: QQBotConfig & { app_secret?: string; clear_secret?: boolean }): Promise<QQBotView> {
    const { data } = await apiClient.put<QQBotView>('/admin/qq-bot', config)
    return data
  }
}
