import { describe, expect, it } from 'vitest'

import { accountSchedulerScoreRows } from '../accountSchedulerScore'
import type { Account } from '@/types'

const groupedAccount = {
  group_ids: [5, 6],
  scheduler_scores: [
    { group_id: 5, group_name: 'five', base_score: 5, sticky_score: 6 },
    { group_id: 6, group_name: 'six', base_score: 7, sticky_score: 8 }
  ]
} as Account

describe('accountSchedulerScoreRows', () => {
  it('keeps only the selected group when requested', () => {
    expect(accountSchedulerScoreRows({
      account: groupedAccount,
      selectedGroupId: 6,
      onlySelectedGroup: true
    })).toEqual([expect.objectContaining({ group_id: 6, group_name: 'six' })])
  })

  it('keeps all group rows when the switch is off', () => {
    expect(accountSchedulerScoreRows({
      account: groupedAccount,
      selectedGroupId: 6,
      onlySelectedGroup: false
    })).toHaveLength(2)
  })
})
