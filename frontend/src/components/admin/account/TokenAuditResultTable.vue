<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { TokenAuditResult } from '@/api/admin/accounts'
import { Icon } from '@/components/icons'

const props = defineProps<{ result: TokenAuditResult; retryingSample?: string }>()
const emit = defineEmits<{ (event: 'retry', sample: TokenAuditResult['samples'][number]): void }>()
const { t } = useI18n()
</script>

<template>
  <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
    <table class="min-w-full text-left text-xs">
      <thead class="bg-gray-50 text-gray-500 dark:bg-dark-700 dark:text-gray-400">
        <tr>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.sample') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.local') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.input') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.difference') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.variable') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.output') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.cached') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.reasoning') }}</th>
          <th class="px-3 py-2 font-medium">HTTP</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.error') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('admin.accounts.tokenAudit.action') }}</th>
        </tr>
      </thead>
      <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
        <tr v-for="sample in result.samples" :key="sample.name" class="text-gray-700 dark:text-gray-200">
          <td class="px-3 py-2 font-medium">{{ sample.name }}</td>
          <td class="px-3 py-2">{{ sample.local_tokens }}</td>
          <td class="px-3 py-2">{{ sample.input_tokens ?? '-' }}</td>
          <td class="px-3 py-2">{{ sample.difference_tokens ?? '-' }}</td>
          <td class="px-3 py-2">{{ sample.variable_tokens?.toFixed(1) ?? '-' }}</td>
          <td class="px-3 py-2">{{ sample.output_tokens ?? '-' }}</td>
          <td class="px-3 py-2">{{ sample.cached_tokens ?? '-' }}</td>
          <td class="px-3 py-2">{{ sample.reasoning_tokens ?? '-' }}</td>
          <td class="px-3 py-2">{{ sample.http_status || '-' }}</td>
          <td class="px-3 py-2 text-red-600 dark:text-red-400">
            {{ sample.error_type || sample.error_code || (sample.transport_error ? 'network_error' : '-') }}
          </td>
          <td class="px-3 py-2">
            <button
              v-if="sample.error_type || sample.error_code || sample.transport_error || sample.timed_out"
              type="button"
              class="inline-flex h-7 w-7 items-center justify-center rounded-md text-gray-500 hover:bg-gray-100 hover:text-primary-600 disabled:opacity-50 dark:hover:bg-dark-600"
              :title="t('admin.accounts.tokenAudit.retry')"
              :aria-label="t('admin.accounts.tokenAudit.retry')"
              :disabled="props.retryingSample === sample.name"
              @click="emit('retry', sample)"
            >
              <Icon name="refresh" size="sm" :class="props.retryingSample === sample.name ? 'animate-spin' : ''" :stroke-width="2" />
            </button>
            <span v-else class="text-gray-400">-</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
