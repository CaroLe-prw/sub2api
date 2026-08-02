<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = withDefaults(defineProps<{
  idPrefix?: string
  showMetadata?: boolean
  hasApiKey?: boolean
  syncInterval?: number
  accessTokenRequired?: boolean
}>(), {
  idPrefix: 'newapi',
  showMetadata: false,
  hasApiKey: false,
  syncInterval: 30,
  accessTokenRequired: false
})

const baseUrl = defineModel<string>('baseUrl', { required: true })
const userId = defineModel<number>('userId', { required: true })
const userAccessToken = defineModel<string>('userAccessToken', { required: true })
const { t } = useI18n()

const ids = computed(() => ({
  baseUrl: `${props.idPrefix}-base-url`,
  userId: `${props.idPrefix}-user-id`,
  accessToken: `${props.idPrefix}-access-token`,
  apiKey: `${props.idPrefix}-api-key`,
  syncInterval: `${props.idPrefix}-sync-interval`
}))
</script>

<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
    <div class="sm:col-span-2">
      <label class="input-label" :for="ids.baseUrl">
        {{ t('admin.accounts.newapiSync.baseUrl') }}
      </label>
      <input
        :id="ids.baseUrl"
        v-model.trim="baseUrl"
        type="url"
        class="input font-mono"
        placeholder="https://newapi.example.com"
        autocomplete="off"
      />
      <p class="input-hint">{{ t('admin.accounts.newapiSync.baseUrlHint') }}</p>
    </div>

    <div class="sm:col-span-2">
      <label class="input-label" :for="ids.userId">
        {{ t('admin.accounts.newapiSync.userId') }}
      </label>
      <input
        :id="ids.userId"
        v-model.number="userId"
        type="number"
        min="1"
        step="1"
        class="input"
        :required="accessTokenRequired"
      />
    </div>

    <div class="sm:col-span-2">
      <label class="input-label" :for="ids.accessToken">
        {{ t('admin.accounts.newapiSync.accessToken') }}
      </label>
      <input
        :id="ids.accessToken"
        v-model="userAccessToken"
        type="password"
        class="input font-mono"
        autocomplete="new-password"
        data-1p-ignore
        data-lpignore="true"
        data-bwignore="true"
        :required="accessTokenRequired"
        :placeholder="t('admin.accounts.newapiSync.secretPlaceholder')"
      />
      <p class="input-hint">
        {{
          t(
            accessTokenRequired
              ? 'admin.accounts.newapiSync.secretRequiredHint'
              : 'admin.accounts.newapiSync.secretHint'
          )
        }}
      </p>
    </div>

    <template v-if="showMetadata">
      <div>
        <label class="input-label" :for="ids.apiKey">
          {{ t('admin.accounts.newapiSync.apiKey') }}
        </label>
        <input
          :id="ids.apiKey"
          type="text"
          class="input font-mono"
          :value="hasApiKey ? '********' : ''"
          :placeholder="t('admin.accounts.newapiSync.apiKeyMissing')"
          disabled
        />
        <p class="input-hint">{{ t('admin.accounts.newapiSync.apiKeyHint') }}</p>
      </div>

      <div>
        <label class="input-label" :for="ids.syncInterval">
          {{ t('admin.accounts.newapiSync.syncInterval') }}
        </label>
        <input
          :id="ids.syncInterval"
          type="number"
          class="input"
          :value="syncInterval"
          disabled
        />
        <p class="input-hint">{{ t('admin.accounts.newapiSync.syncIntervalHint') }}</p>
      </div>
    </template>
  </div>
</template>
