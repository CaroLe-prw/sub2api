import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelPricingCard from '../ModelPricingCard.vue'
import { buildCatalog } from '../catalog'
import { channels, tokenPricing } from './catalog.fixtures'
import type { UserSupportedModelPricing } from '@/api/channels'

function mountCard(pricing: UserSupportedModelPricing | null = tokenPricing, currency = 'USD') {
  const entry = buildCatalog(channels())[0]
  entry.model.pricing = pricing
  entry.priceCurrency = currency
  return mount(ModelPricingCard, {
    props: { entry, showRates: false, userGroupRates: {}, timezone: 'UTC+08:00' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: {} } })] }
  })
}

describe('ModelPricingCard', () => {
  it('shows four token prices per million, preserving zero, null and small decimals', () => {
    const wrapper = mountCard({ ...tokenPricing, input_price: 0, output_price: null, cache_write_price: 0.125e-6 })
    const prices = wrapper.get('[data-testid="base-prices"]').findAll('dd')
    expect(prices).toHaveLength(4)
    expect(prices[0].text()).toContain('$0.00')
    expect(prices[1].text()).toContain('-')
    expect(prices[2].text()).toContain('$0.125')
    expect(prices[3].text()).toContain('$0.20')
  })

  it.each([
    ['per_request', 'per_request_price', 'unitPerRequest'],
    ['image', 'image_output_price', 'unitPerImage'],
    ['video', 'per_request_price', 'unitPerSecond']
  ] as const)('uses the proper %s unit without token scaling', (billing_mode, field, unit) => {
    const wrapper = mountCard({ ...tokenPricing, billing_mode, [field]: 0.05 })
    const prices = wrapper.get('[data-testid="base-prices"]')
    expect(prices.findAll('dd')).toHaveLength(1)
    expect(prices.text()).toContain('$0.05')
    expect(prices.text()).toContain(`availableChannels.pricing.${unit}`)
  })

  it('keeps duration prices, resolved tiers, time overrides and reasoning rules accessible', () => {
    const wrapper = mountCard({
      ...tokenPricing, cache_write_1h_price: 4e-6, cache_write_12h_price: 8e-6,
      reasoning_effort_multipliers: { high: 1.5 },
      intervals: [{ min_tokens: 200000, max_tokens: null, input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: null, input_multiplier: 2, output_multiplier: 1.5 }],
      time_pricing: [{ start: '10:00', end: '12:00', input_price: 0, output_price: null, cache_write_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null }]
    })
    const details = wrapper.get('details')
    expect(details.find('summary').exists()).toBe(true)
    expect(details.text()).toContain('cacheWrite1hPrice')
    expect(details.text()).toContain('cacheWrite12hPrice')
    expect(details.text()).toContain('$4.00')
    expect(details.text()).toContain('$15.00')
    expect(details.text()).toContain('10:00–12:00')
    expect(details.text()).toContain('$0.00')
    expect(details.text()).toContain('$10.00')
    expect(details.text()).toContain('high')
    expect(details.text()).toContain('×1.5')
  })

  it('uses image tier overrides and retains free video tiers', () => {
    for (const mode of ['image', 'video'] as const) {
      const wrapper = mountCard({
        ...tokenPricing, billing_mode: mode, image_output_price: 0.2, per_request_price: 0.2,
        intervals: [{ min_tokens: 0, max_tokens: null, tier_label: 'small', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0 }]
      })
      expect(wrapper.get('details').text()).toContain('small')
      expect(wrapper.get('details').text()).toContain('$0.00')
    }
  })

  it('shows missing pricing rather than inventing free prices', () => {
    const wrapper = mountCard(null)
    expect(wrapper.find('[data-testid="base-prices"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('availableChannels.noPricing')
    expect(wrapper.text()).not.toContain('$0')
  })
  it('shows original CNY prices for base, cache, tiers and time periods without FX conversion', () => {
    const wrapper = mountCard({
      ...tokenPricing, cache_write_1h_price: 3e-6,
      intervals: [{ min_tokens: 100, max_tokens: null, input_price: 7e-6, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: null }],
      time_pricing: [{ start: '09:00', end: '18:00', input_price: 9e-6, output_price: null, cache_write_price: null, cache_read_price: null, image_input_price: null, image_output_price: null, per_request_price: null }]
    }, 'CNY')
    expect(wrapper.get('[data-testid="price-currency"]').text()).toBe('CNY / RMB')
    expect(wrapper.get('[data-testid="base-prices"]').text()).toContain('¥2.00')
    expect(wrapper.text()).toContain('¥3.00')
    expect(wrapper.text()).toContain('¥7.00')
    expect(wrapper.text()).toContain('¥9.00')
    expect(wrapper.text()).not.toContain('$')
  })

  it('never merges equal numeric quotes with different denominations', () => {
    const rows = channels()
    rows[0].price_currency = 'CNY'
    const quotes = buildCatalog(rows).filter(entry => entry.model.name === 'gpt-test')
    expect(quotes).toHaveLength(2)
    expect(quotes.map(entry => entry.priceCurrency)).toEqual(['CNY', 'USD'])
    expect(quotes[0].model.pricing?.input_price).toBe(quotes[1].model.pricing?.input_price)
  })

})
