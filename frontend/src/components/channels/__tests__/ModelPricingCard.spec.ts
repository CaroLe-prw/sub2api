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
    ['image', 'per_request_price', 'unitPerImage'],
    ['video', 'per_request_price', 'unitPerSecond']
  ] as const)('uses the proper %s unit without token scaling', (billing_mode, field, unit) => {
    const wrapper = mountCard({ ...tokenPricing, billing_mode, [field]: 0.05 })
    const prices = wrapper.get('[data-testid="base-prices"]')
    expect(prices.findAll('dd')).toHaveLength(1)
    expect(prices.text()).toContain('$0.05')
    expect(prices.text()).toContain(`availableChannels.pricing.${unit}`)
  })

  it.each([
    [0.01, '¥0.01'],
    [0, '¥0.00'],
    [null, '-']
  ] as const)('uses the configured per-image CNY price (%s), not the image token price', (per_request_price, expected) => {
    const wrapper = mountCard({
      ...tokenPricing, billing_mode: 'image', per_request_price, image_output_price: 0.00003
    }, 'CNY')
    const prices = wrapper.get('[data-testid="base-prices"]')
    expect(prices.findAll('dd')).toHaveLength(1)
    expect(prices.get('dd').text()).toContain(expected)
    expect(prices.text()).toContain('availableChannels.pricing.unitPerImage')
    expect(wrapper.get('[data-testid="price-currency"]').text()).toBe('CNY / RMB')
    expect(wrapper.text()).not.toContain('¥0.00003')
    expect(wrapper.text()).not.toContain('$')
  })

  it.each([
    [null, '¥0.01'],
    [0, '¥0.00'],
    [0.02, '¥0.02']
  ] as const)('resolves image tiers from the per-image price without mixing token prices (%s)', (per_request_price, expected) => {
    const wrapper = mountCard({
      ...tokenPricing, billing_mode: 'image', per_request_price: 0.01, image_output_price: 0.00003,
      intervals: [{ min_tokens: 0, max_tokens: null, tier_label: 'small', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price }]
    }, 'CNY')
    expect(wrapper.get('details').text()).toContain(expected)
    expect(wrapper.get('details').text()).not.toContain('¥0.00003')
  })

  it('still scales image output token prices per million in token billing mode', () => {
    const wrapper = mountCard({ ...tokenPricing, image_output_price: 0.00003, per_request_price: 0.01 }, 'CNY')
    const details = wrapper.get('details')
    expect(details.text()).toContain('¥30.00')
    expect(details.text()).toContain('availableChannels.pricing.unitPerMillionShort')
    expect(details.text()).not.toContain('¥0.01')
  })

  it('keeps duration prices, resolved tiers, time multipliers and reasoning rules accessible', () => {
    const wrapper = mountCard({
      ...tokenPricing, cache_write_1h_price: 4e-6, cache_write_12h_price: 8e-6,
      reasoning_effort_multipliers: { high: 1.5 },
      intervals: [{ min_tokens: 200000, max_tokens: null, input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: null, input_multiplier: 2, output_multiplier: 1.5 }],
      time_pricing: { timezone: 'Asia/Shanghai', weekdays_only: true, periods: [{ start_time: '10:00', end_time: '12:00', multiplier: 2 }] }
    })
    const details = wrapper.get('details')
    expect(details.find('summary').exists()).toBe(true)
    expect(details.text()).toContain('cacheWrite1hPrice')
    expect(details.text()).toContain('cacheWrite12hPrice')
    expect(details.text()).toContain('$4.00')
    expect(details.text()).toContain('$15.00')
    expect(details.text()).toContain('10:00–12:00')
    expect(details.text()).toContain('×2')
    expect(details.text()).toContain('Asia/Shanghai')
    expect(details.text()).toContain('availableChannels.catalog.timePricingWeekdays')
    expect(details.text()).toContain('high')
    expect(details.text()).toContain('×1.5')
  })

  it('uses image tier overrides and retains free video tiers', () => {
    for (const mode of ['image', 'video'] as const) {
      const wrapper = mountCard({
        ...tokenPricing, billing_mode: mode, image_output_price: 0.00003, per_request_price: 0.2,
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
      time_pricing: { timezone: 'Asia/Shanghai', periods: [{ start_time: '09:00', end_time: '18:00', multiplier: 2 }] }
    }, 'CNY')
    expect(wrapper.get('[data-testid="price-currency"]').text()).toBe('CNY / RMB')
    expect(wrapper.get('[data-testid="base-prices"]').text()).toContain('¥2.00')
    expect(wrapper.text()).toContain('¥3.00')
    expect(wrapper.text()).toContain('¥7.00')
    expect(wrapper.get('[data-testid="time-pricing"]').text()).toContain('×2')
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

  it.each([undefined, null, { timezone: 'UTC', periods: [] }, { timezone: 'UTC', periods: null }])(
    'keeps prices visible when the time schedule is empty: %j', (time_pricing) => {
      const wrapper = mountCard({ ...tokenPricing, time_pricing }, 'CNY')
      expect(wrapper.get('[data-testid="base-prices"]').text()).toContain('¥2.00')
      expect(wrapper.find('[data-testid="time-pricing"]').exists()).toBe(false)
    }
  )

  it('does not multiply already-effective prices by the schedule again', () => {
    const wrapper = mountCard({ ...tokenPricing, input_price: 2e-6, output_price: 8e-6, time_pricing: {
      timezone: 'America/New_York', weekdays_only: false,
      periods: [{ start_time: '08:00:00', end_time: '12:00:00', multiplier: 2 }]
    } }, 'CNY')
    const prices = wrapper.get('[data-testid="base-prices"]').findAll('dd')
    expect(prices[0].text()).toContain('¥2.00')
    expect(prices[1].text()).toContain('¥8.00')
    const schedule = wrapper.get('[data-testid="time-pricing"]')
    expect(schedule.text()).toContain('America/New_York')
    expect(schedule.text()).toContain('availableChannels.catalog.timePricingEveryDay')
    expect(schedule.text()).toContain('×2')
  })

})
