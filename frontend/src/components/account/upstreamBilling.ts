export type UpstreamBillingMode = 'off' | 'sub2api' | 'newapi'

// Mirrors the backend's IsUpstreamBillingProbeIdentity eligibility.
export function supportsUpstreamRateCalibration(platform: string | undefined, type: string | undefined): boolean {
  return type === 'apikey' && [
    'openai', 'anthropic', 'gemini', 'antigravity', 'grok',
    'kimi', 'zhipu', 'deepseek', 'minimax'
  ].includes(platform || '')
}

export function supportsNewAPISyncPlatform(platform: string | undefined): boolean {
  return platform === 'openai'
    || platform === 'anthropic'
    || platform === 'gemini'
    || platform === 'grok'
}

export function resolveUpstreamBillingMode(extra?: Record<string, unknown>): UpstreamBillingMode {
  if (extra?.newapi_sync_enabled === true) return 'newapi'
  if (extra?.upstream_billing_probe_enabled === true) return 'sub2api'
  return 'off'
}
