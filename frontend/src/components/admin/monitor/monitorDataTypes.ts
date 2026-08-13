import type { PoolMonitorAccount, PoolMonitorModel, PoolProbeResult } from '@/api/admin/channelMonitor'

export interface MonitorModelRow {
  account: PoolMonitorAccount
  probe: PoolMonitorModel
}

export type ProbeHistoryByPlan = Record<number, PoolProbeResult[]>
