export interface ResponseModelMappingRow {
  from: string
  to: string
}

export function supportsResponseModelMapping(platform: string, type: string): boolean {
  return type === 'apikey' && ['openai', 'anthropic', 'grok', 'kimi', 'zhipu', 'deepseek'].includes(platform)
}

export function loadResponseModelMapping(raw: unknown): ResponseModelMappingRow[] {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return []
  return Object.entries(raw)
    .filter((entry): entry is [string, string] => typeof entry[1] === 'string')
    .map(([from, to]) => ({ from, to }))
}

export function validResponseModelMapping(rows: ResponseModelMappingRow[]): boolean {
  const seen = new Set<string>()
  return rows.length <= 64 && rows.every(({ from, to }) => {
    from = from.trim()
    to = to.trim()
    if (!from || !to || [...from].length > 200 || [...to].length > 200 ||
      /\p{Cc}/u.test(from + to) || seen.has(from)) return false
    seen.add(from)
    return true
  })
}

export function applyResponseModelMapping(credentials: Record<string, unknown>, rows: ResponseModelMappingRow[]) {
  // Explicit empty object also clears the mapping through JSONB merge updates.
  credentials.response_model_mapping = Object.fromEntries(rows.map(({ from, to }) => [from.trim(), to.trim()]))
}
