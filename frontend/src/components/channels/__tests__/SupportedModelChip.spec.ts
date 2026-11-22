import { nextTick } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SupportedModelChip from '../SupportedModelChip.vue'
import { tokenPricing } from './catalog.fixtures'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('SupportedModelChip', () => {
  it.each(['availableChannels.pricing', 'admin.availableChannels.pricing'])(
    'shows video prices per second using %s translations',
    async (pricingKeyPrefix) => {
      const wrapper = mount(SupportedModelChip, {
        attachTo: document.body,
        props: {
          pricingKeyPrefix,
          model: {
            name: 'video-test',
            platform: '',
            pricing: {
              billing_mode: 'video',
              input_price: null,
              output_price: null,
              cache_write_price: null,
              cache_read_price: null,
              image_input_price: null,
              image_output_price: null,
              per_request_price: 0.05,
              intervals: [
                { tier_label: '480p', min_tokens: 0, max_tokens: null, input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0 },
                { tier_label: '720p', min_tokens: 0, max_tokens: null, input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: 0.12 }
              ]
            }
          }
        }
      })
      try {
        await wrapper.find('[tabindex="0"]').trigger('mouseenter')
        await nextTick()
        const tooltip = document.body.querySelector('[role="tooltip"]')
        expect(tooltip?.textContent).toContain(`${pricingKeyPrefix}.billingModeVideo`)
        expect(tooltip?.textContent).toContain(`${pricingKeyPrefix}.videoPrice`)
        expect(tooltip?.textContent).toContain(`$0.05 ${pricingKeyPrefix}.unitPerSecond`)
        expect(tooltip?.textContent).toContain('480p')
        expect(tooltip?.textContent).toContain(`$0 ${pricingKeyPrefix}.unitPerSecond`)
        expect(tooltip?.textContent).toContain('720p')
        expect(tooltip?.textContent).toContain(`$0.12 ${pricingKeyPrefix}.unitPerSecond`)
        const model = wrapper.props('model')
        await wrapper.setProps({
          model: { ...model, pricing: { ...model.pricing!, per_request_price: null } }
        })
        expect(tooltip?.textContent).not.toContain(`${pricingKeyPrefix}.videoPrice`)
        expect(tooltip?.textContent).toContain(`$0.12 ${pricingKeyPrefix}.unitPerSecond`)
        await wrapper.setProps({
          model: { ...model, pricing: { ...model.pricing!, per_request_price: 0 } }
        })
        expect(tooltip?.textContent).toContain(`${pricingKeyPrefix}.videoPrice`)
        expect(wrapper.findComponent({ name: 'PricingRow' }).text()).toContain(`$0 ${pricingKeyPrefix}.unitPerSecond`)
      } finally {
        wrapper.unmount()
      }
    }
  )

  it.each(['availableChannels.pricing', 'admin.availableChannels.pricing'])(
    'reads image prices from the per-request field using %s translations', async (pricingKeyPrefix) => {
      const wrapper = mount(SupportedModelChip, {
        attachTo: document.body,
        props: { pricingKeyPrefix, model: { name: 'image-test', platform: '', pricing: {
          ...tokenPricing, billing_mode: 'image', per_request_price: 0.01, image_output_price: 0.00003
        } } }
      })
      try {
        await wrapper.find('[tabindex="0"]').trigger('mouseenter')
        await nextTick()
        const tooltip = document.body.querySelector('[role="tooltip"]')
        expect(tooltip?.textContent).toContain('$0.01')
        expect(tooltip?.textContent).not.toContain('$0.00003')
        const model = wrapper.props('model')
        await wrapper.setProps({ model: { ...model, pricing: { ...model.pricing!, per_request_price: 0 } } })
        expect(wrapper.findComponent({ name: 'PricingRow' }).text()).toContain('$0 ')
        await wrapper.setProps({ model: { ...model, pricing: { ...model.pricing!, per_request_price: null } } })
        expect(wrapper.findComponent({ name: 'PricingRow' }).exists()).toBe(false)
        expect(tooltip?.textContent).not.toContain('$0.00003')
      } finally {
        wrapper.unmount()
      }
    }
  )

  it('仅配置区间倍率时按基础价展示 token 档位', async () => {
    const wrapper = mount(SupportedModelChip, {
      attachTo: document.body,
      props: {
        model: {
          name: 'gpt-test',
          platform: '',
          pricing: {
            billing_mode: 'token',
            input_price: 10e-6,
            output_price: 50e-6,
            cache_write_price: null,
            cache_read_price: null,
            image_input_price: null,
            image_output_price: null,
            per_request_price: null,
            intervals: [{
              min_tokens: 272000,
              max_tokens: null,
              input_price: null,
              output_price: null,
              cache_write_price: null,
              cache_read_price: null,
              input_multiplier: 2,
              output_multiplier: 1.5,
              per_request_price: null
            }]
          }
        },
        showPlatform: false
      }
    })

    await wrapper.find('[tabindex="0"]').trigger('mouseenter')
    await nextTick()

    expect(document.body.textContent).toContain('$20 / $75')
    wrapper.unmount()
  })
  it('shows the API time multiplier schedule without treating it as an array of prices', async () => {
    const wrapper = mount(SupportedModelChip, {
      attachTo: document.body,
      props: { model: { name: 'deepseek-test', platform: 'deepseek', pricing: {
        ...tokenPricing, time_pricing: {
          timezone: 'Asia/Shanghai', weekdays_only: true,
          periods: [{ start_time: '08:00:00', end_time: '12:00:00', multiplier: 2 }]
        }
      } } }
    })
    try {
      await wrapper.find('[tabindex="0"]').trigger('mouseenter')
      await nextTick()
      const tooltip = document.body.querySelector('[role="tooltip"]')
      expect(tooltip?.textContent).toContain('08:00:00–12:00:00')
      expect(tooltip?.textContent).toContain('×2')
      expect(tooltip?.textContent).toContain('Asia/Shanghai')
      expect(tooltip?.textContent).toContain('availableChannels.catalog.timePricingWeekdays')
    } finally {
      wrapper.unmount()
    }
  })

})
