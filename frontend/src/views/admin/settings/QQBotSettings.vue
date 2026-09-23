<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { qqBotAPI, type QQBotView } from '@/api/admin/qqBot'
import { groupsAPI } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores'
import Toggle from '@/components/common/Toggle.vue'

const { locale } = useI18n()
const app = useAppStore()
const text = (zh: string, en: string) => locale.value.startsWith('zh') ? zh : en
const config = ref<QQBotView | null>(null)
const availableGroups = ref<AdminGroup[]>([])
const secret = ref('')
const clearSecret = ref(false)
const groups = ref('')
const admins = ref('')
const monitors = ref('')
const saving = ref(false)
const loading = ref(true)
const refreshing = ref(false)
const previewing = ref(false)
const previewURL = ref('')
const previewPage = ref(1)
const previewPages = ref(1)
let disposed = false
const online = computed(() => config.value?.status.state === 'online')

function assign(value: QQBotView) {
  config.value = { ...value, allow_unmentioned: value.allow_unmentioned ?? false, groups: value.groups ?? [], admins: value.admins ?? [], group_ids: value.group_ids ?? [], monitor_ids: value.monitor_ids ?? [] }
  groups.value = (value.groups ?? []).join('\n')
  admins.value = (value.admins ?? []).join('\n')
  monitors.value = (value.monitor_ids ?? []).join(', ')
  secret.value = ''
  clearSecret.value = false
}

async function load() {
  loading.value = true
  try {
    const [value, allGroups] = await Promise.all([qqBotAPI.get(), groupsAPI.getAllIncludingInactive()])
    availableGroups.value = allGroups
    assign(value)
  } catch {
    app.showError(text('无法加载 QQ 机器人设置', 'Could not load QQ bot settings'))
  } finally { loading.value = false }
}

function split(value: string) { return [...new Set(value.split(/[\s,，]+/).filter(Boolean))] }

async function save() {
  if (!config.value || saving.value) return
  const ids = split(monitors.value).map(Number)
  if (ids.some(id => !Number.isSafeInteger(id) || id <= 0)) {
    app.showError(text('监控编号必须是正整数', 'Monitor IDs must be positive integers'))
    return
  }
  saving.value = true
  try {
    const c = config.value
    assign(await qqBotAPI.update({ enabled: c.enabled, app_id: c.app_id, groups: split(groups.value), admins: split(admins.value), monitor_ids: ids,
      group_ids: c.group_ids, allow_probe: c.allow_probe, allow_unmentioned: c.allow_unmentioned, app_secret: secret.value || undefined, clear_secret: clearSecret.value }))
    app.showSuccess(text('已保存，连接配置将在约 5 秒内生效', 'Saved. Connection settings apply within about 5 seconds.'))
  } catch {
    app.showError(text('保存失败，请检查凭据、OpenID 和展示分组是否已填写', 'Save failed. Check credentials, OpenIDs and selected groups.'))
  } finally { saving.value = false }
}

async function refreshStatus() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    const value = await qqBotAPI.get()
    if (config.value) config.value.status = value.status
  } catch { app.showError(text('无法获取连接状态', 'Could not fetch connection status')) }
  finally { refreshing.value = false }
}

onMounted(load)
onUnmounted(() => { disposed = true; if (previewURL.value) URL.revokeObjectURL(previewURL.value) })

async function preview(page = 1) {
  if (previewing.value) return
  previewing.value = true
  try {
    const result = await qqBotAPI.preview(page)
    if (disposed) return
    if (previewURL.value) URL.revokeObjectURL(previewURL.value)
    previewURL.value = URL.createObjectURL(result.image)
    previewPage.value = page
    previewPages.value = result.pages
  } catch { app.showError(text('无法生成图片，请先保存展示分组并确认 V2 监控已开启', 'Could not render image. Save the selected groups and enable V2 monitoring first.')) }
  finally { previewing.value = false }
}
</script>

