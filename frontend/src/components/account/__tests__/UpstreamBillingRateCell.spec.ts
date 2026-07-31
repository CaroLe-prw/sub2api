import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamBillingRateCell from '../UpstreamBillingRateCell.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${Object.values(params).join(',')}` : key
    })
  }
})

const makeAccount = (overrides: Partial<Account> = {}): Account => ({
  id: 1,
  name: 'upstream',
  platform: 'openai',
  type: 'apikey',
  proxy_id: null,
  concurrency: 1,
  priority: 1,
  status: 'active',
  error_message: null,
  last_used_at: null,
  expires_at: null,
  auto_pause_on_expired: false,
  created_at: '2026-07-13T00:00:00Z',
  updated_at: '2026-07-13T00:00:00Z',
  schedulable: true,
  rate_multiplier: 0.04,
  rate_limited_at: null,
  rate_limit_reset_at: null,
  overload_until: null,
  temp_unschedulable_until: null,
  temp_unschedulable_reason: null,
  session_window_start: null,
  session_window_end: null,
  session_window_status: null,
  ...overrides
})

const billingData = {
  object: 'sub2api.key_billing' as const,
  schema_version: 1 as const,
  billing_scope: 'token' as const,
  group_rate_multiplier: 0.8,
  resolved_rate_multiplier: 0.6,
  peak_rate_enabled: true,
  peak_start: '09:00',
  peak_end: '18:00',
  peak_rate_multiplier: 1.5,
  applied_peak_multiplier: 1.5,
  effective_rate_multiplier: 0.9,
  timezone: 'Asia/Shanghai',
  observed_at: '2026-07-13T00:00:00Z'
}

