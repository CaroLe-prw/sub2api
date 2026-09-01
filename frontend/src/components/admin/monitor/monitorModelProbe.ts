import type { PoolMonitorModel } from '@/api/admin/schedulerProbes'

export function hasActiveProbe(model: Pick<PoolMonitorModel, 'plan_id' | 'has_probe'>): boolean {
  return model.plan_id > 0 && model.has_probe !== false
}

export function monitorModelKey(model: Pick<PoolMonitorModel, 'plan_id' | 'model'>): string {
  return model.plan_id > 0 ? `probe:${model.plan_id}` : `traffic:${model.model.trim().toLowerCase()}`
}
