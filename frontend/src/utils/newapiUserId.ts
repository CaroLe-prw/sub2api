// Older servers and saved configurations may still return numeric UIDs.
export function normalizeNewAPIUserId(value: string | number): string {
  return value === 0 ? '' : String(value).trim()
}

export function isValidNewAPIUserId(value: string | number): boolean {
  const id = normalizeNewAPIUserId(value)
  return /^[A-Za-z0-9][A-Za-z0-9_-]{0,255}$/.test(id) && !/^0+$/.test(id)
}
