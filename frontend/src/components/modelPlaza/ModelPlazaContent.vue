<template>
  <div class="space-y-5">
    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <div v-if="!embedded">
      <h1 class="text-2xl font-bold tracking-tight text-gray-900 dark:text-white sm:text-3xl">{{ t('modelPlaza.title') }}</h1>
      <p class="mt-1.5 text-sm text-gray-500 dark:text-dark-400">{{ t('modelPlaza.description') }}</p>
    </div>

    <!-- 全局价格说明(管理员配置,Markdown) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-2xl border border-gray-200/80 bg-white px-5 py-4 text-sm dark:border-dark-700 dark:bg-dark-800"
      v-html="descriptionHtml"
    ></div>

    <!-- 未登录提示 -->
    <p
      v-if="!isAuthenticated"
      class="flex items-center gap-1.5 text-xs text-gray-400 dark:text-dark-500"
    >
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <!-- 与可用渠道共用目录外观；广场仍使用公开接口和自己的价格明细。 -->
    <AvailableChannelsCatalog
      :rows="catalogRows"
      :loading="loading"
      :error="error"
      :user-group-rates="userGroupRates"
      :timezone="timezone"
      :search-placeholder="t('modelPlaza.catalog.searchPlaceholder')"
      :error-message="t('modelPlaza.loadFailed')"
      :empty-message="t('modelPlaza.empty')"
      @refresh="$emit('refresh')"
    >
      <template #filter-actions>
        <select
          v-model="selectedRate"
          :aria-label="t('modelPlaza.filters.rateLabel')"
          class="input !w-auto !py-2 !text-xs"
          data-testid="plaza-rate-filter"
        >
          <option value="all">{{ t('modelPlaza.filters.rateLabel') }} · {{ t('modelPlaza.filters.all') }}</option>
          <option v-for="rate in rates" :key="rate" :value="rate">×{{ rate }}</option>
        </select>
      </template>
      <template #model-card="{ entry, showRates }">
        <ModelPricingCard
          :entry="entry"
          :show-rates="showRates"
          :user-group-rates="userGroupRates"
          :timezone="timezone"
          :details-hint="t('modelPlaza.catalog.detailsHint')"
        >
          <template #details="{ open }">
            <p class="leading-5 text-gray-500 dark:text-gray-400">{{ t('modelPlaza.catalog.basePriceNote') }}</p>
            <template v-if="open">
              <PlazaGroupSection
                v-for="group in detailGroups(entry)"
                :key="group.id"
                :group="group"
              />
            </template>
          </template>
        </ModelPricingCard>
      </template>
    </AvailableChannelsCatalog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import AvailableChannelsCatalog from '@/components/channels/AvailableChannelsCatalog.vue'
import ModelPricingCard from '@/components/channels/ModelPricingCard.vue'
import type { CatalogModel } from '@/components/channels/catalog'
import { plazaCatalogRows, plazaDetailGroups } from './catalog'
import { serverTimezoneLabel } from '@/utils/peak-rate'
import { useAppStore } from '@/stores/app'
import PlazaGroupSection from './PlazaGroupSection.vue'
import type { ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

defineEmits<{ (e: 'refresh'): void }>()

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedRate = ref<number | 'all'>('all')
const timezone = computed(() => serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

const groups = computed(() => props.response?.groups ?? [])
const groupsById = computed(() => new Map(groups.value.map((group) => [group.id, group])))
const userGroupRates = computed(() => Object.fromEntries(
  groups.value.flatMap((group) => group.user_rate_multiplier == null ? [] : [[group.id, group.user_rate_multiplier]])
))
const rates = computed(() => [...new Set(groups.value.map(
  (group) => group.user_rate_multiplier ?? group.rate_multiplier
))].sort((a, b) => a - b))

const catalogRows = computed(() => plazaCatalogRows(groups.value.filter(
  (group) => selectedRate.value === 'all' || (group.user_rate_multiplier ?? group.rate_multiplier) === selectedRate.value
)))

function detailGroups(entry: CatalogModel) {
  return plazaDetailGroups(entry, groupsById.value)
}

// 刷新后专属倍率可能改变，清除已经不存在的筛选值。
watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) selectedRate.value = 'all'
})
</script>

<style scoped>
.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
