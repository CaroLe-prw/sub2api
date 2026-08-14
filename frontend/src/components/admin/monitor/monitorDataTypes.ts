import type { PoolMonitorAccount, PoolMonitorModel, PoolProbeResult } from '@/api/admin/schedulerProbes'

export interface MonitorModelRow {
  account: PoolMonitorAccount
  probe: PoolMonitorModel
}

export type ProbeHistoryByPlan = Record<number, PoolProbeResult[]>
