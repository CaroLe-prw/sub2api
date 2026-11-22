export const CANDY_TEST_REASONING_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'] as const

export interface CandyTestSample {
  status: 'passed' | 'failed' | 'error'
  model: string
  reasoning_effort?: string
  checked_at: string
  latency_ms: number
  // Missing for old records and inconclusive API errors.
  response_text?: string
  response_truncated?: boolean
}

export interface CandyTestSnapshot {
  last_verdict?: 'passed' | 'failed'
  // Absent on legacy records. API errors neither increment nor reset the streak.
  consecutive_failures?: number
  // All attempts, including API errors. The pass ratio uses passed + failed instead.
  total: number
  passed: number
  failed: number
  errors: number
  recent: CandyTestSample[]
  schedules?: Record<string, { model: string; reasoning_effort?: string; checked_at: string }>
}
