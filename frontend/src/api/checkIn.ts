import { apiClient } from './client'

export interface CheckInRecord {
  date: string
  reward: number
  created_at: string
}

export interface CheckInOverview {
  today: string
  timezone: string
  year: number
  month: number
  checked_in_today: boolean
  today_reward: number
  current_streak: number
  total_days: number
  month_days: number
  month_reward: number
  total_reward: number
  balance: number
  reward_min: number
  reward_max: number
  records: CheckInRecord[]
}

export interface CheckInClaimResult {
  created: boolean
  record: CheckInRecord
  balance: number
}

export const checkInAPI = {
  async getOverview(year?: number, month?: number): Promise<CheckInOverview> {
    const { data } = await apiClient.get<CheckInOverview>('/user/check-in', {
      params: { year, month },
    })
    return data
  },

  async claim(): Promise<CheckInClaimResult> {
    const { data } = await apiClient.post<CheckInClaimResult>('/user/check-in')
    return data
  },
}
