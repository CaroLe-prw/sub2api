<template>
  <div class="channel-card-groups">
    <section v-for="group in groups" :key="group.platform" :aria-label="group.name" class="platform-section">
      <h2 class="platform-heading">
        <span class="provider-icon" :data-platform="group.platform"><ProviderIcon :provider="group.platform" :size="23" /></span>
        {{ group.name }}
        <span class="platform-count">{{ group.entries.length }}</span>
      </h2>
      <div class="channel-card-grid">
        <article v-for="entry in group.entries" :key="entry.key" class="channel-card" :aria-label="entry.name">
          <header class="channel-card-heading">
            <span class="provider-icon card-provider" :data-platform="group.platform"><ProviderIcon :provider="group.platform" :size="23" /></span>
            <div class="channel-identity">
              <h3 :title="entry.name">{{ entry.name }}</h3>
              <div class="channel-subtitle">
                <span class="provider-label" :data-platform="group.platform">{{ group.name }}</span>
                <span v-if="entry.row.model && entry.row.group_name" class="model-label" :title="entry.row.model">{{ entry.row.model }}</span>
              </div>
            </div>
            <span class="health-badge" :data-state="entry.row.health.overall">{{ t(`channelMonitorV2.cards.states.${entry.row.health.overall}`) }}</span>
          </header>

          <dl class="channel-metrics">
            <div class="channel-metric">
              <dt>{{ t('channelMonitorV2.metrics.cacheRate') }}</dt>
              <dd>{{ monitorCacheRate(entry.row.metrics) }}</dd>
            </div>
            <div class="channel-metric">
              <dt>{{ t('channelMonitorV2.cards.availability') }}</dt>
              <dd :data-state="entry.row.health.error_rate">{{ monitorAvailability(entry.row.metrics) }}</dd>
            </div>
            <div class="channel-metric" :title="latency(entry.row.metrics)">
              <dt>{{ t('channelMonitorV2.metrics.ttft') }}</dt>
              <dd :data-state="ttftDisplayState(entry.row.health.ttft, entry.row.metrics.ttft)">{{ formatMonitorMs(entry.row.metrics.ttft.p50_ms) }}</dd>
            </div>
          </dl>

          <footer class="channel-history">
            <div class="history-caption">
              <span>{{ t('channelMonitorV2.cards.intervals', { count: entry.slots.length }) }}</span>
              <span>{{ refreshing ? t('channelMonitorV2.updating') : t('channelMonitorV2.cards.refreshIn', { seconds: countdown }) }}</span>
            </div>
            <div class="history-pulse" :aria-label="t('channelMonitorV2.cards.history')">
              <button
                v-for="slot in entry.slots"
                :key="slot.time"
                type="button"
                class="history-slot"
                :class="slot.bucket ? healthScoreClass(slot.bucket.health, 'overall', slot.bucket.metrics.request_count) : 'health-unknown'"
                :title="slotLabel(slot)"
                :aria-label="slotLabel(slot)"
                :aria-pressed="selectedKey === `${entry.key}:${slot.time}`"
                @click="selectSlot(entry.key, slot)"
              ><span /></button>
            </div>
            <div class="history-axis" aria-hidden="true"><span>PAST</span><span>NOW</span></div>
            <p v-if="selectedKey?.startsWith(`${entry.key}:`)" class="history-detail" role="status">{{ selectedLabel }}</p>
          </footer>
        </article>
      </div>
    </section>
    <div v-if="!groups.length" class="cards-empty">
      <Icon name="chart" size="lg" class="mx-auto mb-3 text-gray-400" />
      <h2>{{ t('channelMonitorV2.empty.title') }}</h2>
      <p>{{ t('channelMonitorV2.empty.description') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorCoverage, MonitorMatrixRow, MonitorMetric } from '@/api/channelMonitorV2'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatLatencyPrivacy, formatMonitorMs, healthScoreClass, ttftDisplayState } from './monitorFormat'
import { monitorAvailability, monitorCacheRate, monitorCardSlots, monitorPlatformName } from './monitorCards'

