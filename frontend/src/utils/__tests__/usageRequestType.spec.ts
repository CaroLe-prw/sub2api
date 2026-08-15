import { describe, expect, it } from 'vitest'

import {
  isUsageRequestType,
  requestTypeToLegacyStream,
  resolveUsageRequestType,
} from '../usageRequestType'

describe('usage request type', () => {
  it('recognizes system channel probes', () => {
    expect(isUsageRequestType('probe')).toBe(true)
    expect(resolveUsageRequestType({ request_type: 'probe', stream: true })).toBe('probe')
  })

  it('does not collapse probes into the legacy stream filter', () => {
    expect(requestTypeToLegacyStream('probe')).toBeNull()
  })
})
