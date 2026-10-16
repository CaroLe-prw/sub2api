<template>
  <BaseDialog :show="show" :title="t('admin.groups.preferredAccounts.title')" width="wide" :show-close-button="!saving" @close="close">
    <div class="space-y-5">
      <div>
        <p class="font-medium text-gray-900 dark:text-white">{{ group?.name }}</p>
        <p class="mt-1 text-sm text-gray-500">{{ t('admin.groups.preferredAccounts.hint') }}</p>
      </div>
      <form class="space-y-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700" @submit.prevent="save">
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label for="preferred-user" class="mb-1 block text-sm">{{ t('admin.groups.preferredAccounts.user') }}</label>
            <input id="preferred-user" v-model="userQuery" class="input w-full" autocomplete="off" :disabled="saving" :placeholder="t('admin.groups.preferredAccounts.searchUser')" @input="searchUsers" />
            <div v-if="users.length" class="mt-1 max-h-40 overflow-auto rounded-lg border bg-white dark:bg-dark-800">
              <button v-for="user in users" :key="user.id" type="button" class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-600" @click="pickUser(user)">
                #{{ user.id }} {{ user.username || user.email }} <span class="text-gray-400">{{ user.username ? user.email : '' }}</span>
              </button>
            </div>
          </div>
          <div>
            <label for="preferred-account" class="mb-1 block text-sm">{{ t('admin.groups.preferredAccounts.account') }}</label>
            <input id="preferred-account" v-model="accountQuery" class="input w-full" autocomplete="off" :disabled="saving" :placeholder="t('admin.groups.preferredAccounts.searchAccount')" @input="searchAccounts" @focus="searchAccountsIfEmpty" />
            <div v-if="accounts.length" class="mt-1 max-h-40 overflow-auto rounded-lg border bg-white dark:bg-dark-800">
              <button v-for="account in accounts" :key="account.id" type="button" class="block w-full px-3 py-2 text-left text-sm hover:bg-gray-100 dark:hover:bg-dark-600" @click="pickAccount(account)">
                #{{ account.id }} {{ account.name }} <span class="text-gray-400">{{ account.status }}</span>
              </button>
            </div>
          </div>
        </div>
        <p class="text-xs text-gray-500">{{ t('admin.groups.preferredAccounts.replaceHint') }}</p>
        <button type="submit" class="btn btn-primary" :disabled="saving || loading || !selectedUserId || !selectedAccountId">{{ t('common.save') }}</button>
      </form>
      <p v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-else-if="entries.length === 0" class="py-4 text-center text-sm text-gray-500">{{ t('admin.groups.preferredAccounts.empty') }}</p>
      <div v-else class="max-h-96 overflow-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="w-full text-left text-sm">
          <thead class="bg-gray-50 dark:bg-dark-700"><tr>
            <th class="p-3">{{ t('admin.groups.preferredAccounts.user') }}</th>
            <th class="p-3">{{ t('admin.groups.preferredAccounts.account') }}</th>
            <th class="p-3">{{ t('common.actions') }}</th>
          </tr></thead>
          <tbody>
            <tr v-for="entry in entries" :key="entry.user_id" class="border-t border-gray-100 dark:border-dark-600">
              <td class="p-3">#{{ entry.user_id }} {{ entry.user_name || entry.user_email }}<div v-if="entry.user_name" class="text-xs text-gray-500">{{ entry.user_email }}</div></td>
              <td class="p-3">#{{ entry.account_id }} {{ entry.account_name }}<div class="text-xs text-gray-500">{{ entry.in_group ? entry.account_status : t('admin.groups.preferredAccounts.outsideGroup') }}</div></td>
              <td class="whitespace-nowrap p-3">
                <button type="button" class="mr-3 text-primary-600" :disabled="saving" @click="edit(entry)">{{ t('common.edit') }}</button>
                <button type="button" class="text-red-600" :disabled="saving" @click="remove(entry.user_id)">{{ t('common.delete') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <template #footer><button class="btn" :disabled="saving" @click="close">{{ t('common.close') }}</button></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { UserPreferredAccountEntry } from '@/api/admin/groups'
import type { AdminGroup, AdminUser, AccountListItem } from '@/types'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; group: AdminGroup | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const app = useAppStore()
const entries = ref<UserPreferredAccountEntry[]>([])
const users = ref<AdminUser[]>([])
const accounts = ref<AccountListItem[]>([])
const userQuery = ref('')
const accountQuery = ref('')
const selectedUserId = ref<number | null>(null)
const selectedAccountId = ref<number | null>(null)
const loading = ref(false)
const saving = ref(false)
let version = 0
let userSearchVersion = 0
let accountSearchVersion = 0
let userTimer: ReturnType<typeof setTimeout> | undefined
let accountTimer: ReturnType<typeof setTimeout> | undefined

function invalidateSearches() {
  clearTimeout(userTimer)
  clearTimeout(accountTimer)
  userSearchVersion++
  accountSearchVersion++
  users.value = []
  accounts.value = []
}

async function load(groupId: number, current: number) {
  loading.value = true
  try {
    const result = await adminAPI.groups.getPreferredAccounts(groupId)
    if (current === version) entries.value = result
  } catch {
    if (current === version) app.showError(t('admin.groups.failedToLoad'))
  } finally {
    if (current === version) loading.value = false
  }
}

watch([() => props.show, () => props.group?.id], () => {
  const current = ++version
  invalidateSearches()
  entries.value = []
  userQuery.value = accountQuery.value = ''
  selectedUserId.value = selectedAccountId.value = null
  saving.value = false
  loading.value = false
  if (props.show && props.group) void load(props.group.id, current)
}, { immediate: true })

function searchUsers() {
  selectedUserId.value = null
  users.value = []
  clearTimeout(userTimer)
  const request = ++userSearchVersion
  const current = version
  const query = userQuery.value.trim()
  if (!query) return
  userTimer = setTimeout(async () => {
    try {
      const result = await adminAPI.users.list(1, 20, { search: query })
      if (current === version && request === userSearchVersion) users.value = result.items
    } catch {
      if (current === version && request === userSearchVersion) app.showError(t('admin.groups.failedToLoad'))
    }
  }, 250)
}

function searchAccounts() {
  selectedAccountId.value = null
  accounts.value = []
  clearTimeout(accountTimer)
  const request = ++accountSearchVersion
  const current = version
  const groupId = props.group?.id
  const query = accountQuery.value.trim()
  if (!groupId) return
  accountTimer = setTimeout(async () => {
    try {
      const result = await adminAPI.accounts.list(1, 30, { group: String(groupId), search: query, lite: 'true' })
      if (current === version && request === accountSearchVersion) accounts.value = result.items
    } catch {
      if (current === version && request === accountSearchVersion) app.showError(t('admin.groups.failedToLoad'))
    }
  }, 250)
}

function searchAccountsIfEmpty() { if (!selectedAccountId.value) searchAccounts() }
function pickUser(user: AdminUser) {
  userSearchVersion++
  selectedUserId.value = user.id
  userQuery.value = `#${user.id} ${user.username || user.email}`
  users.value = []
}
function pickAccount(account: AccountListItem) {
  accountSearchVersion++
  selectedAccountId.value = account.id
  accountQuery.value = `#${account.id} ${account.name}`
  accounts.value = []
}
function edit(entry: UserPreferredAccountEntry) {
  invalidateSearches()
  selectedUserId.value = entry.user_id
  selectedAccountId.value = entry.in_group ? entry.account_id : null
  userQuery.value = `#${entry.user_id} ${entry.user_name || entry.user_email}`
  accountQuery.value = entry.in_group ? `#${entry.account_id} ${entry.account_name}` : ''
}
async function mutate(operation: (groupId: number) => Promise<void>) {
  if (!props.group || saving.value) return
  const groupId = props.group.id
  const current = version
  saving.value = true
  try {
    await operation(groupId)
    if (current !== version) return
    invalidateSearches()
    userQuery.value = accountQuery.value = ''
    selectedUserId.value = selectedAccountId.value = null
    app.showSuccess(t('admin.groups.preferredAccounts.saved'))
    await load(groupId, current)
  } catch {
    if (current === version) app.showError(t('admin.groups.preferredAccounts.saveFailed'))
  } finally {
    if (current === version) saving.value = false
  }
}
function save() {
  const userId = selectedUserId.value
  const accountId = selectedAccountId.value
  if (userId && accountId) void mutate(groupId => adminAPI.groups.setPreferredAccount(groupId, userId, accountId))
}
function remove(userId: number) { void mutate(groupId => adminAPI.groups.deletePreferredAccount(groupId, userId)) }
function close() { if (!saving.value) emit('close') }
onUnmounted(() => { version++; invalidateSearches() })
</script>
