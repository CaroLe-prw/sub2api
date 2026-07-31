<template>
  <div>
    <div class="mb-1 flex items-center justify-between gap-3">
      <label class="input-label mb-0">
        {{ t('admin.users.groups') }}
        <span class="font-normal text-gray-400">{{ t('common.selectedCount', { count: modelValue.length }) }}</span>
      </label>
      <div
        v-if="priorities !== undefined && modelValue.length > 0"
        class="flex shrink-0 items-center gap-2 text-xs text-gray-600 dark:text-gray-300"
      >
        {{ t('admin.accounts.customizeGroupPriority') }}
        <Toggle
          v-model="customizePriorities"
          :aria-label="t('admin.accounts.customizeGroupPriority')"
          :aria-expanded="customizePriorities"
          data-testid="custom-group-priority-toggle"
        />
      </div>
    </div>
    <div
      v-if="isSearchable"
      class="flex items-center gap-2 rounded-t-lg border border-b-0 border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-600 dark:bg-dark-800"
    >
      <Icon name="search" size="sm" class="shrink-0 text-gray-400" />
      <input
        v-model="searchText"
        type="text"
        :placeholder="t('common.searchPlaceholder')"
        class="flex-1 bg-transparent text-sm text-gray-900 placeholder:text-gray-400 focus:outline-none dark:text-gray-100 dark:placeholder:text-dark-400"
      />
    </div>
    <div
      :class="[
        'grid max-h-48 grid-cols-1 gap-1 overflow-y-auto p-2 sm:grid-cols-2',
        isSearchable
          ? 'rounded-b-lg border border-t-0 border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'
          : 'rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-600 dark:bg-dark-800'
      ]"
    >
      <div
        v-for="group in filteredGroups"
        :key="group.id"
        :class="[
          'flex min-w-0 items-center gap-2 rounded px-2 py-1.5 transition-colors',
          modelValue.includes(group.id)
            ? 'bg-white ring-1 ring-primary-200 dark:bg-dark-700 dark:ring-primary-800'
            : 'hover:bg-white dark:hover:bg-dark-700'
        ]"
        :title="t('admin.groups.rateAndAccounts', { rate: group.rate_multiplier, count: group.account_count || 0 })"
      >
        <label class="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
          <input
            type="checkbox"
            :value="group.id"
            :checked="modelValue.includes(group.id)"
            @change="handleChange(group.id, ($event.target as HTMLInputElement).checked)"
            class="h-3.5 w-3.5 shrink-0 rounded border-gray-300 text-primary-500 focus:ring-primary-500 dark:border-dark-500"
          />
          <GroupBadge
            :name="group.name"
            :platform="group.platform"
            :subscription-type="group.subscription_type"
            :rate-multiplier="group.rate_multiplier"
            class="min-w-0 flex-1"
          />
        </label>
        <label
          v-if="priorities !== undefined && customizePriorities && modelValue.includes(group.id)"
          class="shrink-0"
        >
          <span class="sr-only">{{ group.name }} {{ t('admin.accounts.groupPriority') }}</span>
          <input
            type="number"
            min="1"
            class="h-7 w-16 rounded-md border border-primary-200 bg-white px-1 text-center text-sm font-medium text-gray-900 outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-100 dark:border-primary-800 dark:bg-dark-800 dark:text-gray-100 dark:focus:border-primary-500 dark:focus:ring-primary-900/40"
            :value="priorities[group.id] ?? defaultPriority"
            :title="t('admin.accounts.groupPriorityHint')"
            @input="handlePriorityChange(group.id, $event)"
          />
        </label>
        <span
          v-else-if="priorities !== undefined && modelValue.includes(group.id)"
          class="shrink-0 text-xs text-gray-400"
        >
          {{ t('admin.accounts.followAccountPriorityShort', { priority: defaultPriority }) }}
        </span>
        <span v-else class="shrink-0 text-xs text-gray-400">{{ group.account_count || 0 }}</span>
      </div>
      <div
        v-if="filteredGroups.length === 0"
        class="col-span-2 py-2 text-center text-sm text-gray-500 dark:text-gray-400"
      >
        {{ t('common.noGroupsAvailable') }}
      </div>
    </div>
    <p v-if="priorities !== undefined && modelValue.length > 0" class="input-hint mt-1">
      {{
        customizePriorities
          ? t('admin.accounts.groupPriorityHint')
          : t('admin.accounts.allGroupsFollowAccountPriority', { priority: defaultPriority })
      }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import type { AdminGroup, GroupPlatform } from '@/types'

const { t } = useI18n()

interface Props {
  modelValue: number[]
  groups: AdminGroup[]
  platform?: GroupPlatform // Optional platform filter
  mixedScheduling?: boolean // For antigravity accounts: allow anthropic/gemini groups
  searchable?: boolean | 'auto'
  priorities?: Record<number, number>
  defaultPriority?: number
}

const props = withDefaults(defineProps<Props>(), {
  searchable: 'auto',
  defaultPriority: 1
})
const emit = defineEmits<{
  'update:modelValue': [value: number[]]
  'update:priorities': [value: Record<number, number>]
}>()

const searchText = shallowRef('')
const manuallyCustomizePriorities = shallowRef<boolean | null>(null)

const hasCustomPriorities = computed(() =>
  props.priorities !== undefined &&
  props.modelValue.some(
    (groupID) => (props.priorities?.[groupID] ?? props.defaultPriority) !== props.defaultPriority
  )
)

const resetSelectedPriorities = () => {
  emit(
    'update:priorities',
    Object.fromEntries(props.modelValue.map((groupID) => [groupID, props.defaultPriority]))
  )
}

const customizePriorities = computed({
  get: () => manuallyCustomizePriorities.value ?? hasCustomPriorities.value,
  set: (enabled: boolean) => {
    manuallyCustomizePriorities.value = enabled
    if (!enabled) resetSelectedPriorities()
  }
})

watch(
  () => props.defaultPriority,
  (nextPriority, previousPriority) => {
    if (
      props.priorities === undefined ||
      nextPriority === previousPriority ||
      manuallyCustomizePriorities.value === true
    ) {
      return
    }

    const nextPriorities = { ...props.priorities }
    let changed = false
    for (const groupID of props.modelValue) {
      const currentPriority = nextPriorities[groupID] ?? previousPriority
      if (currentPriority === previousPriority && currentPriority !== nextPriority) {
        nextPriorities[groupID] = nextPriority
        changed = true
      }
    }
    if (changed) {
      emit('update:priorities', nextPriorities)
    }
  }
)

const isSearchable = computed(() => {
  if (props.searchable === 'auto') return props.groups.length > 5
  return props.searchable
})

// Filter groups by platform if specified
const filteredGroups = computed(() => {
  let result: AdminGroup[] = props.groups
  if (props.platform) {
    // antigravity 账户启用混合调度后，可选择 anthropic/gemini 分组
    if (props.platform === 'antigravity' && props.mixedScheduling) {
      result = result.filter(
        (g) => g.platform === 'antigravity' || g.platform === 'anthropic' || g.platform === 'gemini' || g.platform === 'composite'
      )
    } else {
      // 默认：只能选择同 platform 的分组；composite 分组可接收任意具体平台账号
      result = result.filter((g) => g.platform === props.platform || g.platform === 'composite')
    }
  }
  if (isSearchable.value && searchText.value) {
    const q = searchText.value.toLowerCase()
    result = result.filter(
      (g) => g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q)
    )
  }
  return result
})

const handleChange = (groupId: number, checked: boolean) => {
  const newValue = checked
    ? [...props.modelValue, groupId]
    : props.modelValue.filter((id) => id !== groupId)
  emit('update:modelValue', newValue)

  if (props.priorities !== undefined) {
    const next = { ...props.priorities }
    if (checked) {
      next[groupId] ??= props.defaultPriority
    } else {
      delete next[groupId]
    }
    emit('update:priorities', next)
  }
}

const handlePriorityChange = (groupID: number, event: Event) => {
  const raw = Number((event.target as HTMLInputElement).value)
  emit('update:priorities', {
    ...props.priorities,
    [groupID]: Number.isFinite(raw) && raw >= 1 ? Math.trunc(raw) : 1
  })
}
</script>
