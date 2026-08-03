<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { opsAPI } from '@/api/admin/ops'
import type {
  OpsTelegramNotificationDraft,
  OpsTelegramNotificationTestRequest,
  OpsTelegramNotificationUpdateRequest
} from '@/api/admin/ops'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { useAppStore } from '@/stores'
import OpsTelegramNotificationFields from '@/views/admin/ops/components/OpsTelegramNotificationFields.vue'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const testingID = ref('')
const templates = ref<OpsTelegramNotificationDraft[]>([])
const opsAlertTemplateID = ref('')
const upstreamRateChangeEnabled = ref(false)
const upstreamRateChangeTemplateID = ref('')
const upstreamBalanceLowEnabled = ref(false)
const upstreamBalanceLowTemplateID = ref('')

const enabledTemplateOptions = computed(() => [
  { value: '', label: t('admin.ops.telegram.noTemplate') },
  ...templates.value
    .filter((template) => template.enabled)
    .map((template) => ({ value: template.id, label: template.name || t('admin.ops.telegram.unnamedTemplate') }))
])

function newTemplateID(): string {
  return globalThis.crypto?.randomUUID?.() ?? `telegram-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function addTemplate(): void {
  if (templates.value.length >= 50) {
    appStore.showError(t('admin.ops.telegram.validation.tooManyTemplates'))
    return
  }
  templates.value.push({
    id: newTemplateID(),
    name: '',
    enabled: true,
    bot_token: '',
    bot_token_configured: false,
    chat_id: '',
    topic_id: null,
    base_url: 'https://api.telegram.org',
    disable_notification: false,
    protect_content: false
  })
}

function removeTemplate(index: number): void {
  const id = templates.value[index]?.id
  templates.value.splice(index, 1)
  if (opsAlertTemplateID.value === id) opsAlertTemplateID.value = ''
  if (upstreamRateChangeTemplateID.value === id) upstreamRateChangeTemplateID.value = ''
  if (upstreamBalanceLowTemplateID.value === id) upstreamBalanceLowTemplateID.value = ''
}

function validateTemplate(template: OpsTelegramNotificationDraft): string | null {
  if (!template.name.trim()) return t('admin.ops.telegram.validation.nameRequired')
  if (template.name.trim().length > 100) return t('admin.ops.telegram.validation.nameTooLong')
  if (template.enabled && !template.bot_token.trim() && !template.bot_token_configured) {
    return t('admin.ops.telegram.validation.botTokenRequired')
  }
  if (template.enabled && !template.chat_id.trim()) return t('admin.ops.telegram.validation.chatIdRequired')
  if (template.topic_id != null && (!Number.isInteger(template.topic_id) || template.topic_id <= 0)) {
    return t('admin.ops.telegram.validation.topicIdInvalid')
  }
  try {
    const url = new URL(template.base_url.trim())
    if (url.protocol !== 'https:' || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
      return t('admin.ops.telegram.validation.baseUrlInvalid')
    }
  } catch {
    return t('admin.ops.telegram.validation.baseUrlInvalid')
  }
  return null
}

function testRequest(template: OpsTelegramNotificationDraft): OpsTelegramNotificationTestRequest {
  return {
    template_id: template.id,
    bot_token: template.bot_token.trim(),
    chat_id: template.chat_id.trim(),
    topic_id: template.topic_id,
    base_url: template.base_url.trim(),
    disable_notification: template.disable_notification,
    protect_content: template.protect_content
  }
}

async function load(): Promise<void> {
  loading.value = true
  try {
    const config = await opsAPI.getTelegramNotificationConfig()
    templates.value = config.templates.map((template) => ({ ...template, bot_token: '' }))
    opsAlertTemplateID.value = config.ops_alert_template_id
    upstreamRateChangeEnabled.value = config.upstream_rate_change_enabled
    upstreamRateChangeTemplateID.value = config.upstream_rate_change_template_id
    upstreamBalanceLowEnabled.value = config.upstream_balance_low_enabled
    upstreamBalanceLowTemplateID.value = config.upstream_balance_low_template_id
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.ops.telegram.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function testTemplate(template: OpsTelegramNotificationDraft): Promise<void> {
  const validationError = validateTemplate(template)
  if (validationError) {
    appStore.showError(validationError)
    return
  }
  testingID.value = template.id
  try {
    await opsAPI.testTelegramNotification(testRequest(template))
    appStore.showSuccess(t('admin.ops.telegram.testSuccess'))
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.ops.telegram.testFailed'))
  } finally {
    testingID.value = ''
  }
}

async function save(): Promise<void> {
  for (const template of templates.value) {
    const validationError = validateTemplate(template)
    if (validationError) {
      appStore.showError(validationError)
      return
    }
  }
  if (upstreamRateChangeEnabled.value && !upstreamRateChangeTemplateID.value) {
    appStore.showError(t('admin.ops.telegram.validation.rateChangeTemplateRequired'))
    return
  }
  if (upstreamBalanceLowEnabled.value && !upstreamBalanceLowTemplateID.value) {
    appStore.showError(t('admin.ops.telegram.validation.balanceLowTemplateRequired'))
    return
  }
  const payload: OpsTelegramNotificationUpdateRequest = {
    templates: templates.value.map((template) => ({
      ...template,
      name: template.name.trim(),
      bot_token: template.bot_token.trim(),
      chat_id: template.chat_id.trim(),
      base_url: template.base_url.trim()
    })),
    ops_alert_template_id: opsAlertTemplateID.value,
    upstream_rate_change_enabled: upstreamRateChangeEnabled.value,
    upstream_rate_change_template_id: upstreamRateChangeTemplateID.value,
    upstream_balance_low_enabled: upstreamBalanceLowEnabled.value,
    upstream_balance_low_template_id: upstreamBalanceLowTemplateID.value
  }
  saving.value = true
  try {
    const config = await opsAPI.updateTelegramNotificationConfig(payload)
    templates.value = config.templates.map((template) => ({ ...template, bot_token: '' }))
    opsAlertTemplateID.value = config.ops_alert_template_id
    upstreamRateChangeEnabled.value = config.upstream_rate_change_enabled
    upstreamRateChangeTemplateID.value = config.upstream_rate_change_template_id
    upstreamBalanceLowEnabled.value = config.upstream_balance_low_enabled
    upstreamBalanceLowTemplateID.value = config.upstream_balance_low_template_id
    appStore.showSuccess(t('admin.ops.telegram.saveSuccess'))
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.ops.telegram.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div v-if="loading" class="card p-8 text-center text-sm text-gray-500">
      {{ t('common.loading') }}
    </div>

    <template v-else>
      <section class="card">
        <div class="flex items-center justify-between border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.ops.telegram.templatesTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ops.telegram.templatesDescription') }}</p>
          </div>
          <button type="button" class="btn btn-secondary btn-sm" @click="addTemplate">
            {{ t('admin.ops.telegram.addTemplate') }}
          </button>
        </div>
        <div class="space-y-4 p-6">
          <p v-if="templates.length === 0" class="py-6 text-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.ops.telegram.noTemplates') }}
          </p>
          <OpsTelegramNotificationFields
            v-for="(template, index) in templates"
            :key="template.id"
            v-model="templates[index]"
            :index="index"
            :testing="testingID === template.id || saving"
            removable
            @test="testTemplate(template)"
            @remove="removeTemplate(index)"
          />
        </div>
      </section>

      <section class="card">
        <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.ops.telegram.routesTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.ops.telegram.routesDescription') }}</p>
        </div>
        <div class="space-y-5 p-6">
          <div>
            <label class="input-label">{{ t('admin.ops.telegram.opsAlertTemplate') }}</label>
            <Select v-model="opsAlertTemplateID" :options="enabledTemplateOptions" />
          </div>
          <div class="flex items-center justify-between gap-4 border-t border-gray-100 pt-5 dark:border-dark-700">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.ops.telegram.upstreamRateChange') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.telegram.upstreamRateChangeHint') }}</p>
            </div>
            <Toggle v-model="upstreamRateChangeEnabled" />
          </div>
          <div v-if="upstreamRateChangeEnabled">
            <label class="input-label">{{ t('admin.ops.telegram.upstreamRateChangeTemplate') }}</label>
            <Select v-model="upstreamRateChangeTemplateID" :options="enabledTemplateOptions" />
          </div>
          <div class="flex items-center justify-between gap-4 border-t border-gray-100 pt-5 dark:border-dark-700">
            <div>
              <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.ops.telegram.upstreamBalanceLow') }}</p>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.ops.telegram.upstreamBalanceLowHint') }}</p>
            </div>
            <Toggle v-model="upstreamBalanceLowEnabled" />
          </div>
          <div v-if="upstreamBalanceLowEnabled">
            <label class="input-label">{{ t('admin.ops.telegram.upstreamBalanceLowTemplate') }}</label>
            <Select v-model="upstreamBalanceLowTemplateID" :options="enabledTemplateOptions" />
          </div>
        </div>
      </section>

      <div class="flex justify-end">
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </div>
</template>
