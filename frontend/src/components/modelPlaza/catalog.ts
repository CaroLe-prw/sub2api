import type { UserAvailableChannel } from '@/api/channels'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { CatalogModel } from '@/components/channels/catalog'

/** Share catalog presentation, not the authenticated channels endpoint or its billing semantics. */
export function plazaCatalogRows(groups: ModelPlazaGroup[]): UserAvailableChannel[] {
  return [...groups]
    .sort((a, b) =>
      (a.user_rate_multiplier ?? a.rate_multiplier) - (b.user_rate_multiplier ?? b.rate_multiplier) ||
      a.name.localeCompare(b.name)
    )
    .map((group) => ({
      name: group.name,
      description: group.description,
      // Model Plaza pricing and official reference prices are denominated in USD.
      price_currency: 'USD',
      platforms: [{ platform: group.platform, groups: [group], supported_models: group.models }]
    }))
}

/** Keep only this quote's visible groups/models; preserve all original billing metadata. */
export function plazaDetailGroups(entry: CatalogModel, groupsById: Map<number, ModelPlazaGroup>): ModelPlazaGroup[] {
  const ids = new Set(entry.sources.flatMap((source) => source.groups.map((group) => group.id)))
  return [...ids].flatMap((id) => {
    const group = groupsById.get(id)
    if (!group) return []
    const models = group.models.filter((model) =>
      model.name === entry.model.name &&
      (model.platform || group.platform) === entry.model.platform &&
      JSON.stringify(model.pricing) === JSON.stringify(entry.model.pricing)
    )
    return models.length ? [{ ...group, models }] : []
  })
}
