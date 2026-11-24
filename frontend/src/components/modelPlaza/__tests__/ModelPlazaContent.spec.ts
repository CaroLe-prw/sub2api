import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaGroupSection from '../PlazaGroupSection.vue'
import AvailableChannelsCatalog from '@/components/channels/AvailableChannelsCatalog.vue'
import { buildCatalog, filterCatalog } from '@/components/channels/catalog'
import { plazaCatalogRows, plazaDetailGroups } from '../catalog'
import { group, tokenPricing } from '@/components/channels/__tests__/catalog.fixtures'
import type { ModelPlazaGroup, ModelPlazaResponse, PlazaModel } from '@/api/modelPlaza'

const auth = vi.hoisted(() => ({ isAuthenticated: false }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { server_utc_offset: 8 } }) }))

function model(name = 'gpt-test', platform = 'openai'): PlazaModel {
  return {
    name, platform, pricing: { ...tokenPricing },
    official_pricing: { input_price: 4e-6, output_price: 8e-6, cache_write_price: null, cache_read_price: null },
    long_context_basis: 'marginal',
    time_pricing: { timezone: 'Asia/Shanghai', weekdays_only: true, periods: [{ start_time: '08:00', end_time: '12:00', multiplier: 2 }] }
  }
}

function plazaGroup(id: number, overrides: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return {
    ...group(id, `Group ${id}`), description: `Description ${id}`,
    image_rate_independent: true, image_rate_multiplier: 0.5,
    video_rate_independent: true, video_rate_multiplier: 0,
    long_context_pricing_enabled: false, models: [model()], ...overrides
  }
}

function response(): ModelPlazaResponse {
  return {
    description: '**Pricing notes**',
    groups: [
      plazaGroup(1, { user_rate_multiplier: 0, is_exclusive: true }),
      plazaGroup(2, { rate_multiplier: 0.5, models: [model(), { ...model('gpt-unpriced'), pricing: null }] }),
      plazaGroup(3, { platform: 'anthropic', models: [model('claude-test', 'anthropic')] })
    ]
  }
}

function mountPlaza(data = response(), embedded = true) {
  return mount(ModelPlazaContent, {
    props: { response: data, loading: false, embedded },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })],
      stubs: { PlazaGroupSection: true }
    }
  })
}

async function openFirstCard(wrapper: ReturnType<typeof mountPlaza>) {
  const details = wrapper.findAll('[data-testid="model-price-card"] details')[0]
  ;(details.element as HTMLDetailsElement).open = true
  await details.trigger('toggle')
}

describe('Model Plaza catalog adapter', () => {
  it('combines identical base quotes without losing custom rates or billing metadata', () => {
    const data = response()
    const rows = plazaCatalogRows(data.groups)
    const catalog = buildCatalog(rows)
    expect(catalog).toHaveLength(3)
    const details = plazaDetailGroups(catalog[0], new Map(data.groups.map((g) => [g.id, g])))
    expect(details.map((g) => g.id)).toEqual([1, 2])
    expect(details[0].user_rate_multiplier).toBe(0)
    expect(details[0].models[0]).toBe(data.groups[0].models[0])
    expect(details[1].models).toHaveLength(1)
    expect(details[1]).toMatchObject({ image_rate_multiplier: 0.5, video_rate_multiplier: 0, long_context_pricing_enabled: false })
    expect(data.groups[1].models).toHaveLength(2)
  })

  it('preserves different quotes, filters detail sources and uses the actual model platform', () => {
    const data = response()
    data.groups[1].models[0].pricing = { ...tokenPricing, input_price: 7e-6 }
    data.groups[1].models.push(model('gemini-test', 'gemini'))
    const catalog = buildCatalog(plazaCatalogRows(data.groups))
    expect(catalog).toHaveLength(5)
    const entries = filterCatalog(catalog, 'gemini', 2, '')
    expect(entries).toHaveLength(1)
    const details = plazaDetailGroups(entries[0], new Map(data.groups.map((g) => [g.id, g])))
    expect(details).toHaveLength(1)
    expect(details[0].models.map((m) => m.name)).toEqual(['gemini-test'])
  })
})

