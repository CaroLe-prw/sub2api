/** Original quote denomination only: never performs foreign-exchange conversion. */
export const CHANNEL_PRICE_CURRENCIES = ['USD', 'CNY', 'EUR', 'GBP', 'JPY', 'HKD', 'SGD', 'AUD', 'CAD', 'CHF', 'KRW'] as const

export function normalizePriceCurrency(currency?: string): string {
  const code = currency?.trim().toUpperCase() || 'USD'
  return code === 'RMB' ? 'CNY' : code
}

export function priceCurrencyLabel(currency?: string): string {
  const code = normalizePriceCurrency(currency)
  return code === 'CNY' ? 'CNY / RMB' : code
}

export function priceCurrencySymbol(currency?: string): string {
  const code = normalizePriceCurrency(currency)
  const symbols: Record<string, string> = {
    USD: '$', CNY: '¥', EUR: '€', GBP: '£', JPY: 'JP¥', HKD: 'HK$',
    SGD: 'S$', AUD: 'A$', CAD: 'CA$', CHF: 'CHF ', KRW: '₩'
  }
  return symbols[code] ?? `${code} `
}