<template>
  <section class="card" aria-labelledby="qq-bot-heading">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 id="qq-bot-heading" class="text-lg font-semibold text-gray-900 dark:text-white">{{ text('QQ 群渠道机器人', 'QQ channel status bot') }}</h2>
      <p class="mt-1 text-sm text-gray-500">{{ text('直接使用当前服务连接 QQ，群里发指令才查询，不定时播报。', 'Connects using this server. Replies to group commands only, with no scheduled broadcasts.') }}</p>
    </div>
    <div v-if="loading" class="p-6">{{ text('正在加载…', 'Loading…') }}</div>
    <div v-else-if="!config" class="p-6"><button type="button" class="btn btn-secondary" @click="load">{{ text('重新加载', 'Retry') }}</button></div>
    <div v-else class="space-y-5 p-6">
      <div class="rounded-lg bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-900/20 dark:text-blue-200">
        {{ text('QQ 开放平台保持 WebSocket 模式，无需填写回调地址。保存凭据并启用后，先在测试群发送“@机器人 绑定信息”，再将回复的群 OpenID 填入下方。', 'Keep WebSocket mode in QQ Open Platform; no callback URL is needed. Save credentials and enable, then send “@bot 绑定信息” in a test group and enter the returned group OpenID below.') }}
      </div>
      <div class="flex items-center justify-between gap-4">
        <label for="qq-bot-enabled" class="font-medium">{{ text('启用 QQ 机器人', 'Enable QQ bot') }}</label>
        <Toggle id="qq-bot-enabled" v-model="config.enabled" :disabled="saving" />
      </div>
      <div class="flex items-center justify-between gap-4">
        <div>
          <label for="qq-bot-unmentioned" class="font-medium">{{ text('免 @ 查询', 'Query without mentioning the bot') }}</label>
          <p class="mt-1 text-xs text-gray-500">{{ text('需先在 QQ 平台开启“接收所有消息”。开启后，在允许的群里直接发送“渠道监测”或“渠道状态”即可；普通聊天不触发，检测指令仍需 @。', 'Requires “Receive all messages” in QQ. Allowed groups can send 渠道监测 or 渠道状态 directly. Ordinary chat is ignored; live checks still require a mention.') }}</p>
        </div>
        <Toggle id="qq-bot-unmentioned" v-model="config.allow_unmentioned" :disabled="saving" />
      </div>
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-800" aria-live="polite">
        <span :class="online ? 'text-green-600' : 'text-gray-500'">{{ config.status.detail }}</span>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="refreshing" @click="refreshStatus">{{ text('刷新连接状态', 'Refresh status') }}</button>
      </div>
      <div class="grid gap-4 md:grid-cols-2">
        <div><label for="qq-bot-appid" class="mb-1 block text-sm font-medium">AppID</label><input id="qq-bot-appid" v-model="config.app_id" class="input w-full" inputmode="numeric" autocomplete="off" :disabled="saving" @keydown.enter.prevent="save" /></div>
        <div><label for="qq-bot-secret" class="mb-1 block text-sm font-medium">AppSecret</label><input id="qq-bot-secret" v-model="secret" type="password" class="input w-full" autocomplete="new-password" :disabled="saving" :placeholder="config.secret_configured ? text('已保存，留空保持不变', 'Saved; leave blank to keep') : text('填写 QQ 开放平台密钥', 'Enter the QQ app secret')" @keydown.enter.prevent="save" />
          <label v-if="config.secret_configured" class="mt-2 flex items-center gap-2 text-sm text-gray-500"><input v-model="clearSecret" type="checkbox" :disabled="saving" />{{ text('清除已保存密钥（需先停用）', 'Clear saved secret (disable first)') }}</label>
        </div>
        <div><label for="qq-bot-groups" class="mb-1 block text-sm font-medium">{{ text('允许查询的群 OpenID', 'Allowed group OpenIDs') }}</label><textarea id="qq-bot-groups" v-model="groups" class="input w-full" rows="3" :disabled="saving" /><p class="mt-1 text-xs text-gray-500">{{ text('每行一个；不是 QQ 群号。留空时只响应“绑定信息”。', 'One per line; these are not QQ group numbers. Empty allows only the binding command.') }}</p></div>
        <div><label for="qq-bot-admins" class="mb-1 block text-sm font-medium">{{ text('允许即时检测的成员 OpenID', 'Member OpenIDs allowed to run checks') }}</label><textarea id="qq-bot-admins" v-model="admins" class="input w-full" rows="3" :disabled="saving" /><p class="mt-1 text-xs text-gray-500">{{ text('每行一个，从“绑定信息”回复中获取。群主不会自动获得检测权限。', 'One per line, from the binding reply. Group owners do not automatically receive access.') }}</p></div>
      </div>
      <fieldset v-if="config.monitor_mode === 'v2'" :disabled="saving">
        <legend class="mb-2 text-sm font-medium">{{ text('群成员可以查看的分组', 'Groups visible to QQ members') }}</legend>
        <div class="grid max-h-52 gap-2 overflow-y-auto rounded-lg border p-3 dark:border-dark-600 sm:grid-cols-2">
          <label v-for="group in availableGroups" :key="group.id" class="flex items-center gap-2 text-sm"><input v-model="config.group_ids" type="checkbox" :value="group.id" />{{ group.name }} <span class="text-gray-400">#{{ group.id }}</span></label>
          <p v-if="!availableGroups.length" class="text-sm text-gray-500">{{ text('暂无分组，请先创建分组。', 'Create a group first.') }}</p>
        </div>
        <p class="mt-2 text-xs text-gray-500">{{ text('当前是 V2 监控：默认回复状态看板图片，包含可用率、缓存率、首 Token 延迟和历史状态条。数据为最近 90 分钟汇总，不支持即时实测。', 'V2 replies with a dashboard image showing availability, cache rate, first-token latency and history for the last 90 minutes. Live checks are not supported.') }}</p>
      </fieldset>
      <div v-else><label for="qq-bot-monitors" class="mb-1 block text-sm font-medium">{{ text('允许展示的监控编号（可选）', 'Visible monitor IDs (optional)') }}</label><input id="qq-bot-monitors" v-model="monitors" class="input w-full" :disabled="saving" @keydown.enter.prevent="save" /><p class="mt-1 text-xs text-gray-500">{{ text('用逗号分隔；留空展示所有已启用且已公开的 V1 监控。', 'Comma-separated; empty shows all enabled, published V1 monitors.') }}</p></div>
      <div v-if="config.monitor_mode === 'v1'" class="flex items-center justify-between gap-4"><div><label for="qq-bot-probe" class="font-medium">{{ text('允许即时检测', 'Allow live checks') }}</label><p class="text-xs text-gray-500">{{ text('可能产生模型调用费用；每群至少间隔 60 秒。', 'May incur model usage charges; at least 60 seconds between checks per group.') }}</p></div><Toggle id="qq-bot-probe" v-model="config.allow_probe" :disabled="saving" /></div>
      <div class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-800"><code>@机器人 渠道状态</code> · <code>@机器人 渠道状态 OpenAI</code> · <code>@机器人 渠道状态文字</code></div>
      <p v-if="config.allow_unmentioned" class="text-sm text-gray-500">{{ text('免 @ 示例：渠道监测 · 渠道监测 OpenAI', 'Without a mention: 渠道监测 · 渠道监测 OpenAI') }}</p>
      <div class="flex flex-wrap justify-end gap-3">
        <button v-if="config.monitor_mode === 'v2'" type="button" class="btn btn-secondary" :disabled="previewing || saving" @click="preview(1)">{{ previewing ? text('生成图片中…', 'Rendering…') : text('预览已保存的看板', 'Preview saved dashboard') }}</button>
        <button type="button" class="btn btn-primary" :disabled="saving" @click="save">{{ saving ? text('正在保存…', 'Saving…') : text('保存 QQ 机器人设置', 'Save QQ bot settings') }}</button>
      </div>
      <div v-if="previewURL" class="space-y-3">
        <p class="text-xs text-gray-500">{{ text('仅预览已保存配置，不向 QQ 群发送。', 'Previews saved settings only. Nothing is sent to QQ.') }}</p>
        <img :src="previewURL" :alt="text('QQ 渠道状态看板预览', 'QQ channel dashboard preview')" class="w-full rounded-xl border dark:border-dark-600" />
        <div v-if="previewPages > 1" class="flex items-center justify-center gap-4">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="previewing || previewPage <= 1" @click="preview(previewPage - 1)">{{ text('上一页', 'Previous') }}</button>
          <span>{{ previewPage }} / {{ previewPages }}</span>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="previewing || previewPage >= previewPages" @click="preview(previewPage + 1)">{{ text('下一页', 'Next') }}</button>
        </div>
      </div>
    </div>
  </section>
</template>
