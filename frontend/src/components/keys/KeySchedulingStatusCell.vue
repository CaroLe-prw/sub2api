<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import Icon from '@/components/icons/Icon.vue'
import type { ApiKey } from '@/types'
import { formatMultiplier } from '@/utils/formatters'

type SchedulingStatus = NonNullable<ApiKey['scheduling_status']>

const props = defineProps<{
  apiKey: ApiKey
}>()

const { t } = useI18n()

const isRateExceeded = computed(() => {
  const threshold = props.apiKey.max_group_rate_multiplier ?? 0
  const currentRate = props.apiKey.effective_group_rate_multiplier ?? 0
  return props.apiKey.status === 'active' && threshold > 0 && currentRate - threshold > 1e-12
})

const displayStatus = computed<SchedulingStatus>(() => {
  if (isRateExceeded.value) return 'temporarily_unavailable'
  return props.apiKey.scheduling_status ?? props.apiKey.status
})

const badgeClass = computed(() => {
  switch (displayStatus.value) {
    case 'active':
      return 'badge-success'
    case 'quota_exhausted':
    case 'temporarily_unavailable':
      return 'badge-warning'
    case 'expired':
      return 'badge-danger'
    default:
      return 'badge-gray'
  }
})

const exceededDetail = computed(() => {
  const currentRate = props.apiKey.effective_group_rate_multiplier
  const threshold = props.apiKey.max_group_rate_multiplier
  if (displayStatus.value !== 'temporarily_unavailable' || currentRate == null || threshold == null) {
    return ''
  }
  return t('keys.groupRateGuardExceededList', {
    rate: formatMultiplier(currentRate),
    threshold: formatMultiplier(threshold),
  })
})
</script>

<template>
  <div class="flex flex-col items-start gap-1">
    <span :class="['badge', badgeClass]">
      {{ t('keys.status.' + displayStatus) }}
    </span>
    <div
      v-if="exceededDetail"
      class="flex items-center gap-1 whitespace-nowrap text-xs font-medium text-amber-700 dark:text-amber-300"
      data-testid="group-rate-exceeded"
      :title="exceededDetail"
    >
      <Icon name="exclamationTriangle" size="xs" :stroke-width="2" />
      <span>{{ exceededDetail }}</span>
    </div>
  </div>
</template>
