import type { Account, AccountSchedulerGroupScore } from '@/types'

interface AccountSchedulerScoreRowsOptions {
  account: Account
  selectedGroupId: number | null
  onlySelectedGroup: boolean
}

export function accountSchedulerScoreRows({
  account,
  selectedGroupId,
  onlySelectedGroup
}: AccountSchedulerScoreRowsOptions): AccountSchedulerGroupScore[] {
  let groupRows = Array.isArray(account.scheduler_scores)
    ? account.scheduler_scores.filter((score) => score.group_id != null)
    : []

  if (onlySelectedGroup && selectedGroupId != null) {
    groupRows = groupRows.filter((score) => score.group_id === selectedGroupId)
  }
  if (groupRows.length) return groupRows

  const hasGroups = Boolean(
    account.group_ids?.length ||
    account.account_groups?.length ||
    account.groups?.length
  )
  if (hasGroups) return []
  if (account.scheduler_score) {
    return [{ group_id: null, ...account.scheduler_score }]
  }
  return []
}
