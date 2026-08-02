<template>
  <BaseDialog
    :show="show"
    :title="t('admin.groups.manageAccountsTitle', { name: group?.name || '' })"
    width="extra-wide"
    @close="handleClose"
  >
    <div v-if="group" class="space-y-4">
      <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 px-4 py-3 text-sm dark:bg-dark-700">
        <span class="inline-flex items-center gap-1.5 font-medium text-gray-900 dark:text-white">
          <PlatformIcon :platform="group.platform" size="sm" />
          {{ t(`admin.groups.platforms.${group.platform}`) }}
        </span>
        <span class="text-gray-400">|</span>
        <span class="text-gray-600 dark:text-gray-300">
          {{ t('admin.groups.manageAccountsSummary', { selected: selectedIds.size, total: accounts.length }) }}
        </span>
      </div>

      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div class="relative min-w-0 flex-1 sm:max-w-md">
          <Icon
            name="search"
            size="sm"
            class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
          />
          <input
            v-model="searchQuery"
            type="search"
            class="input pl-9"
            :placeholder="t('admin.groups.manageAccountsSearch')"
            :disabled="loading"
          />
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading || filteredAccounts.length === 0"
            @click="selectVisible"
          >
            {{ t('admin.groups.manageAccountsSelectVisible') }}
          </button>
          <button
            type="button"
            class="btn btn-secondary btn-sm"
            :disabled="loading || filteredAccounts.length === 0"
            @click="clearVisible"
          >
            {{ t('admin.groups.manageAccountsClearVisible') }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="flex min-h-40 items-center justify-center rounded-lg border border-gray-200 dark:border-dark-600">
        <Icon name="refresh" size="md" class="animate-spin text-primary-500" />
      </div>

      <div
        v-else-if="filteredAccounts.length === 0"
        class="flex min-h-40 items-center justify-center rounded-lg border border-dashed border-gray-200 px-4 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400"
      >
        {{ accounts.length === 0 ? t('admin.groups.manageAccountsEmpty') : t('admin.groups.manageAccountsNoMatch') }}
      </div>

      <div
        v-else
        class="max-h-[min(56vh,560px)] overflow-y-auto rounded-lg border border-gray-200 dark:border-dark-600"
      >
        <div class="grid grid-cols-1 gap-px bg-gray-200 sm:grid-cols-2 lg:grid-cols-3 dark:bg-dark-600">
          <label
            v-for="account in filteredAccounts"
            :key="account.id"
            class="flex min-w-0 cursor-pointer items-start gap-3 bg-white px-3 py-3 transition-colors hover:bg-gray-50 dark:bg-dark-800 dark:hover:bg-dark-700"
          >
            <input
              type="checkbox"
              class="mt-0.5 h-4 w-4 shrink-0 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
              :checked="selectedIds.has(account.id)"
              @change="toggleAccount(account.id, ($event.target as HTMLInputElement).checked)"
            />
            <span class="min-w-0 flex-1">
              <span class="flex min-w-0 items-center gap-2">
                <span class="truncate font-medium text-gray-900 dark:text-white" :title="account.name">
                  {{ account.name }}
                </span>
                <span class="shrink-0 font-mono text-[10px] text-gray-400">#{{ account.id }}</span>
              </span>
              <span class="mt-1 flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                <span class="inline-flex items-center gap-1">
                  <PlatformIcon :platform="account.platform" size="xs" />
                  {{ t(`admin.groups.platforms.${account.platform}`) }}
                </span>
                <span
                  :class="[
                    'rounded px-1.5 py-0.5 font-medium',
                    account.status === 'active'
                      ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                      : 'bg-gray-100 text-gray-600 dark:bg-dark-600 dark:text-gray-300'
                  ]"
                >
                  {{ t(`admin.accounts.status.${account.status}`) }}
                </span>
              </span>
            </span>
          </label>
        </div>
      </div>

      <p class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.groups.manageAccountsHint') }}
      </p>
    </div>

    <template #footer>
      <div class="flex items-center justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.close') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="loading || saving || !hasChanges"
          @click="handleSave"
        >
          <Icon v-if="saving" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ saving ? t('admin.groups.manageAccountsSaving') : t('admin.groups.manageAccountsSave') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showMixedChannelWarning"
    :title="t('admin.accounts.mixedChannelWarningTitle')"
    :message="mixedChannelWarningMessage"
    :confirm-text="t('common.confirm')"
    :cancel-text="t('common.cancel')"
    danger
    @confirm="confirmMixedChannelSave"
    @cancel="cancelMixedChannelSave"
  />
