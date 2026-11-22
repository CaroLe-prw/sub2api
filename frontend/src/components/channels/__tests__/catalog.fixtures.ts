import type { UserAvailableChannel, UserAvailableGroup, UserSupportedModelPricing } from '@/api/channels'

export const tokenPricing: UserSupportedModelPricing = {
  billing_mode: 'token', input_price: 2e-6, output_price: 10e-6,
  cache_write_price: 2.5e-6, cache_read_price: 0.2e-6,
  image_input_price: null, image_output_price: null, per_request_price: null, intervals: []
}
export function group(id: number, name: string, platform = 'openai'): UserAvailableGroup {
  return {
    id, name, platform, subscription_type: 'standard', rate_multiplier: 0.8,
    peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1, is_exclusive: false
  }
}
export function channels(): UserAvailableChannel[] {
  return [
    {
      name: 'Primary', description: 'Low latency', platforms: [{
        platform: 'openai', groups: [group(1, 'Standard'), group(2, 'Exclusive')],
        supported_models: [
          { name: 'gpt-test', platform: 'openai', pricing: { ...tokenPricing } },
          { name: 'gpt-mini', platform: 'openai', pricing: null }
        ]
      }]
    },
    {
      name: 'Backup', description: 'Backup access', platforms: [{
        platform: 'openai', groups: [group(3, 'Backup group')],
        supported_models: [{ name: 'gpt-test', platform: 'openai', pricing: { ...tokenPricing } }]
      }]
    },
    {
      name: 'Premium', description: '', platforms: [{
        platform: 'anthropic', groups: [group(4, 'Claude', 'anthropic')],
        supported_models: [{ name: 'claude-test', platform: 'anthropic', pricing: { ...tokenPricing, input_price: 3e-6 } }]
      }]
    }
  ]
}
