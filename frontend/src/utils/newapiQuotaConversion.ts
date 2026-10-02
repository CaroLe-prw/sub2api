export function isValidNewAPIQuotaPerUSD(value: number | null | undefined): boolean {
  return value == null || (
    Number.isFinite(value)
    && value > 0
    && value <= Number.MAX_SAFE_INTEGER
  )
}
