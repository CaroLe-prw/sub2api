import { apiClient } from '../client'
import type { LotteryPrize, LotteryRound } from '../lottery'
import type { PaginatedResponse } from '@/types'

export interface LotteryAdminConfig {
  enabled: boolean
  server_time: string
  timezone: string
  current_round?: LotteryRound | null
  defaults: LotteryPrize[]
}

export interface UpdateLotteryConfigRequest {
  enabled: boolean
  starts_at: string
  ends_at: string
  first_prize_reward: number
  first_prize_weight: number
  second_prize_reward: number
  second_prize_weight: number
  third_prize_reward: number
  third_prize_weight: number
}

export interface LotteryAdminResult {
  entry_id: number
  round_id: number
  user_id: number
  email: string
  username: string
  entered_at: string
  settled_at?: string | null
  prize_tier: number
  prize_name: string
  reward: number
  balance_after: number
}

const lotteryAdminAPI = {
  async getConfig(): Promise<LotteryAdminConfig> {
    const { data } = await apiClient.get<LotteryAdminConfig>('/admin/lottery/config')
    return data
  },

  async updateConfig(payload: UpdateLotteryConfigRequest): Promise<LotteryAdminConfig> {
    const { data } = await apiClient.put<LotteryAdminConfig>('/admin/lottery/config', payload)
    return data
  },

  async listResults(page = 1, pageSize = 20): Promise<PaginatedResponse<LotteryAdminResult>> {
    const { data } = await apiClient.get<PaginatedResponse<LotteryAdminResult>>('/admin/lottery/results', {
      params: { page, page_size: pageSize },
    })
    return data
  },
}

export default lotteryAdminAPI
