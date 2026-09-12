import { describe, expect, it } from 'vitest'
import { loadGroupModelMapping, serializeGroupModelMapping, validGroupModelMapping } from '../groupModelMapping'

describe('group model mapping', () => {
  it('loads and serializes trimmed pairs, with an explicit empty map to clear', () => {
    expect(loadGroupModelMapping({ luna: 'terra' })).toEqual([{ from: 'luna', to: 'terra' }])
    expect(serializeGroupModelMapping([{ from: ' luna ', to: ' terra ' }])).toEqual({ luna: 'terra' })
    expect(serializeGroupModelMapping([])).toEqual({})
  })
  it('rejects ambiguous or invalid mappings before saving', () => {
    expect(validGroupModelMapping([{ from: 'luna', to: 'terra' }])).toBe(true)
    expect(validGroupModelMapping([{ from: 'luna', to: 'terra' }, { from: ' luna ', to: 'sol' }])).toBe(false)
    for (const row of [{ from: '', to: 'terra' }, { from: 'luna', to: '' }, { from: 'luna*', to: 'terra' }, { from: 'lu\nna', to: 'terra' }]) {
      expect(validGroupModelMapping([row])).toBe(false)
    }
  })
})
