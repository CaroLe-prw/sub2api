<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{
  show: boolean
  monitor: ChannelMonitor | null
  publishing: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm', confirmName: string): void
}>()

const { t } = useI18n()
const confirmation = ref('')
const canPublish = computed(() => (
  Boolean(props.monitor)
  && confirmation.value.trim() === props.monitor?.name
  && !props.publishing
))

watch(() => props.show, (show) => {
  if (show) confirmation.value = ''
})

function confirm() {
  if (!canPublish.value) return
  emit('confirm', confirmation.value.trim())
}
</script>

<template>
  <BaseDialog
    :show="show"
    :title="t('admin.channelMonitor.publish.title')"
    width="narrow"
    :close-on-escape="!publishing"
    @close="emit('close')"
  >
    <form class="space-y-4" @submit.prevent="confirm">
      <div class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
        {{ t('admin.channelMonitor.publish.warning') }}
      </div>
      <p class="text-sm text-gray-600 dark:text-gray-300">
        {{ t('admin.channelMonitor.publish.typeName', { name: monitor?.name || '' }) }}
      </p>
      <input
        v-model="confirmation"
        class="input font-mono"
        autocomplete="off"
        :placeholder="monitor?.name || ''"
      />
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="publishing" @click="emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-danger" :disabled="!canPublish" @click="confirm">
          {{ publishing ? t('common.submitting') : t('admin.channelMonitor.publish.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>
