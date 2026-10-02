import { describe, expect, it } from 'vitest'
import { isValidNewAPIQuotaPerUSD } from '../newapiQuotaConversion'

describe('NewAPI fallback quota conversion', () => {
  it.each([undefined, null, 500000, 0.5, Number.MAX_SAFE_INTEGER])('accepts %s', (value) => {
    expect(isValidNewAPIQuotaPerUSD(value)).toBe(true)
  })

  it.each([0, -1, Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY, Number.MAX_SAFE_INTEGER + 1])('rejects %s', (value) => {
    expect(isValidNewAPIQuotaPerUSD(value)).toBe(false)
  })
})
