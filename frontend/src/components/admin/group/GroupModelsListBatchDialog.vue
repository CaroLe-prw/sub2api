<template>
  <BaseDialog
    :show="show"
    :title="t('admin.groups.modelsList.batch.title')"
    width="normal"
    @close="close"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('admin.groups.modelsList.batch.description', { count: selectedCount }) }}
      </p>

      <div class="flex items-center justify-between gap-4 rounded-lg border border-gray-200 px-3 py-3 dark:border-dark-600">
        <div>
          <p class="text-sm font-medium text-gray-800 dark:text-gray-100">
            {{ t('admin.groups.modelsList.batch.enable') }}
          </p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.groups.modelsList.hint') }}
          </p>
        </div>
        <button
          type="button"
          class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors"
          :class="draft.enabled ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'"
          :aria-pressed="draft.enabled"
          @click="draft.enabled = !draft.enabled"
        >
          <span
            class="inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform"
            :class="draft.enabled ? 'translate-x-6' : 'translate-x-1'"
          />
        </button>
      </div>

      <div>
        <label class="input-label" for="batch-models-list-input">
          {{ t('admin.groups.modelsList.batch.models') }}
        </label>
        <textarea
          id="batch-models-list-input"
          v-model="draft.modelsText"
          rows="9"
          class="input min-h-40 resize-y font-mono text-sm"
          :placeholder="t('admin.groups.modelsList.batch.modelsPlaceholder')"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.groups.modelsList.batch.modelsHint', { count: modelCount }) }}
        </p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="submit">
          <Icon v-if="saving" name="sync" size="sm" class="mr-2 animate-spin" />
          {{ t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelsListConfig } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  show: boolean
  selectedCount: number
  saving: boolean
}>()

const emit = defineEmits<{
  close: []
  save: [config: ModelsListConfig]
}>()

const { t } = useI18n()
const draft = reactive({ enabled: true, modelsText: '' })

const models = computed(() => normalizeModels(draft.modelsText))
const modelCount = computed(() => models.value.length)

watch(
  () => props.show,
  (show) => {
    if (show) {
      draft.enabled = true
      draft.modelsText = ''
    }
  },
)

function close() {
  if (!props.saving) {
    emit('close')
  }
}

function submit() {
  emit('save', { enabled: draft.enabled, models: models.value })
}

function normalizeModels(text: string): string[] {
  const seen = new Set<string>()
  const models: string[] = []
  for (const raw of text.split(/[,\n]/)) {
    const model = raw.trim()
    if (!model || seen.has(model)) continue
    seen.add(model)
    models.push(model)
  }
  return models
}
</script>
