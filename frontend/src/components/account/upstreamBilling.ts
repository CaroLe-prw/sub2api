export type UpstreamBillingMode = 'off' | 'sub2api' | 'newapi'

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
