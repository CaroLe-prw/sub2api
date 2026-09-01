import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import type { PoolMonitorAccount, PoolProbeResult } from '@/api/admin/schedulerProbes'
import MonitorModelHistoryDialog from './MonitorModelHistoryDialog.vue'
import type { ProbeHistoryByPlan } from './monitorDataTypes'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params: Record<string, unknown> = {}) => ({
        'admin.channelMonitor.dataPanel.channelDetailTitle': '渠道监控详情',
        'admin.channelMonitor.dataPanel.streaming': '流式',
        'admin.channelMonitor.dataPanel.probeState': '探测状态',
        'admin.channelMonitor.dataPanel.healthy': '正常',
        'admin.channelMonitor.dataPanel.hasIssues': '存在异常',
        'admin.channelMonitor.dataPanel.scheduling': '调度',
        'admin.channelMonitor.dataPanel.automatic': '自动',
        'admin.channelMonitor.dataPanel.detectedModels': '模型数',
        'admin.channelMonitor.dataPanel.concurrency': '并发',
        'admin.channelMonitor.dataPanel.avgLatency': '平均延迟',
        'admin.channelMonitor.dataPanel.validSamples': '有效样本',
        'admin.channelMonitor.dataPanel.availability': '可用率',
        'admin.channelMonitor.dataPanel.totalSamples': '总样本',
        'admin.channelMonitor.dataPanel.abnormalSamples': '失败样本（红色）',
        'admin.channelMonitor.dataPanel.retainedSamples': '保留样本',
        'admin.channelMonitor.dataPanel.modelPerformance': '各模型表现',
        'admin.channelMonitor.dataPanel.historyScope': '历史',
        'admin.channelMonitor.dataPanel.modelCount': '1 个模型',
        'admin.channelMonitor.dataPanel.probeStatus.success': '在线',
        'admin.channelMonitor.dataPanel.probeStatus.failed': '异常',
        'admin.channelMonitor.dataPanel.probeStatus.degraded': '响应偏慢',
        'admin.channelMonitor.dataPanel.probeStatus.pending': '待探测',
        'admin.channelMonitor.dataPanel.combinedHealth': '真实调用 + 主动探测',
        'admin.channelMonitor.dataPanel.combinedState': '综合状态',
        'admin.channelMonitor.dataPanel.combinedSummary': '综合结果',
        'admin.channelMonitor.dataPanel.userTraffic': '用户调用',
        'admin.channelMonitor.dataPanel.userTrafficTimeline': '用户调用按首字速度分色',
        'admin.channelMonitor.dataPanel.activeProbeTimeline': '主动探测记录',
        'admin.channelMonitor.dataPanel.noActiveProbeForModel': '仅用户调用，未配置主动探测',
        'admin.channelMonitor.dataPanel.noUserTraffic': '暂无用户调用',
        'admin.channelMonitor.dataPanel.timelineSampleCount': `${params.n ?? 0}条`,
        'admin.channelMonitor.dataPanel.activeProbeSummary': '主动探测',
        'admin.channelMonitor.dataPanel.resultBreakdown': `${params.success ?? 0}成功·${params.failed ?? 0}失败`,
        'admin.channelMonitor.dataPanel.probeBreakdown': '探测明细',
        'admin.channelMonitor.dataPanel.modelSourceCounts': '来源样本',
        'admin.channelMonitor.dataPanel.combinedStatus.success': '综合正常',
        'admin.channelMonitor.dataPanel.combinedStatus.degraded': '可用但有异常',
        'admin.channelMonitor.dataPanel.combinedStatus.failed': '综合异常',
        'admin.channelMonitor.dataPanel.combinedStatus.pending': '暂无样本',
        'admin.channelMonitor.runNow': '立即检测',
        'common.close': '关闭',
      })[key] ?? key,
    }),
  }
})

const account: PoolMonitorAccount = {
  account_id: 347,
  name: 'tkapi-福利',
  platform: 'openai',
  type: 'apikey',
  status: 'active',
  schedulable: true,
  concurrency: 100,
  models: [{
    plan_id: 81,
    model: 'gpt-5.6-terra',
    enabled: true,
    status: 'failed',
    latency_ms: 178,
    availability: 99,
    sample_count: 100,
    failure_count: 2,
    last_checked_at: '2026-08-16T03:35:00Z',
    recent_results: [],
  }],
}

function result(id: number, status: PoolProbeResult['status'], createdAt: string, ttftMs: number | null): PoolProbeResult {
  return {
    id,
    plan_id: 81,
    status,
    response_text: status === 'success' ? 'ok' : '',
    error_message: status === 'failed' ? 'upstream error' : '',
    ttft_ms: ttftMs,
    latency_ms: status === 'success' ? 12_500 : 178,
    started_at: createdAt,
    finished_at: createdAt,
    created_at: createdAt,
  }
}

