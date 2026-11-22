import { describe, expect, it } from 'vitest'
import { normalizePriceCurrency, priceCurrencyLabel } from '../channelCurrency'
import { formatScaled } from '../pricing'

describe('native channel currency', () => {
  it.each([
    ['USD', '$2.50'], ['CNY', '¥2.50'], ['RMB', '¥2.50'], ['EUR', '€2.50'],
    ['GBP', '£2.50'], ['JPY', 'JP¥2.50'], ['HKD', 'HK$2.50'],
    ['SGD', 'S$2.50'], ['AUD', 'A$2.50'], ['CAD', 'CA$2.50'], ['CHF', 'CHF 2.50'], ['KRW', '₩2.50']
  ])('keeps numeric amounts unchanged for %s', (currency, expected) => {
    expect(formatScaled(2.5e-6, 1_000_000, 2, currency)).toBe(expected)
    expect(formatScaled(2.5, 1, 2, currency)).toBe(expected)
    expect(formatScaled(null, 1, 2, currency)).toBe('-')
  })
  it('normalizes RMB and preserves legacy USD defaults', () => {
    expect(normalizePriceCurrency(' rmb ')).toBe('CNY')
    expect(priceCurrencyLabel('CNY')).toBe('CNY / RMB')
    expect(normalizePriceCurrency()).toBe('USD')
    expect(formatScaled(2e-6, 1_000_000)).toBe('$2')
    expect(formatScaled(0, 1, 2, 'CNY')).toBe('¥0.00')
  })
})
