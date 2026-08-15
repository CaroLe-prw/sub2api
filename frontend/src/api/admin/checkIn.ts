import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export interface CheckInAdminRecord {
  id: number
  user_id: number
  email: string
  username: string
  date: string
  reward: number
  created_at: string
}

const checkInAdminAPI = {
  async listRecords(page = 1, pageSize = 20): Promise<PaginatedResponse<CheckInAdminRecord>> {
    const { data } = await apiClient.get<PaginatedResponse<CheckInAdminRecord>>('/admin/check-in/records', {
      params: { page, page_size: pageSize },
    })
    return data
  },
}

export default checkInAdminAPI
