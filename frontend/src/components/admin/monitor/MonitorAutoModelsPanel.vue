<script setup lang="ts">
import { computed, onMounted, ref, shallowRef } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { SchedulerProbePolicy, SchedulerProbeProvider } from '@/api/admin/schedulerProbes'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import Toggle from '@/components/common/Toggle.vue'

const { t } = useI18n()
const appStore = useAppStore()
const loading = shallowRef(true)
const saving = shallowRef(false)
// The toggle mutates policy.enabled in place, so this object needs deep
// reactivity. The loading/saving flags remain shallow scalar refs.
const policy = ref<SchedulerProbePolicy | null>(null)
const whitelistText = shallowRef('')

const providers: SchedulerProbeProvider[] = ['openai', 'anthropic', 'gemini', 'grok']
const whitelist = computed(() => whitelistText.value
  .split(/[\n,]/)
  .map((item) => item.trim())
  .filter(Boolean))

function hydrate(next: SchedulerProbePolicy) {
  policy.value = next
  whitelistText.value = (next.whitelist || []).join('\n')
}

function providerModels(provider: SchedulerProbeProvider, eligible: boolean): string[] {
  const source = eligible ? policy.value?.eligible_by_provider : policy.value?.discovered_by_provider
  return source?.[provider] || []
}

async function load() {
  loading.value = true
  try {
    hydrate(await adminAPI.schedulerProbes.getPolicy())
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.autoModels.loadError')))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!policy.value || saving.value) return
  saving.value = true
  try {
    hydrate(await adminAPI.schedulerProbes.updatePolicy({
      enabled: policy.value.enabled,
      whitelist: whitelist.value,
    }))
    appStore.showSuccess(t('admin.channelMonitor.autoModels.saved'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.autoModels.saveError')))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="rounded-3xl bg-white p-5 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 sm:p-6">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('admin.channelMonitor.autoModels.title') }}
        </h2>
        <p class="mt-1 max-w-3xl text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.channelMonitor.autoModels.description') }}
        </p>
      </div>
      <button type="button" class="btn btn-primary" :disabled="loading || saving || !policy" @click="save">
        {{ saving ? t('common.saving') : t('common.save') }}
      </button>
    </div>

    <div v-if="loading" class="py-8 text-center text-sm text-gray-400">
      {{ t('common.loading') }}
    </div>

    <div v-else-if="policy" class="mt-5 space-y-5">
      <div class="flex items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.channelMonitor.autoModels.enabled') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelMonitor.autoModels.enabledHint') }}</p>
        </div>
        <Toggle v-model="policy.enabled" />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.autoModels.whitelist') }}</label>
        <textarea
          v-model="whitelistText"
          class="input min-h-28 font-mono text-sm"
          :placeholder="t('admin.channelMonitor.autoModels.whitelistPlaceholder')"
        ></textarea>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelMonitor.autoModels.whitelistHint') }}</p>
      </div>

      <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
        <div v-for="provider in providers" :key="provider" class="rounded-xl border border-gray-200 p-3 dark:border-dark-700">
          <div class="flex items-center justify-between gap-2">
            <strong class="text-sm text-gray-900 dark:text-white">{{ t(`monitorCommon.providers.${provider}`) }}</strong>
            <span class="badge badge-gray">
              {{ providerModels(provider, true).length }}/{{ providerModels(provider, false).length }}
            </span>
          </div>
          <p class="mt-2 max-h-24 overflow-y-auto break-all font-mono text-[11px] leading-5 text-gray-500 dark:text-gray-400">
            {{ providerModels(provider, true).join(', ') || t('admin.channelMonitor.autoModels.noModels') }}
          </p>
        </div>
      </div>

      <p class="rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        {{ t('admin.channelMonitor.autoModels.billingHint') }}
      </p>
    </div>
  </section>
</template>
