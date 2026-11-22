<template>
  <article class="min-w-0 overflow-hidden rounded-2xl border border-gray-200/90 bg-white dark:border-dark-700 dark:bg-dark-800" data-testid="model-price-card">
    <header class="px-4 py-4">
      <div class="mb-2 flex items-center gap-2 text-xs font-medium text-gray-500 dark:text-gray-400">
        <PlatformIcon :platform="entry.model.platform as GroupPlatform" size="xs" :class="platformIconClass(entry.model.platform)" />
        {{ platformLabel(entry.model.platform) }}
        <span class="ml-auto rounded-md bg-gray-100 px-2 py-1 font-mono text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300" data-testid="price-currency">{{ priceCurrencyLabel(entry.priceCurrency) }}</span>
      </div>
      <h3 class="break-words text-base font-bold leading-6 text-gray-950 dark:text-white">{{ entry.model.name }}</h3>
    </header>

    <dl v-if="entry.model.pricing" class="grid grid-cols-2" data-testid="base-prices">
      <div
        v-for="metric in metrics"
        :key="metric.key"
        class="min-w-0 border-t border-gray-100 px-4 py-3.5 odd:border-r dark:border-dark-700/70"
        :class="{ 'col-span-2 odd:border-r-0': metrics.length === 1 }"
      >
        <dt class="mb-1.5 text-xs font-medium text-gray-500 dark:text-gray-400">{{ priceLabel(metric.label) }}</dt>
        <dd class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <span class="font-mono text-base font-semibold tabular-nums text-gray-950 dark:text-gray-100">{{ formatScaled(metric.value, metric.scale, 2, entry.priceCurrency) }}</span>
          <span class="text-xs text-gray-400 dark:text-gray-500">{{ priceLabel(metric.unit) }}</span>
        </dd>
      </div>
    </dl>
    <p v-else class="border-t border-gray-100 px-4 py-8 text-sm text-gray-400 dark:border-dark-700">{{ t('availableChannels.noPricing') }}</p>

    <details class="group border-t border-gray-100 dark:border-dark-700/70">
      <summary class="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-2.5 text-xs leading-5 text-gray-500 transition-colors hover:bg-gray-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-teal-500 dark:text-gray-400 dark:hover:bg-dark-700/40 [&::-webkit-details-marker]:hidden">
        <span>{{ t('availableChannels.catalog.detailsHint') }}</span>
        <Icon name="chevronDown" size="xs" class="shrink-0 transition-transform group-open:rotate-180" />
      </summary>
      <div class="space-y-4 border-t border-gray-100 bg-gray-50/50 p-4 text-xs dark:border-dark-700 dark:bg-dark-900/30">
        <p class="leading-5 text-gray-500 dark:text-gray-400">{{ t('availableChannels.catalog.basePriceNote') }}</p>
        <dl v-if="extraMetrics.length" class="space-y-2">
          <div v-for="metric in extraMetrics" :key="metric.key" class="flex flex-wrap justify-between gap-2">
            <dt class="text-gray-500 dark:text-gray-400">{{ priceLabel(metric.label) }}</dt>
            <dd class="font-mono text-gray-900 dark:text-gray-100">{{ formatScaled(metric.value, metric.scale, 2, entry.priceCurrency) }} {{ priceLabel(metric.unit) }}</dd>
          </div>
        </dl>
        <section v-for="(tier, index) in tiers" :key="`tier-${index}`" class="space-y-2">
          <h4 class="font-semibold text-gray-700 dark:text-gray-200">{{ priceLabel('intervals') }} · {{ tier.label }}</h4>
          <dl class="space-y-1.5">
            <div v-for="metric in tier.metrics" :key="metric.key" class="flex flex-wrap justify-between gap-2">
              <dt class="text-gray-500 dark:text-gray-400">{{ priceLabel(metric.label) }}</dt>
              <dd class="font-mono text-gray-900 dark:text-gray-100">{{ formatScaled(metric.value, metric.scale, 2, entry.priceCurrency) }} {{ priceLabel(metric.unit) }}</dd>
            </div>
          </dl>
        </section>
        <section v-for="(period, index) in periods" :key="`period-${index}`" class="space-y-2">
          <h4 class="font-semibold text-gray-700 dark:text-gray-200">{{ priceLabel('timePricing') }} · {{ period.label }}</h4>
          <dl class="space-y-1.5">
            <div v-for="metric in period.metrics" :key="metric.key" class="flex flex-wrap justify-between gap-2">
              <dt class="text-gray-500 dark:text-gray-400">{{ priceLabel(metric.label) }}</dt>
              <dd class="font-mono text-gray-900 dark:text-gray-100">{{ formatScaled(metric.value, metric.scale, 2, entry.priceCurrency) }} {{ priceLabel(metric.unit) }}</dd>
            </div>
          </dl>
        </section>
        <section v-if="reasoningMultipliers.length" class="space-y-2">
          <h4 class="font-semibold text-gray-700 dark:text-gray-200">{{ t('availableChannels.catalog.reasoningMultipliers') }}</h4>
          <p v-for="[effort, multiplier] in reasoningMultipliers" :key="effort" class="flex justify-between gap-2 text-gray-500 dark:text-gray-400">
            <span>{{ effort }}</span><span class="font-mono">×{{ multiplier }}</span>
          </p>
        </section>
        <section v-for="(source, index) in entry.sources" :key="index" class="space-y-2">
          <h4 class="break-words font-semibold text-gray-700 dark:text-gray-200">{{ t('availableChannels.columns.name') }} · {{ source.channel }}</h4>
          <p v-if="source.description" class="break-words leading-5 text-gray-500 dark:text-gray-400">{{ source.description }}</p>
          <div v-for="group in source.groups" :key="group.id" class="space-y-1.5">
            <div class="flex flex-wrap items-center gap-2 text-gray-600 dark:text-gray-300">
              <span class="break-words">{{ group.name }}</span>
              <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ t(group.is_exclusive ? 'availableChannels.exclusive' : 'availableChannels.public') }}</span>
              <span v-if="group.subscription_type === 'subscription'" class="text-[10px] text-teal-600 dark:text-teal-400">{{ t('modelPlaza.badges.subscription') }}</span>
              <span v-if="showRates" class="font-mono text-teal-600 dark:text-teal-400">×{{ userGroupRates[group.id] ?? group.rate_multiplier }}</span>
            </div>
            <p v-if="hasPeakRate(group)" class="leading-5 text-amber-600 dark:text-amber-400">{{ t('common.peakRateTooltip', { window: formatPeakRateWindow(group, timezone) }) }}{{ t('common.peakRateImageNote') }}</p>
          </div>
        </section>
      </div>
    </details>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import type { GroupPlatform } from '@/types'