</template>

<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account, AdminGroup, CheckMixedChannelResponse, UpdateAccountRequest } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'

interface Props {
  show: boolean
  group: AdminGroup | null
}

const props = defineProps<Props>()
const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const accounts = shallowRef<Account[]>([])
const selectedIds = shallowRef<Set<number>>(new Set())
const originalSelectedIds = shallowRef<Set<number>>(new Set())
const searchQuery = shallowRef('')
const loading = shallowRef(false)
const saving = shallowRef(false)
const showMixedChannelWarning = shallowRef(false)
const mixedChannelRisk = shallowRef<CheckMixedChannelResponse | null>(null)
let loadRequestID = 0

interface AccountLinkChange {
  account: Account
  payload: UpdateAccountRequest
}

const pendingChanges = shallowRef<AccountLinkChange[]>([])
const confirmedRiskAccountIDs = shallowRef<Set<number>>(new Set())

const getAccountGroupIDs = (account: Account): number[] => {
  if (Array.isArray(account.group_ids)) return account.group_ids
  return account.groups?.map((group) => group.id) || []
}

const getAccountGroupPriorities = (account: Account, groupIDs: Set<number>): Record<number, number> =>
  Object.fromEntries(
    (account.account_groups || [])
      .filter((group) => groupIDs.has(group.group_id))
      .map((group) => [group.group_id, group.priority])
  )

const filteredAccounts = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return accounts.value

  return accounts.value.filter((account) =>
    [account.name, String(account.id), account.notes || '', account.platform]
      .some((value) => value.toLowerCase().includes(query))
  )
})

const hasChanges = computed(() => {
  if (selectedIds.value.size !== originalSelectedIds.value.size) return true
  for (const id of selectedIds.value) {
    if (!originalSelectedIds.value.has(id)) return true
  }
  return false
})

const mixedChannelWarningMessage = computed(() => {
  const details = mixedChannelRisk.value?.details
  if (!details) return mixedChannelRisk.value?.message || t('admin.groups.manageAccountsSaveFailed')
  return t('admin.accounts.mixedChannelWarning', {
    groupName: details.group_name,
    currentPlatform: details.current_platform,
    otherPlatform: details.other_platform
  })
})

const setAccountSelected = (accountID: number, selected: boolean) => {
  const next = new Set(selectedIds.value)
  if (selected) next.add(accountID)
  else next.delete(accountID)
  selectedIds.value = next
}

const toggleAccount = (accountID: number, selected: boolean) => {
  setAccountSelected(accountID, selected)
}

const selectVisible = () => {
  const next = new Set(selectedIds.value)
  filteredAccounts.value.forEach((account) => next.add(account.id))
  selectedIds.value = next
}

const clearVisible = () => {
  const visibleIDs = new Set(filteredAccounts.value.map((account) => account.id))
  selectedIds.value = new Set(
    [...selectedIds.value].filter((accountID) => !visibleIDs.has(accountID))
  )
}

const loadAccounts = async () => {
  if (!props.group) return

  const requestID = ++loadRequestID
  loading.value = true
  searchQuery.value = ''
  try {
    const pageSize = 1000
    const firstPage = await adminAPI.accounts.list(1, pageSize, {
      lite: 'true',
      sort_by: 'name',
      sort_order: 'asc'
    })
    const loaded = [...(firstPage.items || [])]
    const total = Number(firstPage.total) || loaded.length
    const pages = Math.max(Number(firstPage.pages) || 0, Math.ceil(total / pageSize), 1)

    for (let page = 2; page <= pages; page += 1) {
      const response = await adminAPI.accounts.list(page, pageSize, {
        lite: 'true',
        sort_by: 'name',
        sort_order: 'asc'
      })
      loaded.push(...(response.items || []))
    }

    if (requestID !== loadRequestID) return
    accounts.value = loaded
    const groupID = props.group.id
    const associatedIDs = new Set(
      loaded
        .filter((account) => getAccountGroupIDs(account).includes(groupID))
        .map((account) => account.id)
    )
    selectedIds.value = associatedIDs
    originalSelectedIds.value = new Set(associatedIDs)
  } catch (error: any) {
    if (requestID !== loadRequestID) return
    accounts.value = []
    selectedIds.value = new Set()
    originalSelectedIds.value = new Set()
    appStore.showError(error?.message || t('admin.groups.manageAccountsLoadFailed'))
    console.error('Error loading accounts for group:', error)
  } finally {
    if (requestID === loadRequestID) loading.value = false
  }
}

