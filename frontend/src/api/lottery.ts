import { apiClient } from './client'

export interface LotteryPrize {
  tier: number
  name: string
  reward: number
  weight: number
}

export interface LotteryRound {
  id: number
  starts_at: string
  ends_at: string
  settled_at?: string | null
  created_at: string
  participant_count: number
  status: 'scheduled' | 'open' | 'paused' | 'drawing' | 'settled'
  prizes: LotteryPrize[]
}

export interface LotteryEntry {
  id: number
  round_id: number
  entered_at: string
  round_starts_at: string
  round_ends_at: string
  prize_tier?: number | null
  prize_name: string
  reward: number
  balance_after?: number | null
  settled_at?: string | null
  cancelled_at?: string | null
}

export interface LotteryOverview {
  enabled: boolean
  server_time: string
  timezone: string
  current_round?: LotteryRound | null
  my_entries: LotteryEntry[]
}

export interface LotteryEnterResult {
  created: boolean
  round: LotteryRound
  entry: LotteryEntry
}

export const lotteryAPI = {
  async getOverview(): Promise<LotteryOverview> {
    const { data } = await apiClient.get<LotteryOverview>('/user/lottery')
    return data
  },

  async enter(): Promise<LotteryEnterResult> {
    const { data } = await apiClient.post<LotteryEnterResult>('/user/lottery/entries')
    return data
  },
}
