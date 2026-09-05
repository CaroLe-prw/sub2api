import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'
import AdminMonitorCardGrid from './AdminMonitorCardGrid.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function makeMonitor(overrides: Partial<ChannelMonitor> = {}): ChannelMonitor {
  return {
    id: 42,
    name: 'internal-upstream',
    provider: 'openai',
    api_mode: 'responses',
    endpoint: 'https://api.example.com',
    api_key_masked: 'sk-t***',
    primary_model: 'gpt-5.6-sol',
    extra_models: [],
    group_name: '',
    enabled: false,
    public_visible: false,
    streaming: true,
    interval_seconds: 300,
    jitter_seconds: 30,
    last_checked_at: '2026-08-13T12:34:56Z',
    created_by: 1,
    created_at: '2026-08-13T00:00:00Z',
    updated_at: '2026-08-13T00:00:00Z',
    primary_status: 'operational',
    primary_latency_ms: 3379,
    primary_ping_latency_ms: 741,
    availability_7d: 99.48,
    extra_models_status: [],
    timeline: [
      {
        status: 'operational',
        latency_ms: 3379,
        ping_latency_ms: 741,
        checked_at: '2026-08-13T12:34:56Z',
      },
    ],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
    ...overrides,
  }
}

describe('AdminMonitorCardGrid', () => {
  beforeEach(() => setActivePinia(createPinia()))
  it('previews private and disabled monitors without making the card interactive', () => {
    const monitor = makeMonitor()
    const wrapper = mount(AdminMonitorCardGrid, {
      props: { items: [monitor], loading: false },
    })

    const card = wrapper.findComponent(MonitorCard)
    expect(card.exists()).toBe(true)
    expect(card.props('interactive')).toBe(false)
    expect(card.props('countdownSeconds')).toBeNull()
    expect(card.props('item')).toMatchObject({
      id: 42,
      primary_ping_latency_ms: 741,
      timeline: monitor.timeline,
    })
    expect(wrapper.text()).toContain('admin.channelMonitor.publish.private')
    expect(wrapper.text()).toContain('admin.channelMonitor.cardPreview.disabled')
  })

  it('labels only an enabled and explicitly published monitor as public', () => {
    const wrapper = mount(AdminMonitorCardGrid, {
      props: {
        items: [makeMonitor({ enabled: true, public_visible: true })],
        loading: false,
      },
    })

    expect(wrapper.text()).toContain('admin.channelMonitor.publish.public')
    expect(wrapper.text()).not.toContain('admin.channelMonitor.cardPreview.disabled')
  })
})
