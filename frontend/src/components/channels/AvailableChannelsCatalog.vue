<template>
  <div class="space-y-5" :aria-busy="loading">
    <section class="rounded-2xl border border-gray-200/80 bg-white p-4 sm:p-5 dark:border-dark-700 dark:bg-dark-800" :aria-label="t('availableChannels.catalog.filters')">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-start">
        <h2 class="flex shrink-0 items-center gap-2 py-2.5 text-sm font-semibold text-gray-800 sm:w-24 dark:text-gray-200">
          <Icon name="chart" size="sm" class="text-gray-400" />
          {{ t('availableChannels.columns.platform') }}
        </h2>
        <div class="flex min-w-0 flex-1 flex-wrap gap-1.5" role="group" :aria-label="t('availableChannels.columns.platform')">
          <button type="button" class="platform-filter" :class="{ active: !selectedPlatform }" :aria-pressed="!selectedPlatform" data-testid="platform-all" @click="selectedPlatform = ''">
            <Icon name="grid" size="sm" />
            {{ t('availableChannels.catalog.allPlatforms') }}
            <span class="count-badge">{{ countModels(catalog) }}</span>
          </button>
          <button
            v-for="platform in platforms"
            :key="platform.name"
            type="button"
            class="platform-filter"
            :class="{ active: selectedPlatform === platform.name }"
            :aria-pressed="selectedPlatform === platform.name"
            :data-platform="platform.name"
            @click="selectedPlatform = platform.name"
          >
            <PlatformIcon :platform="platform.name as GroupPlatform" size="sm" :class="platformIconClass(platform.name)" />
            {{ platformLabel(platform.name) }}
            <span class="count-badge">{{ platform.count }}</span>
          </button>
        </div>
      </div>

      <div class="mb-3 mt-5 flex flex-wrap items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h2 class="flex items-center gap-2 text-sm font-semibold text-gray-800 dark:text-gray-200">
            <Icon name="users" size="sm" class="text-gray-400" />
            {{ t('availableChannels.catalog.groupLabel') }}
          </h2>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('availableChannels.catalog.groupHint') }}</p>
        </div>
        <div class="flex w-full flex-wrap items-center gap-3 sm:w-auto">
          <div class="relative min-w-0 flex-1 sm:w-56 sm:flex-none">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="searchQuery" type="search" :aria-label="searchPlaceholder || t('availableChannels.searchPlaceholder')" :placeholder="searchPlaceholder || t('availableChannels.searchPlaceholder')" class="input !py-2 pl-9 !text-xs" data-testid="catalog-search" />
          </div>
          <slot name="filter-actions" />
          <div class="flex items-center gap-2">
            <label for="channel-rate-toggle" class="cursor-pointer whitespace-nowrap text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('availableChannels.catalog.showRates') }}</label>
            <Toggle id="channel-rate-toggle" v-model="showRates" :aria-label="t('availableChannels.catalog.showRates')" />
          </div>
          <button type="button" class="btn btn-secondary !p-2" :disabled="loading" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="$emit('refresh')">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          </button>
        </div>
      </div>

      <div class="group-grid max-h-72 overflow-y-auto p-0.5" role="group" :aria-label="t('availableChannels.catalog.groupLabel')">
        <button type="button" class="group-filter" :class="{ active: selectedGroupId === null }" :aria-pressed="selectedGroupId === null" data-testid="group-all" @click="selectedGroupId = null">
          <span class="flex-1 text-sm font-semibold">{{ t('availableChannels.catalog.allGroups') }}</span>
          <span class="selection-dot"><Icon v-if="selectedGroupId === null" name="check" size="xs" /></span>
        </button>
        <button
          v-for="option in groups"
          :key="option.group.id"
          type="button"
          class="group-filter"
          :class="{ active: selectedGroupId === option.group.id }"
          :aria-pressed="selectedGroupId === option.group.id"
          :data-group-id="option.group.id"
          @click="selectedGroupId = option.group.id"
        >
          <PlatformIcon :platform="option.group.platform as GroupPlatform" size="sm" :class="platformIconClass(option.group.platform)" />
          <span class="min-w-0 flex-1">
            <span class="flex items-center gap-1.5 text-sm font-semibold">
              <span class="truncate" :title="option.group.name">{{ option.group.name }}</span>
              <Icon v-if="option.group.is_exclusive" name="shield" size="xs" class="shrink-0 text-purple-500" :title="t('availableChannels.exclusiveTooltip')" />
            </span>
            <span class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-gray-400 dark:text-gray-500">
              <span>{{ t('availableChannels.catalog.modelCount', { count: option.count }) }}</span>
              <span v-if="showRates" class="font-mono text-teal-600 dark:text-teal-400" data-testid="group-rate">×{{ userGroupRates[option.group.id] ?? option.group.rate_multiplier }}</span>
            </span>
          </span>
          <span class="selection-dot"><Icon v-if="selectedGroupId === option.group.id" name="check" size="xs" /></span>
        </button>
      </div>
    </section>

    <div v-if="loading" role="status" class="flex min-h-60 items-center justify-center gap-3 text-sm text-gray-500 dark:text-gray-400" data-testid="catalog-loading">
      <Icon name="refresh" size="lg" class="animate-spin" />{{ t('common.loading') }}
    </div>
    <div v-else-if="error" role="alert" class="rounded-2xl border border-red-200 bg-white px-5 py-12 text-center text-sm text-red-600 dark:border-red-900 dark:bg-dark-800 dark:text-red-400">
      <p>{{ errorMessage || t('availableChannels.catalog.loadFailed') }}</p>
      <button type="button" class="btn btn-secondary mt-4" @click="$emit('refresh')">{{ t('common.refresh') }}</button>
    </div>
    <div v-else-if="filteredModels.length" class="grid grid-cols-1 items-start gap-3 md:grid-cols-2 xl:grid-cols-3" data-testid="model-grid">
      <template v-for="entry in filteredModels" :key="entry.key">
        <slot name="model-card" :entry="entry" :show-rates="showRates">
          <ModelPricingCard :entry="entry" :show-rates="showRates" :user-group-rates="userGroupRates" :timezone="timezone" />
        </slot>
      </template>
    </div>
    <div v-else class="rounded-2xl border border-dashed border-gray-300 bg-white/70 px-5 py-16 text-center dark:border-dark-600 dark:bg-dark-800/70" data-testid="catalog-empty">
      <Icon name="inbox" size="xl" class="mx-auto mb-3 text-gray-400" />
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ emptyLabel }}</p>
      <button v-if="hasFilters" type="button" class="mt-3 text-sm font-medium text-teal-600 hover:text-teal-700 dark:text-teal-400" @click="resetFilters">{{ t('availableChannels.catalog.resetFilters') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Toggle from '@/components/common/Toggle.vue'
import ModelPricingCard from './ModelPricingCard.vue'
import { buildCatalog, countModels, filterCatalog } from './catalog'
import type { UserAvailableChannel, UserAvailableGroup } from '@/api/channels'
import type { GroupPlatform } from '@/types'
import { platformIconClass, platformLabel } from '@/utils/platformColors'

const props = withDefaults(defineProps<{
  rows: UserAvailableChannel[]
  loading: boolean
  error?: boolean
  userGroupRates: Record<number, number>
  timezone?: string
  searchPlaceholder?: string
  errorMessage?: string
  emptyMessage?: string
}>(), { error: false, timezone: '' })
defineEmits<{ (e: 'refresh'): void }>()
const { t } = useI18n()
const selectedPlatform = ref('')
const selectedGroupId = ref<number | null>(null)
const searchQuery = ref('')
const showRates = ref(true)
const catalog = computed(() => buildCatalog(props.rows))
const platforms = computed(() => {
  const names = new Set(props.rows.flatMap((channel) => channel.platforms.map((section) => section.platform)))
  catalog.value.forEach(({ model }) => names.add(model.platform))
  return [...names].sort().map((name) => ({
    name,
    count: countModels(catalog.value.filter(({ model }) => model.platform === name))
  }))
})
const groups = computed(() => {
  const options = new Map<number, UserAvailableGroup>()
  for (const channel of props.rows) {
    for (const section of channel.platforms) {
      if (selectedPlatform.value && section.platform !== selectedPlatform.value &&
        !section.supported_models.some((model) => model.platform === selectedPlatform.value)) continue
      section.groups.forEach((group) => options.set(group.id, group))
    }
  }
  return [...options.values()].map((group) => ({
    group,
    count: countModels(filterCatalog(catalog.value, selectedPlatform.value, group.id, ''))
  }))
})
const filteredModels = computed(() => filterCatalog(catalog.value, selectedPlatform.value, selectedGroupId.value, searchQuery.value))
const hasFilters = computed(() => !!selectedPlatform.value || selectedGroupId.value !== null || !!searchQuery.value.trim())
const emptyLabel = computed(() => {
  if (!props.rows.length) return props.emptyMessage || t('availableChannels.empty')
  if (!catalog.value.length || (!searchQuery.value.trim() && selectedGroupId.value !== null)) return t('availableChannels.noModels')
  return t('availableChannels.catalog.noResults')
})
watch(platforms, (options) => {
  if (selectedPlatform.value && !options.some((option) => option.name === selectedPlatform.value)) selectedPlatform.value = ''
})
watch(selectedPlatform, () => { selectedGroupId.value = null })
watch(groups, (options) => {
  if (selectedGroupId.value !== null && !options.some(({ group }) => group.id === selectedGroupId.value)) selectedGroupId.value = null
})
function resetFilters() {
  selectedPlatform.value = ''
  selectedGroupId.value = null
  searchQuery.value = ''
}
</script>

<style scoped>
.platform-filter {
  @apply inline-flex items-center gap-2 rounded-lg px-3 py-2.5 text-xs font-semibold text-gray-500 transition-colors hover:bg-gray-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-teal-500 dark:text-gray-400 dark:hover:bg-dark-700;
}
.platform-filter.active {
  @apply bg-teal-50 text-teal-700 dark:bg-teal-500/10 dark:text-teal-300;
}
.count-badge {
  @apply rounded bg-gray-100 px-1.5 py-0.5 text-[10px] leading-none text-gray-500 dark:bg-dark-700 dark:text-gray-400;
}
.group-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 190px), 1fr));
  gap: 8px;
}
.group-filter {
  @apply flex min-h-[72px] min-w-0 items-center gap-3 rounded-xl border border-gray-200 px-3 py-3 text-left text-gray-700 transition-colors hover:border-teal-300 hover:bg-teal-50/30 focus-visible:outline focus-visible:outline-2 focus-visible:outline-teal-500 dark:border-dark-600 dark:text-gray-200 dark:hover:border-teal-700 dark:hover:bg-teal-500/5;
}
.group-filter.active {
  @apply border-teal-500 bg-teal-50 text-teal-700 dark:border-teal-500 dark:bg-teal-500/10 dark:text-teal-300;
}
.selection-dot {
  @apply flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded-full border border-gray-200 dark:border-dark-500;
}
.active .selection-dot {
  @apply border-teal-600 bg-teal-600 text-white dark:border-teal-500 dark:bg-teal-500;
}
</style>
