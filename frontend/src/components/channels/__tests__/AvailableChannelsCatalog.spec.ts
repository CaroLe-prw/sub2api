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

  it('renders configured CNY prices when a group has multiplier-based time pricing', async () => {
    // Match the real API shape: time_pricing is a schedule, not a price array.
    const schedule = { timezone: 'Asia/Shanghai', weekdays_only: false, periods: [
      { start_time: '08:00:00', end_time: '12:00:00', multiplier: 2 },
      { start_time: '14:00:00', end_time: '18:00:00', multiplier: 2 }
    ] }
    const flashPricing = { ...tokenPricing, input_price: 1e-6, output_price: 4e-6, cache_write_price: null, cache_read_price: 0.02e-6, time_pricing: schedule }
    const rows = [{ name: 'DeepSeek channel', description: '', price_currency: 'CNY', platforms: [{
      platform: 'deepseek',
      groups: [{ ...group(8, 'deepseek-008', 'deepseek'), rate_multiplier: 0.08 }, { ...group(35, 'deepseek-035', 'deepseek'), rate_multiplier: 0.35 }],
      supported_models: [
        ...['deepseek-v4-flash', 'deepseek-v4-flash-vision-exp', 'deepseek-v4.1-flash'].map(name => ({ name, platform: 'deepseek', pricing: flashPricing })),
        ...['deepseek-v4-pro', 'deepseek-v4-pro-0813'].map(name => ({ name, platform: 'deepseek', pricing: null }))
      ]
    }] }]
    const wrapper = mountCatalog(rows)
    await wrapper.get('[data-group-id="8"]').trigger('click')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(5)
    const card = wrapper.findAll('[data-testid="model-price-card"]')[0]
    expect(card.text()).toContain('deepseek-v4-flash')
    expect(card.get('[data-testid="base-prices"]').text()).toContain('¥1.00')
    expect(card.get('[data-testid="base-prices"]').text()).toContain('¥4.00')
    expect(card.get('[data-testid="base-prices"]').text()).toContain('¥0.02')
    expect(card.get('[data-testid="price-currency"]').text()).toBe('CNY / RMB')
    expect(card.text()).toContain('08:00:00–12:00:00')
    expect(card.text()).toContain('14:00:00–18:00:00')
    expect(card.text()).toContain('×2')
    expect(card.text()).toContain('Asia/Shanghai')
    await wrapper.get('[data-group-id="35"]').trigger('click')
    expect(wrapper.findAll('[data-testid="model-price-card"]')).toHaveLength(5)
    expect(wrapper.get('[data-testid="model-grid"]').text()).toContain('¥1.00')
  })

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
