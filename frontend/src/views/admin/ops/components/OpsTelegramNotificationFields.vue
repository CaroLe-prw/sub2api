<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import type { OpsTelegramNotificationDraft } from '../types'

const props = defineProps<{
  testing: boolean
  index?: number
  removable?: boolean
}>()

const emit = defineEmits<{
  test: []
  remove: []
}>()

const model = defineModel<OpsTelegramNotificationDraft>({ required: true })
const { t } = useI18n()
const fieldPrefix = computed(() => (props.index == null ? 'ops-telegram' : `telegram-template-${props.index}`))

const topicIdInput = computed<number | ''>({
  get: () => model.value.topic_id ?? '',
  set: (value) => {
    model.value.topic_id = value === '' ? null : Number(value)
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
  <section class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700/50">
    <div class="flex items-start justify-between gap-4">
      <div>
        <input
          v-model="model.name"
          type="text"
          class="input max-w-sm font-medium"
          :aria-label="t('admin.ops.telegram.templateName')"
          :placeholder="t('admin.ops.telegram.templateNamePlaceholder')"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.ops.telegram.description') }}
        </p>
      </div>
      <div class="flex items-center gap-2">
        <span class="text-xs font-medium text-gray-600 dark:text-gray-300">
          {{ t('admin.ops.telegram.enabled') }}
        </span>
        <Toggle v-model="model.enabled" :aria-label="t('admin.ops.telegram.enabled')" />
        <button
          v-if="removable"
          type="button"
          class="btn btn-secondary btn-sm px-2 text-red-600 dark:text-red-400"
          :aria-label="t('common.delete')"
          :title="t('common.delete')"
          @click="emit('remove')"
        >
          <Icon name="trash" size="sm" />
        </button>
      </div>
    </div>

    <div class="mt-4 grid grid-cols-1 gap-4 md:grid-cols-2">
      <div>
        <label :for="`${fieldPrefix}-bot-token`" class="input-label">
          {{ t('admin.ops.telegram.botToken') }}
        </label>
        <input
          :id="`${fieldPrefix}-bot-token`"
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
        <label :for="`${fieldPrefix}-chat-id`" class="input-label">
          {{ t('admin.ops.telegram.chatId') }}
        </label>
        <input
          :id="`${fieldPrefix}-chat-id`"
          v-model="model.chat_id"
          type="text"
          class="input"
          :placeholder="t('admin.ops.telegram.chatIdPlaceholder')"
        />
      </div>

      <div>
        <label :for="`${fieldPrefix}-topic-id`" class="input-label">
          {{ t('admin.ops.telegram.topicId') }}
        </label>
        <input
          :id="`${fieldPrefix}-topic-id`"
          v-model="topicIdInput"
          type="number"
          min="1"
          step="1"
          class="input"
          :placeholder="t('admin.ops.telegram.topicIdPlaceholder')"
        />
      </div>

      <div>
        <label :for="`${fieldPrefix}-base-url`" class="input-label">
          {{ t('admin.ops.telegram.baseUrl') }}
        </label>
        <input
          :id="`${fieldPrefix}-base-url`"
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
