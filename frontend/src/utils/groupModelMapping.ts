export interface GroupModelMappingRow {
  from: string
  to: string
}

export function loadGroupModelMapping(mapping?: Record<string, string> | null): GroupModelMappingRow[] {
  return Object.entries(mapping ?? {}).map(([from, to]) => ({ from, to }))
}

export function validGroupModelMapping(rows: GroupModelMappingRow[]): boolean {
  const seen = new Set<string>()
  return rows.length <= 64 && rows.every(({ from, to }) => {
    from = from.trim()
    to = to.trim()
    if (!from || !to || [...from].length > 200 || [...to].length > 200 ||
      /[\p{Cc}*]/u.test(from + to) || seen.has(from)) return false
    seen.add(from)
    return true
  })
}

export function serializeGroupModelMapping(rows: GroupModelMappingRow[]): Record<string, string> {
  return Object.fromEntries(rows.map(({ from, to }) => [from.trim(), to.trim()]))
}
