import { apiClient } from '../client'

export interface PaymentVerificationConfig {
  enabled: boolean
  template_id: string
  lookback_days: number
  interval_minutes: number
  admin_telegram_ids: number[]
  webhook_url: string
  webhook_configured: boolean
}

export type PaymentVerificationConfigUpdate = Omit<PaymentVerificationConfig, 'webhook_configured'>
export type PaymentVerificationAction = 'ban' | 'ignore'
export type PaymentVerificationResult = 'verified' | 'unpaid' | 'amount_mismatch' | 'trade_mismatch' | 'query_error'

export interface PaymentVerificationAlert {
  id: number
  order_id: number
  user_id: number
  email: string
  provider_name: string
  out_trade_no: string
  amount: number
  pay_amount: number
  currency: string
  order_status: string
  result: PaymentVerificationResult
  reason: string
  upstream_amount: number | null
  state: 'pending' | 'ignored' | 'banned' | 'resolved'
  checked_at: string
  created_at: string
  handled_by: string
}

export interface PaymentVerificationScanResult {
  checked: number
  anomalies: number
  errors: number
}

const base = '/admin/payment/verification'

export const paymentVerificationAPI = {
  async getConfig(): Promise<PaymentVerificationConfig> {
    return (await apiClient.get<PaymentVerificationConfig>(`${base}/config`)).data
  },
  async updateConfig(config: PaymentVerificationConfigUpdate): Promise<PaymentVerificationConfig> {
    return (await apiClient.put<PaymentVerificationConfig>(`${base}/config`, config)).data
  },
  async getAlerts(limit = 50): Promise<PaymentVerificationAlert[]> {
    return (await apiClient.get<PaymentVerificationAlert[]>(`${base}/alerts`, { params: { limit } })).data
  },
  async scan(): Promise<PaymentVerificationScanResult> {
    return (await apiClient.post<PaymentVerificationScanResult>(`${base}/scan`, undefined, { timeout: 200_000 })).data
  },
  async connectTelegram(): Promise<void> {
    await apiClient.post(`${base}/telegram/connect`)
  },
  async resolve(id: number, action: PaymentVerificationAction): Promise<PaymentVerificationAlert> {
    return (await apiClient.post<PaymentVerificationAlert>(`${base}/alerts/${id}/resolve`, { action })).data
  }
}