describe('MonitorModelHistoryDialog', () => {
  it('treats a null history response for a new probe plan as no samples', () => {
    let wrapper: ReturnType<typeof mount> | undefined

    expect(() => {
      wrapper = mount(MonitorModelHistoryDialog, {
        props: {
          show: true,
          account: {
            ...account,
            models: [{ ...account.models[0], status: '', sample_count: 0, failure_count: 0 }],
          },
          histories: { 81: null } as unknown as ProbeHistoryByPlan,
          loading: false,
          runningPlanId: null,
        },
        global: {
          stubs: {
            BaseDialog: {
              props: ['show'],
              template: '<div v-if="show"><slot /><slot name="footer" /></div>',
            },
            Icon: true,
            MonitorHeartbeatTimeline: true,
          },
        },
      })
    }).not.toThrow()

    expect(wrapper?.text()).toContain('暂无样本')
  })

  it('does not count successful yellow slow-response samples as abnormal', () => {
    const wrapper = mount(MonitorModelHistoryDialog, {
      props: {
        show: true,
        account: {
          ...account,
          models: [{ ...account.models[0], status: 'success', failure_count: 0 }],
        },
        histories: {
          81: [result(1, 'success', '2026-08-16T03:36:00Z', 12_000)],
        },
        loading: false,
        runningPlanId: null,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
          MonitorHeartbeatTimeline: true,
        },
      },
    })

    expect(wrapper.text()).toContain('1成功·0失败')
    expect(wrapper.text()).toContain('可用但有异常')
  })

  it('shows the latest slow success as degraded even when older history contains failures', () => {
    const wrapper = mount(MonitorModelHistoryDialog, {
      props: {
        show: true,
        account,
        histories: {
          81: [
            result(2, 'success', '2026-08-16T03:36:00Z', 12_000),
            result(1, 'failed', '2026-08-16T03:35:00Z', null),
          ],
        },
        loading: false,
        runningPlanId: null,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
          MonitorHeartbeatTimeline: true,
        },
      },
    })

    expect(wrapper.text()).toContain('可用但有异常')
    expect(wrapper.text()).not.toContain('综合异常')
    expect(wrapper.findAll('.bg-red-500')).toHaveLength(0)
    expect(wrapper.findAll('.bg-amber-500').length).toBeGreaterThan(0)
    expect(wrapper.text()).toContain('1')
  })

  it('does not paint low historical availability green', () => {
    const wrapper = mount(MonitorModelHistoryDialog, {
      props: {
        show: true,
        account: {
          ...account,
          models: [{ ...account.models[0], status: 'success', availability: 86.9 }],
        },
        histories: {
          81: [result(2, 'success', '2026-08-16T03:36:00Z', 500)],
        },
        loading: false,
        runningPlanId: null,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
          MonitorHeartbeatTimeline: true,
        },
      },
    })

    const value = wrapper.findAll('span').find((item) => item.text() === '86.9%')
    expect(value).toBeDefined()
    expect(value!.classes()).toContain('text-red-500')
    expect(value!.classes()).not.toContain('text-emerald-500')
  })

  it('renders real user successes and failures in a separate timeline', () => {
    const wrapper = mount(MonitorModelHistoryDialog, {
      props: {
        show: true,
        account: {
          ...account,
          models: [{
            ...account.models[0],
            user_traffic: {
              window_minutes: 30,
              success_count: 1,
              failure_count: 1,
              avg_ttft_ms: 450,
              last_success_at: '2026-08-16T03:38:00Z',
              last_failure_at: '2026-08-16T03:37:00Z',
              recent_events: [
                { id: 'error:7', status: 'failed', ttft_ms: null, latency_ms: 700, created_at: '2026-08-16T03:37:00Z' },
                { id: 'usage:9', status: 'success', ttft_ms: 450, latency_ms: 900, created_at: '2026-08-16T03:38:00Z' },
              ],
            },
          }],
        },
        histories: {
          81: [result(1, 'success', '2026-08-16T03:36:00Z', 500)],
        },
        loading: false,
        runningPlanId: null,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
          MonitorHeartbeatTooltip: {
            props: ['sample'],
            template: '<button class="heartbeat-stub" :data-id="sample.id" :data-source="sample.source || \'probe\'" :data-status="sample.status" />',
          },
        },
      },
    })

    expect(wrapper.text()).toContain('用户调用按首字速度分色')
    expect(wrapper.text()).toContain('主动探测记录')
    const heartbeats = wrapper.findAll('.heartbeat-stub')
    expect(heartbeats).toHaveLength(3)
    expect(heartbeats.map((item) => [
      item.attributes('data-source'),
      item.attributes('data-status'),
    ])).toEqual([
      ['user', 'failed'],
      ['user', 'success'],
      ['probe', 'success'],
    ])
  })

  it('renders a traffic-only model without offering an invalid probe action', () => {
    const wrapper = mount(MonitorModelHistoryDialog, {
      props: {
        show: true,
        account: {
          ...account,
          models: [
            { ...account.models[0], has_probe: true },
            {
              plan_id: 0,
              has_probe: false,
              model: 'gpt-5.6-sol',
              enabled: false,
              status: '',
              latency_ms: null,
              availability: null,
              sample_count: 0,
              failure_count: 0,
              last_checked_at: null,
              recent_results: [],
              user_traffic: {
                window_minutes: 30,
                success_count: 4,
                failure_count: 0,
                avg_ttft_ms: 430,
                last_success_at: '2026-08-16T03:38:00Z',
                last_failure_at: null,
                recent_events: [
                  { id: 'usage:10', status: 'success', ttft_ms: 430, latency_ms: 840, created_at: '2026-08-16T03:38:00Z' },
                ],
              },
            },
          ],
        },
        histories: { 81: [result(1, 'success', '2026-08-16T03:36:00Z', 500)] },
        loading: false,
        runningPlanId: null,
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
          MonitorHeartbeatTimeline: true,
        },
      },
    })

    expect(wrapper.text()).toContain('gpt-5.6-sol')
    expect(wrapper.text()).toContain('仅用户调用，未配置主动探测')
    expect(wrapper.findAll('[data-testid="run-probe"]')).toHaveLength(1)
  })
})