import { platformIconClass, platformLabel } from '@/utils/platformColors'
import { priceCurrencyLabel } from '@/utils/channelCurrency'
import { formatScaled, resolveIntervalPrices } from '@/utils/pricing'
import { formatPeakRateWindow, hasPeakRate } from '@/utils/peak-rate'
import { additionalPriceMetrics, priceMetrics, type CatalogModel } from './catalog'

const props = defineProps<{
  entry: CatalogModel
  showRates: boolean
  userGroupRates: Record<number, number>
  timezone: string
}>()
const { t } = useI18n()
const priceLabel = (key: string) => t(`availableChannels.pricing.${key}`)
const metrics = computed(() => props.entry.model.pricing ? priceMetrics(props.entry.model.pricing) : [])
const extraMetrics = computed(() => props.entry.model.pricing ? additionalPriceMetrics(props.entry.model.pricing) : [])
const reasoningMultipliers = computed(() => Object.entries(props.entry.model.pricing?.reasoning_effort_multipliers ?? {}))
const tiers = computed(() => {
  const pricing = props.entry.model.pricing
  return (pricing?.intervals ?? []).map((interval) => {
    const resolved = {
      ...pricing!,
      ...resolveIntervalPrices(interval, pricing!),
      per_request_price: interval.per_request_price ?? pricing!.per_request_price,
      // Image tiers share the per-request override in the API.
      image_output_price: pricing!.billing_mode === 'image'
        ? interval.per_request_price ?? pricing!.image_output_price
        : pricing!.image_output_price
    }
    return {
      label: interval.tier_label || `(${interval.min_tokens.toLocaleString()}, ${interval.max_tokens?.toLocaleString() ?? '∞'}]`,
      metrics: [...priceMetrics(resolved), ...additionalPriceMetrics(resolved)]
    }
  })
})
const periods = computed(() => {
  const pricing = props.entry.model.pricing
  return (pricing?.time_pricing ?? []).map((period) => {
    const resolved = { ...pricing! }
    for (const field of ['input_price', 'output_price', 'cache_write_price', 'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price'] as const) {
      resolved[field] = period[field] ?? pricing![field]
    }
    return { label: `${period.start}–${period.end}`, metrics: [...priceMetrics(resolved), ...additionalPriceMetrics(resolved)] }
  })
})
</script>
