import { normalizePriceCurrency } from '@/utils/channelCurrency'
import type {
  UserAvailableChannel,
  UserAvailableGroup,
  UserSupportedModel,
  UserSupportedModelPricing
} from '@/api/channels'

export interface CatalogSource {
  channel: string
  description: string
  groups: UserAvailableGroup[]
}

export interface CatalogModel {
  key: string
  model: UserSupportedModel
  priceCurrency: string
  sources: CatalogSource[]
}

/** Only combine identical quotes; the same model can have different channel prices. */
export function buildCatalog(channels: UserAvailableChannel[]): CatalogModel[] {
  const models = new Map<string, CatalogModel>()
  for (const channel of channels) {
    const priceCurrency = normalizePriceCurrency(channel.price_currency)
    for (const section of channel.platforms) {
      for (const supported of section.supported_models) {
        const model = { ...supported, platform: supported.platform || section.platform }
        const key = JSON.stringify([model.platform, model.name, priceCurrency, model.pricing])
        const source = { channel: channel.name, description: channel.description, groups: section.groups }
        const existing = models.get(key)
        if (existing) existing.sources.push(source)
        else models.set(key, { key, model, priceCurrency, sources: [source] })
      }
    }
  }
  return [...models.values()]
}

export function filterCatalog(
  models: CatalogModel[],
  platform: string,
  groupId: number | null,
  search: string
): CatalogModel[] {
  const query = search.trim().toLowerCase()
  return models.flatMap((entry) => {
    if (platform && entry.model.platform !== platform) return []
    const modelMatches = [entry.model.name, entry.model.platform].some((v) => v.toLowerCase().includes(query))
    const sources = entry.sources.flatMap((source) => {
      const groups = groupId === null ? source.groups : source.groups.filter((g) => g.id === groupId)
      if (groupId !== null && groups.length === 0) return []
      const sourceMatches = [source.channel, source.description, ...groups.map((g) => g.name)]
        .some((v) => v.toLowerCase().includes(query))
      return !query || modelMatches || sourceMatches ? [{ ...source, groups }] : []
    })
    return sources.length ? [{ ...entry, sources }] : []
  })
}

export function countModels(models: CatalogModel[]): number {
  return new Set(models.map(({ model }) => JSON.stringify([model.platform, model.name]))).size
}

export interface PriceMetric {
  key: string
  label: string
  value: number | null
  scale: number
  unit: string
}

/** API token prices are per token; request/image/video prices are already per unit. */
export function priceMetrics(pricing: UserSupportedModelPricing): PriceMetric[] {
  const metric = (key: string, label: string, value: number | null, scale = 1_000_000, unit = 'unitPerMillionShort') =>
    ({ key, label, value, scale, unit })
  switch (pricing.billing_mode) {
    case 'per_request':
      return [metric('request', 'perRequestPrice', pricing.per_request_price, 1, 'unitPerRequest')]
    case 'image':
      return [metric('image', 'imageOutputPrice', pricing.image_output_price, 1, 'unitPerImage')]
    case 'video':
      return [metric('video', 'videoPrice', pricing.per_request_price, 1, 'unitPerSecond')]
    default:
      return [
        metric('input', 'inputPrice', pricing.input_price),
        metric('output', 'outputPrice', pricing.output_price),
        metric('write', 'cacheWriteShort', pricing.cache_write_price),
        metric('read', 'cacheReadShort', pricing.cache_read_price)
      ]
  }
}

export function additionalPriceMetrics(pricing: UserSupportedModelPricing): PriceMetric[] {
  if (pricing.billing_mode !== 'token') return []
  return ([
    ['cache_write_1h_price', 'cacheWrite1hPrice'],
    ['cache_write_12h_price', 'cacheWrite12hPrice'],
    ['image_input_price', 'imageInputPrice'],
    ['image_output_price', 'imageOutputPrice']
  ] as const).flatMap(([key, label]) => {
    const value = pricing[key]
    return value == null ? [] : [{ key, label, value, scale: 1_000_000, unit: 'unitPerMillionShort' }]
  })
}
