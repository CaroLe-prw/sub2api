import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import AvailableChannelsCatalog from '../AvailableChannelsCatalog.vue'
import { buildCatalog, countModels, filterCatalog } from '../catalog'
import { channels, group, tokenPricing } from './catalog.fixtures'

function mountCatalog(rows = channels()) {
  return mount(AvailableChannelsCatalog, {
    props: { rows, loading: false, userGroupRates: { 1: 0 }, timezone: 'UTC+08:00' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] }
  })
}

describe('available channel catalog data', () => {
  it('combines identical prices, retains all sources and counts unique models', () => {
    const catalog = buildCatalog(channels())
    expect(catalog).toHaveLength(3)
    expect(catalog[0].sources.map((source) => source.channel)).toEqual(['Primary', 'Backup'])
    expect(countModels(catalog)).toBe(3)
  })

  it('does not silently collapse different prices for the same model', () => {
    const rows = channels()
    rows[1].platforms[0].supported_models[0].pricing = { ...tokenPricing, input_price: 5e-6 }
    expect(buildCatalog(rows)).toHaveLength(4)
    expect(countModels(buildCatalog(rows))).toBe(3)
  })

  it('filters the actual model and source, not all models in a matching section', () => {
    const catalog = buildCatalog(channels())
    expect(filterCatalog(catalog, '', null, 'GPT-MINI').map((entry) => entry.model.name)).toEqual(['gpt-mini'])
    expect(filterCatalog(catalog, '', null, 'low latency')).toHaveLength(2)
    expect(filterCatalog(catalog, '', null, 'Backup group')[0].sources.map((source) => source.channel)).toEqual(['Backup'])
    expect(filterCatalog(catalog, 'openai', 1, 'Backup group')).toHaveLength(0)
    expect(filterCatalog(catalog, 'openai', 3, '')[0].sources[0].groups.map((g) => g.id)).toEqual([3])
  })
})

describe('AvailableChannelsCatalog', () => {
  it('shows model cards, narrows by platform and resets the previous group', async () => {
    const wrapper = mountCatalog()
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(3)
    await wrapper.get('[data-group-id="3"]').trigger('click')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="model-price-card"]').text()).toContain('Backup')
    expect(wrapper.get('[data-testid="model-price-card"]').text()).not.toContain('Primary')
    await wrapper.get('[data-platform="anthropic"]').trigger('click')
    expect(wrapper.get('[data-testid="group-all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(1)
    expect(wrapper.get('h3').text()).toBe('claude-test')
    expect(wrapper.find('[data-group-id="1"]').exists()).toBe(false)
  })

  it('shows rates by default, including zero, and allows toggling without changing base prices', async () => {
    const wrapper = mountCatalog()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-group-id="1"] [data-testid="group-rate"]').text()).toBe('×0')
    expect(wrapper.get('[data-group-id="2"] [data-testid="group-rate"]').text()).toBe('×0.8')
    const before = wrapper.findAll('[data-testid="base-prices"]').map((item) => item.text())
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-testid="group-rate"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="base-prices"]').map((item) => item.text())).toEqual(before)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(wrapper.get('[data-group-id="1"] [data-testid="group-rate"]').text()).toBe('×0')
  })

  it('searches, offers reset on no matches and preserves unpriced models', async () => {
    const wrapper = mountCatalog()
    await wrapper.get('input').setValue('gpt-mini')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="model-price-card"]').text()).toContain('availableChannels.noPricing')
    await wrapper.get('input').setValue('not-a-model')
    expect(wrapper.find('[data-testid="catalog-empty"]').exists()).toBe(true)
    await wrapper.get('[data-testid="catalog-empty"] button').trigger('click')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(3)
  })

  it('retains empty groups and recovers stale selections on refresh', async () => {
    const rows = channels()
    rows.push({ name: 'Empty', description: '', platforms: [{ platform: 'gemini', groups: [group(5, 'Empty group', 'gemini')], supported_models: [] }] })
    const wrapper = mountCatalog(rows)
    await wrapper.get('[data-group-id="5"]').trigger('click')
    expect(wrapper.get('[data-testid="catalog-empty"]').text()).toContain('availableChannels.noModels')
    await wrapper.setProps({ rows: channels() })
    expect(wrapper.get('[data-testid="group-all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(3)
  })

  it('renders loading, error and empty states and emits refresh', async () => {
    const wrapper = mountCatalog([])
    expect(wrapper.get('[data-testid="catalog-empty"]').text()).toContain('availableChannels.empty')
    await wrapper.setProps({ loading: true })
    expect(wrapper.find('[data-testid="catalog-loading"]').exists()).toBe(true)
    await wrapper.setProps({ loading: false, error: true })
    expect(wrapper.get('[role="alert"]').text()).toContain('availableChannels.catalog.loadFailed')
    await wrapper.get('[role="alert"] button').trigger('click')
    expect(wrapper.emitted('refresh')).toHaveLength(1)
  })
})