const updateLocalAccount = (updated: Account | undefined, accountID: number, groupIDs: number[]) => {
  accounts.value = accounts.value.map((account) => {
    if (account.id !== accountID) return account
    return updated ? { ...account, ...updated } : { ...account, group_ids: groupIDs }
  })
}

const buildChanges = (): AccountLinkChange[] => {
  if (!props.group) return []
  const groupID = props.group.id
  return accounts.value
    .filter((account) =>
      originalSelectedIds.value.has(account.id) !== selectedIds.value.has(account.id)
    )
    .map((account) => {
      const nextGroupIDs = new Set(getAccountGroupIDs(account))
      if (selectedIds.value.has(account.id)) nextGroupIDs.add(groupID)
      else nextGroupIDs.delete(groupID)

      return {
        account,
        payload: {
          group_ids: [...nextGroupIDs],
          group_priorities: getAccountGroupPriorities(account, nextGroupIDs)
        }
      }
    })
}

const persistChanges = async (changes: AccountLinkChange[], confirmedAccountIDs = new Set<number>()) => {
  saving.value = true
  try {
    for (const { account, payload } of changes) {
      const updatePayload = confirmedAccountIDs.has(account.id)
        ? { ...payload, confirm_mixed_channel_risk: true }
        : payload
      const updated = await adminAPI.accounts.update(account.id, updatePayload)
      updateLocalAccount(updated, account.id, payload.group_ids || [])
    }

    originalSelectedIds.value = new Set(selectedIds.value)
    appStore.showSuccess(t('admin.groups.manageAccountsSaved'))
    emit('success')
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.groups.manageAccountsSaveFailed'))
    console.error('Error updating group accounts:', error)
    await loadAccounts()
  } finally {
    saving.value = false
  }
}

const handleSave = async () => {
  if (!props.group || !hasChanges.value) return

  const changes = buildChanges()
  if (changes.length === 0) return

  saving.value = true
  try {
    const riskyAccountIDs = new Set<number>()
    let firstRisk: CheckMixedChannelResponse | null = null
    for (const { account, payload } of changes) {
      if (account.platform !== 'anthropic' && account.platform !== 'antigravity') continue
      const risk = await adminAPI.accounts.checkMixedChannelRisk({
        platform: account.platform,
        group_ids: payload.group_ids || [],
        account_id: account.id
      })
      if (!risk.has_risk) continue
      riskyAccountIDs.add(account.id)
      firstRisk ||= risk
    }

    if (firstRisk) {
      pendingChanges.value = changes
      confirmedRiskAccountIDs.value = riskyAccountIDs
      mixedChannelRisk.value = firstRisk
      showMixedChannelWarning.value = true
      return
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.groups.manageAccountsSaveFailed'))
    console.error('Error checking mixed channel risk:', error)
    return
  } finally {
    saving.value = false
  }

  await persistChanges(changes)
}

const cancelMixedChannelSave = () => {
  showMixedChannelWarning.value = false
  mixedChannelRisk.value = null
  pendingChanges.value = []
  confirmedRiskAccountIDs.value = new Set()
}

const confirmMixedChannelSave = async () => {
  const changes = pendingChanges.value
  const confirmedIDs = confirmedRiskAccountIDs.value
  cancelMixedChannelSave()
  await persistChanges(changes, confirmedIDs)
}

const handleClose = () => {
  if (saving.value) return
  emit('close')
}

watch(
  () => [props.show, props.group?.id] as const,
  ([show]) => {
    if (show) void loadAccounts()
    else {
      loadRequestID += 1
      cancelMixedChannelSave()
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.btn-sm {
  padding: 0.375rem 0.625rem;
  font-size: 0.75rem;
}
</style>