describe('UpstreamBillingRateCell', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-13T00:30:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('calibrates the current upstream rate and keeps the icon-only probe action', async () => {
    const wrapper = mount(UpstreamBillingRateCell, {
      props: {
        account: makeAccount({
          extra: {
            upstream_billing_probe_enabled: true,
            openai_upstream_rate_calibration: 0.1,
            upstream_billing_probe: {
              status: 'ok',
              data: billingData,
              received_at: '2026-07-13T00:00:00Z',
              fresh_until: '2026-07-14T00:00:00Z',
              last_attempt_at: '2026-07-13T00:00:00Z',
              next_probe_at: '2026-07-13T00:30:00Z'
            }
          }
        }),
        now: Date.now()
      }
    })

    expect(wrapper.text()).toContain('0.06x')
    await wrapper.setProps({ now: Date.parse('2026-07-13T01:00:00Z') })
    expect(wrapper.text()).toContain('0.09x')
    await wrapper.setProps({ now: Date.parse('2026-07-13T10:00:00Z') })
    expect(wrapper.text()).toContain('0.06x')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBilling.latest')
    expect(wrapper.get('[data-testid="upstream-billing-probe"]').text()).toBe('')
    expect(wrapper.get('[data-testid="upstream-billing-probe"]').attributes('aria-label')).toBe(
      'admin.accounts.upstreamBilling.manualProbe'
    )
  })

  it('shows subscription remaining and highlights a low balance', async () => {
    const wrapper = mount(UpstreamBillingRateCell, {
      attachTo: document.body,
      props: {
        account: makeAccount({
          extra: {
            upstream_billing_probe_enabled: true,
            upstream_billing_probe: {
              status: 'ok',
              data: {
                ...billingData,
                balance: 12.5,
                balance_kind: 'subscription_remaining'
              },
              balance_alert: {
                active: true,
                threshold: 20,
                triggered_at: '2026-07-13T00:00:00Z'
              },
              received_at: '2026-07-13T00:00:00Z',
              fresh_until: '2026-07-14T00:00:00Z',
              last_attempt_at: '2026-07-13T00:00:00Z',
              next_probe_at: '2026-07-13T00:30:00Z'
            }
          }
        }),
        now: Date.now()
      }
    })

    expect(wrapper.get('[data-testid="upstream-billing-balance"]').text()).toBe('$12.5')
    expect(wrapper.get('[data-testid="upstream-billing-balance"]').classes()).toContain('text-red-600')
    await wrapper.get('[data-testid="upstream-billing-details"]').trigger('mouseenter')
    await flushPromises()
    expect(document.body.textContent).toContain('admin.accounts.upstreamBilling.subscriptionRemaining:$12.5')
    expect(document.body.textContent).toContain('admin.accounts.upstreamBilling.balanceLow:$12.5,$20')
    wrapper.unmount()
  })

  it('uses a synchronized NewAPI group ratio as the calibrated scheduling cost', async () => {
    const newAPIData = {
      object: 'newapi.group_ratio' as const,
      schema_version: 1 as const,
      source: 'newapi' as const,
      billing_scope: 'token' as const,
      group_rate_multiplier: 1,
      resolved_rate_multiplier: 1,
      peak_rate_enabled: false,
      effective_rate_multiplier: 1,
      observed_at: '2026-07-13T00:00:00Z',
      newapi_group: 'GPT Lite大户组',
      balance: 19.864546,
      balance_kind: 'wallet' as const
    }
    const wrapper = mount(UpstreamBillingRateCell, {
      attachTo: document.body,
      props: {
        account: makeAccount({
          rate_multiplier: 1,
          extra: {
            newapi_sync_enabled: true,
            upstream_billing_probe_enabled: false,
            openai_upstream_rate_calibration: 0.03,
            upstream_billing_probe: {
              status: 'ok',
              data: newAPIData,
              balance_alert: {
                active: true,
                threshold: 20,
                triggered_at: '2026-07-13T00:00:00Z'
              },
              received_at: '2026-07-13T00:00:00Z',
              fresh_until: '2026-07-13T01:00:00Z',
              last_attempt_at: '2026-07-13T00:00:00Z',
              next_probe_at: '2026-07-13T00:30:00Z'
            }
          }
        }),
        now: Date.now()
      }
    })

    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.03x')
    expect(wrapper.get('[data-testid="upstream-billing-balance"]').text()).toBe('$19.8645')
    expect(wrapper.get('[data-testid="upstream-billing-balance"]').classes()).toContain('text-red-600')
    await wrapper.get('[data-testid="upstream-billing-details"]').trigger('mouseenter')
    await flushPromises()
    const tooltips = document.body.querySelectorAll('[role="tooltip"]')
    const tooltip = tooltips[tooltips.length - 1] as HTMLElement
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.newapiGroupRate:GPT Lite大户组,1')
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.walletBalance:$19.8645')
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.balanceLow:$19.8645,$20')
    expect(tooltip.textContent).not.toContain('admin.accounts.upstreamBilling.unsupported')
    expect(tooltip.querySelector('[data-testid="upstream-billing-probe-state"] span')?.className).toContain('text-emerald-400')
    wrapper.unmount()
  })

  it('uses retained failed data only while it is still fresh', async () => {
    const account = makeAccount({
      extra: {
        upstream_billing_probe: {
          status: 'ok',
          data: billingData,
          received_at: '2026-07-12T22:00:00Z',
          fresh_until: '2026-07-12T23:00:00Z',
          last_attempt_at: '2026-07-12T22:00:00Z',
          next_probe_at: '2026-07-12T22:30:00Z'
        }
      }
    })
    const wrapper = mount(UpstreamBillingRateCell, { props: { account, now: Date.now() } })
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.stale')
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')

    await wrapper.setProps({
      account: makeAccount({
        extra: {
          upstream_billing_probe: {
            status: 'failed',
            data: billingData,
            received_at: '2026-07-13T00:00:00Z',
            fresh_until: '2026-07-13T01:00:00Z',
            last_attempt_at: '2026-07-13T00:00:00Z',
            next_probe_at: '2026-07-13T01:00:00Z',
            last_error: 'http_error'
          }
        }
      })
    })
    expect(wrapper.text()).toContain('0.6x')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.failed')

    await wrapper.setProps({ now: Date.parse('2026-07-13T01:00:00Z') })
    expect(wrapper.text()).toContain('0.9x')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBilling.stale')

    await wrapper.setProps({ now: Date.parse('2026-07-13T01:00:00.001Z') })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.stale')

    await wrapper.setProps({
      now: Date.now(),
      account: makeAccount({
        extra: {
          upstream_billing_probe: {
            status: 'failed',
            data: billingData,
            received_at: '2026-07-12T22:00:00Z',
            fresh_until: '2026-07-12T23:00:00Z',
            last_attempt_at: '2026-07-13T00:00:00Z',
            next_probe_at: '2026-07-13T01:00:00Z',
            last_error: 'http_error'
          }
        }
      })
    })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.stale')
  })

  it('shows stale snapshot details, local next probe time, and the account probe state', async () => {
    const wrapper = mount(UpstreamBillingRateCell, {
      attachTo: document.body,
      props: {
        account: makeAccount({
          extra: {
            upstream_billing_probe_enabled: true,
            upstream_billing_probe: {
              status: 'ok',
              data: billingData,
              received_at: '2026-07-12T22:00:00Z',
              fresh_until: '2026-07-12T23:00:00Z',
              last_attempt_at: '2026-07-12T22:00:00Z',
              next_probe_at: '2026-07-13T01:00:00Z'
            }
          }
        }),
        now: Date.now()
      }
    })

    expect(wrapper.getComponent(HelpTooltip).props('widthClass')).toBe('w-max max-w-[calc(100vw-2rem)]')
    await wrapper.get('[data-testid="upstream-billing-details"]').trigger('mouseenter')
    await flushPromises()

    const tooltips = document.body.querySelectorAll('[role="tooltip"]')
    const tooltip = tooltips[tooltips.length - 1] as HTMLElement
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.lastDetectedRate:0.9')
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.lastDetectedAt:')
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.elapsedSince:admin.accounts.upstreamBilling.hoursAgo:2')
    expect(tooltip.textContent).toContain('admin.accounts.upstreamBilling.nextProbeAt:')
    expect(tooltip.textContent).not.toContain('admin.accounts.upstreamBilling.stale')
    expect(tooltip.querySelector('[data-testid="upstream-billing-probe-state"] span')?.className).toContain('text-emerald-400')

    await wrapper.setProps({
      account: makeAccount({
        extra: {
          upstream_billing_probe_enabled: false,
          upstream_billing_probe: {
            status: 'unsupported',
            last_attempt_at: '2026-07-13T00:00:00Z',
            next_probe_at: '2026-07-13T01:00:00Z'
          }
        }
      })
    })
    expect(tooltip.querySelector('[data-testid="upstream-billing-next-probe"]')).toBeNull()
    expect(tooltip.querySelector('[data-testid="upstream-billing-probe-state"] span')?.className).toContain('text-red-400')
    wrapper.unmount()
  })

  it('stacks the global-off state below the account state and hides it when globally enabled', async () => {
    const wrapper = mount(UpstreamBillingRateCell, {
      attachTo: document.body,
      props: {
        account: makeAccount({
          extra: {
            upstream_billing_probe_enabled: true,
            upstream_billing_probe: {
              status: 'unsupported',
              last_attempt_at: '2026-07-13T00:00:00Z',
              next_probe_at: '2026-07-13T01:00:00Z'
            }
          }
        }),
        globalProbeEnabled: false,
        now: Date.now()
      }
    })

    await wrapper.get('[data-testid="upstream-billing-details"]').trigger('mouseenter')
    await flushPromises()

    const tooltips = document.body.querySelectorAll('[role="tooltip"]')
    const tooltip = tooltips[tooltips.length - 1] as HTMLElement
    const accountState = tooltip.querySelector('[data-testid="upstream-billing-probe-state"]')
    const globalState = tooltip.querySelector('[data-testid="upstream-billing-global-probe-state"]')
    expect(accountState?.querySelector('span')?.className).toContain('text-emerald-400')
    expect(globalState?.textContent).toContain('admin.accounts.upstreamBilling.globalProbeState')
    expect(globalState?.querySelector('span')?.className).toContain('text-red-400')
    expect(tooltip.querySelector('[data-testid="upstream-billing-next-probe"]')).toBeNull()

    await wrapper.setProps({ globalProbeEnabled: true })
    expect(tooltip.querySelector('[data-testid="upstream-billing-global-probe-state"]')).toBeNull()
    expect(tooltip.querySelector('[data-testid="upstream-billing-next-probe"]')).not.toBeNull()

    await wrapper.setProps({
      globalProbeEnabled: false,
      account: makeAccount({
        extra: { upstream_billing_probe_enabled: false }
      })
    })
    expect(accountState?.querySelector('span')?.className).toContain('text-red-400')
    expect(tooltip.querySelector('[data-testid="upstream-billing-global-probe-state"]')).not.toBeNull()
    expect(tooltip.querySelector('[data-testid="upstream-billing-next-probe"]')).toBeNull()
    wrapper.unmount()
  })

  it('emits manual probe commands only for eligible accounts', async () => {
    const wrapper = mount(UpstreamBillingRateCell, {
      props: { account: makeAccount(), now: Date.now() }
    })
    await wrapper.get('[data-testid="upstream-billing-probe"]').trigger('click')
    expect(wrapper.emitted('probe')).toHaveLength(1)

    await wrapper.setProps({ account: makeAccount({ type: 'oauth' }) })
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(wrapper.text()).toBe('-')
  })

  it('fails neutral for malformed data and timestamps', async () => {
    const malformedAccount = (
      dataOverrides: Partial<typeof billingData> = {},
      snapshotOverrides: Record<string, unknown> = {}
    ) => makeAccount({
      extra: {
        upstream_billing_probe: {
          status: 'ok',
          data: { ...billingData, ...dataOverrides },
          received_at: '2026-07-13T00:00:00Z',
          fresh_until: '2026-07-13T01:00:00Z',
          last_attempt_at: '2026-07-13T00:00:00Z',
          next_probe_at: '2026-07-13T01:00:00Z',
          ...snapshotOverrides
        }
      }
    })
    const wrapper = mount(UpstreamBillingRateCell, {
      props: {
        account: malformedAccount({
          resolved_rate_multiplier: -1,
          peak_rate_enabled: false,
          effective_rate_multiplier: -1
        }),
        now: Date.now()
      }
    })

    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    await wrapper.setProps({ account: malformedAccount({ billing_scope: 'request' as 'token' }) })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    await wrapper.setProps({ account: malformedAccount({}, { received_at: 'not-a-time' }) })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    await wrapper.setProps({ account: malformedAccount({}, { received_at: '2026-07-13T00:31:00Z' }) })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.6x')
    await wrapper.setProps({ account: malformedAccount({}, { received_at: '2026-07-13T00:36:00Z' }) })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    await wrapper.setProps({ account: malformedAccount({}, { fresh_until: '2026-07-12T23:59:00Z' }) })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')

    await wrapper.setProps({
      account: makeAccount({
        extra: {
          upstream_billing_probe: {
            status: 'failed',
            last_attempt_at: '2026-07-13T00:00:00Z',
            next_probe_at: '2026-07-13T01:00:00Z',
            last_error: 'network_error'
          }
        }
      })
    })
    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    expect(wrapper.text()).toContain('admin.accounts.upstreamBilling.failed')
    expect(wrapper.text()).not.toContain('admin.accounts.upstreamBilling.stale')
  })

  it('uses the account billing rate when upstream probing is unsupported', () => {
    const wrapper = mount(UpstreamBillingRateCell, {
      props: {
        account: makeAccount({
          extra: {
            upstream_billing_probe: {
              status: 'unsupported',
              last_attempt_at: '2026-07-13T00:00:00Z',
              next_probe_at: '2026-07-13T00:30:00Z',
              last_error: 'unsupported'
            }
          }
        }),
        now: Date.now()
      }
    })

    expect(wrapper.get('[data-testid="upstream-billing-rate"]').text()).toBe('0.04x')
    expect(wrapper.text()).not.toContain('-admin.accounts.upstreamBilling.unsupported')
  })
})
