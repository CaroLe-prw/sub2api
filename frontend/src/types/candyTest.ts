export interface CandyTestSample {
  status: 'passed' | 'failed' | 'error'
  model: string
  checked_at: string
  latency_ms: number
}

export interface CandyTestSnapshot {
  total: number
  passed: number
  failed: number
  errors: number
  recent: CandyTestSample[]
  schedules?: Record<string, { model: string; checked_at: string }>
}
