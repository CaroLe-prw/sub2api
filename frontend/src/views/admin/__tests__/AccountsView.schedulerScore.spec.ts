import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups,
  listProbeOverview,
  listProbeResults,
  runProbeNow
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  listProbeOverview: vi.fn(),
  listProbeResults: vi.fn(),
  runProbeNow: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    },
    schedulerProbes: {
      listOverview: listProbeOverview,
      listResults: listProbeResults,
      runNow: runProbeNow
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

// Render the relevant cells for every row so their formatting is observable.
const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <div data-test="data-table">
      <div v-for="row in data" :key="row.id">
        <div :data-test="'scheduler-score-' + row.id">
          <slot name="cell-scheduler_score" :row="row" />
        </div>
        <span :data-test="'rate-multiplier-' + row.id">
          <slot name="cell-rate_multiplier" :row="row" />
        </span>
        <div :data-test="'model-availability-' + row.id">
          <slot name="cell-model_availability" :row="row" />
        </div>
      </div>
    </div>
  `
}

const PaginationStub = {
  name: 'Pagination',
  emits: ['update:page', 'update:pageSize'],
  template: '<button data-test="next-page" type="button" @click="$emit(\'update:page\', 2)">next</button>'
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: true,
        Pagination: PaginationStub,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: { template: '<div></div>' },
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        MonitorCompactHeartbeatStrip: true,
        MonitorModelHistoryDialog: {
          props: ['show', 'account', 'histories', 'loading', 'runningPlanId'],
          template: '<div data-test="monitor-detail" :data-show="String(show)" />'
        },
        AccountGroupsCell: true,
        AccountUsageCell: true,
        Icon: true
      }
    }
  })
}

const baseAccount = {
  platform: 'openai',
  type: 'apikey',
  status: 'active',
  schedulable: true,
  concurrency: 1,
  priority: 0,
  error_message: null,
  last_used_at: null,
  expires_at: null,
  auto_pause_on_expired: false,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z'
}

describe('admin AccountsView scheduler score column', () => {
  beforeEach(() => {
    localStorage.clear()

    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()
    listProbeOverview.mockReset()
    listProbeResults.mockReset()
    runProbeNow.mockReset()

    listAccounts.mockResolvedValue({
      items: [
        {
          ...baseAccount,
          id: 1,
          name: 'ungrouped-openai',
          rate_multiplier: 0.065,
          // 未分组账号：后端只返回基础分（scheduler_score），无分组维度分数
          scheduler_score: {
            base_score: 1.234567,
            sticky_score: 0,
            sticky_weighted_enabled: false
          }
        },
        {
          ...baseAccount,
          id: 2,
          name: 'grouped-openai',
          group_ids: [5],
          scheduler_score: {
            base_score: 2,
            sticky_score: 3,
            sticky_weighted_enabled: true
          },
          scheduler_scores: [
            {
              group_id: 5,
              group_name: 'group-five',
              base_score: 2,
              sticky_score: 3,
              sticky_weighted_enabled: true
            }
          ]
        },
        {
          ...baseAccount,
          id: 4,
          name: 'cost-ineligible-grouped-openai',
          group_ids: [6],
          scheduler_score: {
            base_score: 4,
            sticky_score: 5,
            sticky_weighted_enabled: true
          },
          scheduler_scores: []
        },
        {
          ...baseAccount,
          id: 3,
          name: 'no-score',
          platform: 'anthropic'
        }
      ],
      total: 4,
      page: 1,
      page_size: 20,
      pages: 1
    })
    listWithEtag.mockResolvedValue({
      notModified: true,
      etag: null,
      data: null
    })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
    listProbeOverview.mockResolvedValue({
      items: [{
        account_id: 1,
        name: 'ungrouped-openai',
        platform: 'openai',
        type: 'apikey',
        status: 'active',
        schedulable: true,
        concurrency: 1,
        models: [
          {
            plan_id: 81,
            model: 'gpt-5.6-sol',
            enabled: true,
            status: 'success',
            latency_ms: 900,
            availability: 100,
            sample_count: 1,
            failure_count: 0,
            last_checked_at: '2026-08-14T00:00:00Z',
            recent_results: []
          },
          {
            plan_id: 82,
            model: 'gpt-5.6-terra',
            enabled: true,
            status: 'failed',
            latency_ms: 15000,
            availability: 0,
            sample_count: 1,
            failure_count: 1,
            last_checked_at: '2026-08-14T00:00:00Z',
            recent_results: []
          }
        ]
      }]
    })
    listProbeResults.mockResolvedValue([])
    runProbeNow.mockResolvedValue(null)
  })

  it('falls back to the base score for ungrouped accounts instead of showing a dash', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listAccounts.mock.calls[0]?.[2]).toEqual(expect.objectContaining({
      include_scheduler_score: '0'
    }))

    const ungroupedCell = wrapper.find('[data-test="scheduler-score-1"]')
    expect(ungroupedCell.exists()).toBe(true)
    expect(ungroupedCell.text()).toContain('1.234567')
    expect(ungroupedCell.text()).toContain('admin.accounts.schedulerScore.ungrouped')
    expect(ungroupedCell.text()).not.toBe('-')
  })

  it('shows current model availability and opens the shared monitor detail', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(listProbeOverview).toHaveBeenCalledWith([1, 2, 4, 3])
    const availabilityCell = wrapper.get('[data-test="model-availability-1"]')
    expect(availabilityCell.text()).toContain('1/2')

    await availabilityCell.get('button').trigger('click')
    await flushPromises()

    expect(listProbeResults).toHaveBeenCalledWith(81, 100)
    expect(listProbeResults).toHaveBeenCalledWith(82, 100)
    expect(wrapper.get('[data-test="monitor-detail"]').attributes('data-show')).toBe('true')
  })

  it('loads model availability automatically after switching pages', async () => {
    const accountForPage = (id: number, name: string) => ({
      ...baseAccount,
      id,
      name,
      rate_multiplier: 1,
    })
    listAccounts.mockImplementation(async (page: number) => ({
      items: page === 2
        ? [accountForPage(7, 'page-two-openai')]
        : [accountForPage(1, 'page-one-openai')],
      total: 2,
      page,
      page_size: 1,
      pages: 2,
    }))
    listProbeOverview.mockImplementation(async (accountIDs: number[]) => ({
      items: accountIDs.map((accountID) => ({
        account_id: accountID,
        name: accountID === 7 ? 'page-two-openai' : 'page-one-openai',
        platform: 'openai',
        type: 'apikey',
        status: 'active',
        schedulable: true,
        concurrency: 1,
        models: [{
          plan_id: accountID * 10,
          model: 'gpt-5.6-sol',
          enabled: true,
          status: 'success',
          latency_ms: 900,
          availability: 100,
          sample_count: 1,
          failure_count: 0,
          last_checked_at: '2026-08-14T00:00:00Z',
          recent_results: [],
        }],
      })),
    }))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="model-availability-1"]').text()).toContain('1/1')

    await wrapper.get('[data-test="next-page"]').trigger('click')
    await flushPromises()

    expect(listProbeOverview).toHaveBeenLastCalledWith([7])
    expect(wrapper.get('[data-test="model-availability-7"]').text()).toContain('1/1')
  })

  it('renders per-group scores for grouped accounts', async () => {
    const wrapper = mountView()
    await flushPromises()

    const groupedCell = wrapper.find('[data-test="scheduler-score-2"]')
    expect(groupedCell.exists()).toBe(true)
    expect(groupedCell.text()).toContain('group-five')
    expect(groupedCell.text()).toContain('2')
  })

  it('does not fall back to the base score when a grouped account has no eligible group score', async () => {
    const wrapper = mountView()
    await flushPromises()

    const ineligibleCell = wrapper.find('[data-test="scheduler-score-4"]')
    expect(ineligibleCell.exists()).toBe(true)
    expect(ineligibleCell.text()).toBe('-')
  })

  it('keeps the account billing multiplier precision in the list', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="rate-multiplier-1"]').text()).toBe('0.065x')
  })

  it('keeps scheduler score hidden for old saved column settings until the admin opts in again', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['today_stats']))

    mountView()
    await flushPromises()

    expect(listAccounts.mock.calls[0]?.[2]).toEqual(expect.objectContaining({
      include_scheduler_score: '0'
    }))
    expect(JSON.parse(localStorage.getItem('account-hidden-columns') || '[]')).toContain('scheduler_score')
  })

  it('requests scheduler scores when the migrated column settings explicitly show the column', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['today_stats']))
    localStorage.setItem('account-hidden-columns-version', 'scheduler-score-hidden-by-default')

    mountView()
    await flushPromises()

    expect(listAccounts.mock.calls[0]?.[2]).toEqual(expect.objectContaining({
      include_scheduler_score: '1'
    }))
  })

  it('still shows a dash when no scheduler score is available', async () => {
    const wrapper = mountView()
    await flushPromises()

    const emptyCell = wrapper.find('[data-test="scheduler-score-3"]')
    expect(emptyCell.exists()).toBe(true)
    expect(emptyCell.text()).toBe('-')
  })
})