describe('ModelPlazaContent shared channel styling', () => {
  it('renders the shared platform, group and model cards with zero personal rates', async () => {
    const wrapper = mountPlaza()
    expect(wrapper.findComponent(AvailableChannelsCatalog).exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="platform-all"] .count-badge').text()).toBe('3')
    expect(wrapper.get('[data-group-id="1"] [data-testid="group-rate"]').text()).toBe('×0')
    expect(wrapper.get('[data-testid="model-price-card"] [data-testid="price-currency"]').text()).toBe('USD')
    expect(wrapper.get('[data-testid="model-price-card"] summary').text()).toContain('modelPlaza.catalog.detailsHint')
    expect(wrapper.findComponent(PlazaGroupSection).exists()).toBe(false)
    await openFirstCard(wrapper)
    const sections = wrapper.findAllComponents(PlazaGroupSection)
    expect(sections).toHaveLength(2)
    expect(sections[0].props('group').models[0].official_pricing).toEqual(response().groups[0].models[0].official_pricing)
    expect(sections[0].props('group').models[0].time_pricing?.weekdays_only).toBe(true)
  })

  it('filters platforms and groups and keeps detail prices limited to visible groups', async () => {
    const wrapper = mountPlaza()
    await wrapper.get('[data-group-id="2"]').trigger('click')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(2)
    await openFirstCard(wrapper)
    expect(wrapper.findAllComponents(PlazaGroupSection).map((section) => section.props('group').id)).toEqual([2])
    await wrapper.get('[data-platform="anthropic"]').trigger('click')
    expect(wrapper.get('[data-testid="group-all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="model-price-card"] h3').text()).toBe('claude-test')
  })

  it('retains rate filtering, including zero, and resets stale rates after refresh', async () => {
    const wrapper = mountPlaza()
    await wrapper.get('[data-testid="plaza-rate-filter"]').setValue(0)
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(1)
    expect(wrapper.find('[data-group-id="2"]').exists()).toBe(false)
    const refreshed = response()
    refreshed.groups[0].user_rate_multiplier = 0.25
    await wrapper.setProps({ response: refreshed })
    expect((wrapper.get('[data-testid="plaza-rate-filter"]').element as HTMLSelectElement).value).toBe('all')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(3)
  })

  it('searches models and groups, preserves unpriced models and toggles rate badges', async () => {
    const wrapper = mountPlaza()
    await wrapper.get('[data-testid="catalog-search"]').setValue('Group 2')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(2)
    await openFirstCard(wrapper)
    expect(wrapper.findAllComponents(PlazaGroupSection).map((section) => section.props('group').id)).toEqual([2])
    await wrapper.get('[data-testid="catalog-search"]').setValue('gpt-unpriced')
    expect(wrapper.get('[data-testid="model-price-card"]').text()).toContain('availableChannels.noPricing')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.find('[data-testid="group-rate"]').exists()).toBe(false)
    await wrapper.get('[data-testid="catalog-search"]').setValue('missing-model')
    expect(wrapper.find('[data-testid="catalog-empty"]').exists()).toBe(true)
    await wrapper.get('[data-testid="catalog-empty"] button').trigger('click')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(3)
  })

  it('retains public and embedded headings, sanitizes descriptions and uses plaza states', async () => {
    auth.isAuthenticated = false
    const wrapper = mountPlaza({ description: '<img src=x onerror="alert(1)">', groups: [] }, false)
    expect(wrapper.find('h1').exists()).toBe(true)
    expect(wrapper.text()).toContain('modelPlaza.anonymousHint')
    expect(wrapper.html()).not.toContain('onerror')
    expect(wrapper.get('[data-testid="catalog-empty"]').text()).toContain('modelPlaza.empty')
    await wrapper.setProps({ embedded: true, loading: true })
    expect(wrapper.find('h1').exists()).toBe(false)
    expect(wrapper.find('[data-testid="catalog-loading"]').exists()).toBe(true)
    await wrapper.setProps({ loading: false, error: true })
    expect(wrapper.get('[role="alert"]').text()).toContain('modelPlaza.loadFailed')
    await wrapper.get('[role="alert"] button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
    wrapper.unmount()
    auth.isAuthenticated = true
    const signedIn = mountPlaza()
    expect(signedIn.text()).not.toContain('modelPlaza.anonymousHint')
    signedIn.unmount()
    auth.isAuthenticated = false
  })
})
