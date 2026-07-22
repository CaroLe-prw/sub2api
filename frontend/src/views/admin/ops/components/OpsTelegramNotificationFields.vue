<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import type { OpsTelegramNotificationDraft } from '../types'

const props = defineProps<{
  testing: boolean
}>()

const emit = defineEmits<{
  test: []
}>()

const model = defineModel<OpsTelegramNotificationDraft>({ required: true })
const { t } = useI18n()

const topicIdInput = computed<string>({
  get: () => (model.value.topic_id == null ? '' : String(model.value.topic_id)),
  set: (value) => {
    const normalized = value.trim()
    model.value.topic_id = normalized === '' ? null : Number(normalized)
  }
})

const canTest = computed(
  () =>
    !props.testing &&
    model.value.chat_id.trim() !== '' &&
    (model.value.bot_token.trim() !== '' || model.value.bot_token_configured)
)
</script>

<template>
  <section class="rounded-2xl bg-gray-50 p-4 dark:bg-dark-700/50">
    <div class="flex items-start justify-between gap-4">
      <div>
        <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.ops.telegram.title') }}
        </h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.ops.telegram.description') }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
          {{ t('admin.ops.telegram.enabled') }}
        </span>
        <Toggle v-model="model.enabled" :aria-label="t('admin.ops.telegram.enabled')" />
      </div>
    </div>

    <div class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
      <div>
        <label for="ops-telegram-bot-token" class="input-label">
          {{ t('admin.ops.telegram.botToken') }}
        </label>
        <input
          id="ops-telegram-bot-token"
          v-model="model.bot_token"
          type="password"
          class="input"
          autocomplete="new-password"
          autocapitalize="off"
          spellcheck="false"
          :placeholder="
            model.bot_token_configured
              ? t('admin.ops.telegram.botTokenConfiguredPlaceholder')
              : t('admin.ops.telegram.botTokenPlaceholder')
          "
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{
            model.bot_token_configured
              ? t('admin.ops.telegram.botTokenConfiguredHint')
              : t('admin.ops.telegram.botTokenHint')
          }}
        </p>
      </div>

      <div>
        <label for="ops-telegram-chat-id" class="input-label">
          {{ t('admin.ops.telegram.chatId') }}
        </label>
        <input
          id="ops-telegram-chat-id"
          v-model="model.chat_id"
          type="text"
          class="input"
          :placeholder="t('admin.ops.telegram.chatIdPlaceholder')"
        />
      </div>

      <div>
        <label for="ops-telegram-topic-id" class="input-label">
          {{ t('admin.ops.telegram.topicId') }}
        </label>
        <input
          id="ops-telegram-topic-id"
          v-model="topicIdInput"
          type="number"
          min="1"
          step="1"
          class="input"
          :placeholder="t('admin.ops.telegram.topicIdPlaceholder')"
        />
      </div>

      <div>
        <label for="ops-telegram-base-url" class="input-label">
          {{ t('admin.ops.telegram.baseUrl') }}
        </label>
        <input
          id="ops-telegram-base-url"
          v-model="model.base_url"
          type="url"
          class="input"
          placeholder="https://api.telegram.org"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.ops.telegram.baseUrlHint') }}
        </p>
      </div>

      <div class="flex items-start justify-between gap-4 rounded-xl bg-white px-4 py-3 dark:bg-dark-800/50">
        <div>
          <p class="text-sm font-medium text-gray-800 dark:text-gray-200">
            {{ t('admin.ops.telegram.disableNotification') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.ops.telegram.disableNotificationHint') }}
          </p>
        </div>
        <Toggle
          v-model="model.disable_notification"
          :aria-label="t('admin.ops.telegram.disableNotification')"
        />
      </div>

      <div class="flex items-start justify-between gap-4 rounded-xl bg-white px-4 py-3 dark:bg-dark-800/50">
        <div>
          <p class="text-sm font-medium text-gray-800 dark:text-gray-200">
            {{ t('admin.ops.telegram.protectContent') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.ops.telegram.protectContentHint') }}
          </p>
        </div>
        <Toggle v-model="model.protect_content" :aria-label="t('admin.ops.telegram.protectContent')" />
      </div>
    </div>

    <div class="mt-4 flex justify-end">
      <button
        data-test="ops-telegram-test"
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="!canTest"
        @click="emit('test')"
      >
        {{ testing ? t('admin.ops.telegram.testing') : t('admin.ops.telegram.test') }}
      </button>
    </div>
  </section>
</template>
