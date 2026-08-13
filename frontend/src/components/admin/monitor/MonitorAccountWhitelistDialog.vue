<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PoolAccountModelPolicy, PoolMonitorAccount } from '@/api/admin/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ModelTagInput from '@/components/admin/channel/ModelTagInput.vue'

const props = defineProps<{
  show: boolean
  account: PoolMonitorAccount | null
  policy: PoolAccountModelPolicy | null
  loading: boolean
  saving: boolean
}>()

const emit = defineEmits<{
  close: []
  save: [whitelist: string[]]
}>()

const { t } = useI18n()
const draft = ref<string[]>([])
const title = computed(() => props.account
  ? t('admin.channelMonitor.dataPanel.accountWhitelist.titleWithName', { name: props.account.name })
  : t('admin.channelMonitor.dataPanel.accountWhitelist.title'))

watch(
  () => [props.show, props.policy] as const,
  ([show, policy]) => {
    if (show) draft.value = [...(policy?.whitelist ?? [])]
  },
  { immediate: true }
)

function addModel(model: string) {
  if (!draft.value.includes(model)) draft.value = [...draft.value, model]
}

function addAllDiscovered() {
  draft.value = [...new Set([...(props.policy?.discovered_models ?? []), ...draft.value])]
}
</script>

<template>
  <BaseDialog :show="show" :title="title" width="wide" :close-on-escape="!saving" @close="emit('close')">
    <div v-if="loading" class="flex min-h-52 items-center justify-center text-gray-400">
      <Icon name="refresh" size="lg" class="animate-spin" />
    </div>
    <div v-else-if="account && policy" class="space-y-5">
      <div class="rounded-xl border border-blue-100 bg-blue-50/70 p-3 text-xs leading-5 text-blue-700 dark:border-blue-900/60 dark:bg-blue-950/30 dark:text-blue-300">
        {{ t('admin.channelMonitor.dataPanel.accountWhitelist.description') }}
      </div>

      <section>
        <div class="mb-2 flex items-center justify-between gap-3">
          <label class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.whitelist') }}</label>
          <button type="button" class="text-xs font-medium text-primary-600 hover:text-primary-700" @click="draft = []">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.inheritGlobal') }}</button>
        </div>
        <ModelTagInput
          :models="draft"
          :platform="account.platform"
          :placeholder="t('admin.channelMonitor.dataPanel.accountWhitelist.placeholder')"
          @update:models="draft = $event"
        />
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ draft.length === 0 ? t('admin.channelMonitor.dataPanel.accountWhitelist.emptyMeansGlobal') : t('admin.channelMonitor.dataPanel.accountWhitelist.restrictedCount', { n: draft.length }) }}
        </p>
      </section>

      <section>
        <div class="mb-2 flex items-center justify-between gap-3">
          <h4 class="text-sm font-semibold text-gray-800 dark:text-gray-100">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.discovered') }}</h4>
          <button type="button" class="text-xs font-medium text-primary-600 hover:text-primary-700" @click="addAllDiscovered">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.selectAll') }}</button>
        </div>
        <div class="flex max-h-40 flex-wrap gap-2 overflow-auto rounded-xl border border-gray-200 p-3 dark:border-dark-700">
          <button
            v-for="model in policy.discovered_models"
            :key="model"
            type="button"
            class="rounded-lg border px-2.5 py-1 font-mono text-xs transition"
            :class="draft.includes(model) ? 'border-primary-400 bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300' : 'border-gray-200 text-gray-600 hover:border-primary-300 dark:border-dark-600 dark:text-gray-300'"
            @click="addModel(model)"
          >
            {{ model }}
          </button>
          <span v-if="policy.discovered_models.length === 0" class="text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.noModels') }}</span>
        </div>
      </section>

      <section class="rounded-xl bg-gray-50 p-3 dark:bg-dark-900/40">
        <div class="text-xs font-semibold text-gray-600 dark:text-gray-300">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.currentEffective') }}</div>
        <div class="mt-2 flex flex-wrap gap-1.5">
          <span v-for="model in policy.effective_models" :key="model" class="rounded-md bg-white px-2 py-1 font-mono text-[11px] text-gray-600 shadow-sm dark:bg-dark-800 dark:text-gray-300">{{ model }}</span>
          <span v-if="policy.effective_models.length === 0" class="text-xs text-gray-400">{{ t('admin.channelMonitor.dataPanel.accountWhitelist.noEffectiveModels') }}</span>
        </div>
      </section>
    </div>

    <template #footer>
      <button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('common.cancel') }}</button>
      <button type="button" class="btn btn-primary" :disabled="loading || saving || !policy" @click="emit('save', draft)">
        <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
        {{ t('common.save') }}
      </button>
    </template>
  </BaseDialog>
</template>