const props = defineProps<{
  rows: MonitorMatrixRow[]
  coverage: MonitorCoverage
  countdown: number
  refreshing: boolean
}>()
const { t, locale } = useI18n()
type Slot = ReturnType<typeof monitorCardSlots>[number]
const selectedKey = ref<string | null>(null)
const selectedLabel = ref('')
const groups = computed(() => {
  const grouped = new Map<string, { platform: string; name: string; entries: Array<{
    key: string; name: string; row: MonitorMatrixRow; slots: Slot[]
  }> }>()
  for (const row of props.rows) {
    if (!grouped.has(row.platform)) grouped.set(row.platform, { platform: row.platform, name: monitorPlatformName(row.platform), entries: [] })
    grouped.get(row.platform)!.entries.push({
      key: JSON.stringify([row.platform, row.group_id, row.model]),
      name: row.group_name || row.model || monitorPlatformName(row.platform),
      row,
      slots: monitorCardSlots(props.coverage, row),
    })
  }
  return [...grouped.values()]
})
function latency(metric: MonitorMetric) {
  return formatLatencyPrivacy(metric.ttft.p50_ms, metric.ttft.p90_ms, metric.ttft.avg_ms, metric.ttft.p95_ms)
}
function slotLabel(slot: Slot): string {
  const date = new Intl.DateTimeFormat(locale.value || undefined, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(slot.time)
  if (!slot.bucket) return t('channelMonitorV2.matrix.noTrafficAt', { time: date })
  const { metrics, health } = slot.bucket
  const parts = [date, t(`channelMonitorV2.cards.states.${health.overall}`), `${t('channelMonitorV2.cards.availability')} ${monitorAvailability(metrics)}`, `${t('channelMonitorV2.metrics.ttft')} ${formatMonitorMs(metrics.ttft.p50_ms)}`]
  parts.push(`${t('channelMonitorV2.metrics.cacheRate')} ${monitorCacheRate(metrics)}`)
  if (metrics.availability_source && metrics.availability_source !== 'traffic') {
    parts.push(t('channelMonitorV2.matrix.availabilityEvidence', { source: t(`channelMonitorV2.matrix.availabilitySources.${metrics.availability_source}`) }))
  }
  return parts.join(' · ')
}
function selectSlot(key: string, slot: Slot) {
  const next = `${key}:${slot.time}`
  selectedKey.value = selectedKey.value === next ? null : next
  selectedLabel.value = slotLabel(slot)
}
watch(() => props.coverage, () => { selectedKey.value = null })
</script>

<style scoped>
.channel-card-groups { container-type: inline-size; --card-surface: #fff; --metric-surface: #f5f7fb; --card-border: #dce4ef; --card-text: #202938; --card-muted: #8c9db6; color: var(--card-text); }
:global(.dark .channel-card-groups) { container-type: inline-size; --card-surface: #172031; --metric-surface: #1d293b; --card-border: #334155; --card-text: #e5ecf5; --card-muted: #96a7bd; }
.platform-section + .platform-section { margin-top: 30px; }
.platform-heading { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; font-size: 19px; font-weight: 700; }
.provider-icon { display: inline-flex; align-items: center; justify-content: center; flex: none; width: 42px; height: 42px; border-radius: 8px; background: #dcfcef; color: #239778; }
.provider-icon[data-platform="grok"], .provider-label[data-platform="grok"] { background: #eef0f3; color: #454750; }
.provider-icon[data-platform="anthropic"], .provider-label[data-platform="anthropic"] { background: #f7e9dd; color: #a76646; }
.provider-icon[data-platform="gemini"], .provider-label[data-platform="gemini"] { background: #e8edff; color: #5c75bf; }
.platform-count { padding: 0 8px; border: 1px solid var(--card-border); border-radius: 999px; background: var(--card-surface); color: #71829e; font-size: 12px; font-weight: 500; }
.channel-card-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 22px; }
.channel-card { container-type: inline-size; min-width: 0; padding: 24px 22px 20px; border-radius: 7px; background: var(--card-surface); }
.channel-card-heading { display: flex; align-items: center; gap: 12px; position: relative; min-height: 64px; padding-right: 42px; }
.card-provider { width: 46px; height: 46px; }
.channel-identity { min-width: 0; }
.channel-identity h3 { overflow-wrap: anywhere; font-size: 15px; line-height: 1.4; font-weight: 700; }
.channel-subtitle { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin-top: 5px; font-size: 10px; }
.provider-label { padding: 2px 6px; border-radius: 4px; background: #d6fae9; color: #198c6b; font-weight: 600; }
.model-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; max-width: 100%; color: var(--card-muted); }
.health-badge { position: absolute; right: 0; top: 0; padding: 2px 7px; border-radius: 999px; background: var(--metric-surface); color: var(--card-muted); white-space: nowrap; font-size: 10px; font-weight: 600; }
.health-badge[data-state="healthy"] { background: #d5fae9; color: #168565; }
.health-badge[data-state="warning"] { background: #fff0cb; color: #b57708; }
.health-badge[data-state="critical"] { background: #ffe1e3; color: #e04750; }
.channel-metrics { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 7px; margin: 24px 0 22px; }
.channel-metric { min-width: 0; padding: 12px 6px; background: var(--metric-surface); border-radius: 6px; }
.channel-metric dt { font-size: 10px; letter-spacing: .04em; color: var(--card-muted); white-space: nowrap; }
.channel-metric dd { margin-top: 8px; font-size: clamp(14px, 6.5cqw, 22px); font-weight: 700; line-height: 1.2; font-variant-numeric: tabular-nums; white-space: nowrap; }
.channel-metric dd[data-state="healthy"] { color: #12946d; }
.channel-metric dd[data-state="warning"] { color: #ca8507; }
.channel-metric dd[data-state="critical"] { color: #ef5054; }
.channel-metric dd[data-state="unknown"] { color: var(--card-muted); }
.channel-history { border-top: 1px solid var(--card-border); padding-top: 18px; }
.history-caption { display: flex; justify-content: space-between; gap: 8px; color: var(--card-muted); font-size: 10px; font-variant-numeric: tabular-nums; }
.history-pulse { display: flex; gap: 3px; align-items: flex-end; height: 30px; margin-top: 7px; }
.history-slot { position: relative; display: flex; flex: 1; min-width: 0; height: 30px; align-items: flex-end; padding: 3px 0; border-radius: 3px; outline-offset: 2px; }
.history-slot span { width: 100%; height: var(--pulse-height, 21px); border-radius: 3px; background: var(--pulse-color, #0db783); transition: opacity .15s; }
.history-slot:hover span, .history-slot:focus-visible span { opacity: .65; }
.history-slot:focus-visible, .history-slot[aria-pressed="true"] { outline: 2px solid #94a3b8; }
.health-unknown { --pulse-color: #adbacb; --pulse-height: 4px; }
.health-healthy, .health-score8, .health-score9, .health-score10 { --pulse-color: #0db783; }
.health-warning, .health-score5, .health-score6, .health-score7 { --pulse-color: #f5a300; --pulse-height: 12px; }
.health-critical, .health-score0, .health-score1, .health-score2, .health-score3, .health-score4 { --pulse-color: #f34f59; --pulse-height: 5px; }
.history-axis { display: flex; justify-content: space-between; margin-top: 5px; font-size: 8px; color: var(--card-muted); font-family: ui-monospace, monospace; }
.history-detail { margin-top: 12px; color: var(--card-muted); font-size: 11px; line-height: 1.7; }
.cards-empty { padding: 64px 24px; text-align: center; border-radius: 8px; background: var(--card-surface); }
.cards-empty h2 { font-weight: 600; }
.cards-empty p { margin-top: 8px; font-size: 13px; color: var(--card-muted); }
@container (min-width: 600px) { .channel-card-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@container (min-width: 920px) { .channel-card-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); } }
@container (min-width: 1200px) { .channel-card-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); } }
@media (max-width: 639px) { .channel-card-grid { grid-template-columns: minmax(0, 1fr); gap: 16px; } .channel-card { padding: 22px 20px 18px; } .platform-heading { font-size: 18px; } }
</style>
