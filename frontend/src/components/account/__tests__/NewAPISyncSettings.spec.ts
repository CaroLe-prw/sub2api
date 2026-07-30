import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import NewAPISyncSettings from '../NewAPISyncSettings.vue'
import type { NewAPISyncConfig } from '@/types'

const api = vi.hoisted(() => ({
  getNewAPISyncConfig: vi.fn(),
  updateNewAPISyncConfig: vi.fn(),
  testNewAPISyncConnection: vi.fn(),
  syncNewAPIRatio: vi.fn()
}))
const notifications = vi.hoisted(() => ({ showSuccess: vi.fn(), showError: vi.fn() }))

vi.mock('@/api/admin', () => ({ adminAPI: { accounts: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => notifications }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
      te: () => false
    })
  }
})

const config = (overrides: Partial<NewAPISyncConfig> = {}): NewAPISyncConfig => ({
  newapi_sync_enabled: true,
  newapi_base_url: 'https://newapi.example.test',
  newapi_user_access_token: '********',
  newapi_user_id: 42,
  newapi_last_sync_status: 'never',
  newapi_cross_group_retry: false,
  current_ratio: 0.04,
  has_newapi_user_access_token: true,
  ...overrides
})

describe('NewAPISyncSettings', () => {
  beforeEach(() => {
    Object.values(api).forEach(mock => mock.mockReset())
    notifications.showSuccess.mockReset()
    notifications.showError.mockReset()
    api.getNewAPISyncConfig.mockResolvedValue(config())
    api.updateNewAPISyncConfig.mockResolvedValue(config())
  })

  it('does not render or validate connection parameters while synchronization is disabled', async () => {
    api.getNewAPISyncConfig.mockResolvedValue(config({
      newapi_sync_enabled: false,
      newapi_base_url: '',
      newapi_user_access_token: '',
      newapi_user_id: 0,
      has_newapi_user_access_token: false
    }))
    api.updateNewAPISyncConfig.mockResolvedValue(config({
      newapi_sync_enabled: false,
      newapi_base_url: '',
      newapi_user_access_token: '',
      newapi_user_id: 0,
      has_newapi_user_access_token: false
    }))
    const wrapper = mount(NewAPISyncSettings, { props: { accountId: 7, enabled: false } })
    await flushPromises()

    expect(wrapper.find('#newapi-base-url').exists()).toBe(false)
    expect(wrapper.find('#newapi-user-id').exists()).toBe(false)
    expect(wrapper.find('#newapi-access-token').exists()).toBe(false)
    expect(wrapper.find('[data-testid="newapi-sync-test"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="newapi-sync-run"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="newapi-sync-save"]').exists()).toBe(false)

    const exposed = wrapper.vm as unknown as { persistConfig: () => Promise<boolean> }
    expect(await exposed.persistConfig()).toBe(true)
    await flushPromises()

    expect(api.updateNewAPISyncConfig).not.toHaveBeenCalled()
  })

  it('persists disabling an existing NewAPI synchronization without showing its fields', async () => {
    api.updateNewAPISyncConfig.mockResolvedValue(config({ newapi_sync_enabled: false }))
    const wrapper = mount(NewAPISyncSettings, { props: { accountId: 7, enabled: false } })
    await flushPromises()

    const exposed = wrapper.vm as unknown as { persistConfig: () => Promise<boolean> }
    expect(await exposed.persistConfig()).toBe(true)

    expect(api.updateNewAPISyncConfig).toHaveBeenCalledWith(7, {
      newapi_sync_enabled: false,
      newapi_base_url: 'https://newapi.example.test',
      newapi_user_access_token: '********',
      newapi_user_id: 42
    })
    expect(wrapper.find('[data-testid="newapi-sync-settings"]').exists()).toBe(false)
  })

  it('loads only masked credentials and preserves them when testing', async () => {
    api.testNewAPISyncConnection.mockResolvedValue({
      account_id: 7,
      status: 'ok',
      changed: false,
      old_ratio: 0.04,
      new_ratio: 0.065,
      resolution: {
        user_group: 'GPT Lite',
        token_group: 'GPT Lite大户组',
        actual_group: 'GPT Lite大户组',
        cross_group_retry: false,
        ratio: 0.0325,
        ratio_source: 'configured_group'
      }
    })
    const wrapper = mount(NewAPISyncSettings, { props: { accountId: 7, enabled: true } })
    await flushPromises()

    expect((wrapper.get('#newapi-access-token').element as HTMLInputElement).value).toBe('********')
    expect(wrapper.text()).not.toContain('encrypted')

    await wrapper.get('[data-testid="newapi-sync-test"]').trigger('click')
    await flushPromises()

    expect(api.updateNewAPISyncConfig).toHaveBeenCalledWith(7, expect.objectContaining({
      newapi_user_access_token: '********'
    }))
    expect(api.updateNewAPISyncConfig.mock.calls[0]?.[1]).not.toHaveProperty('newapi_sync_interval')
    expect(api.updateNewAPISyncConfig.mock.calls[0]?.[1]).not.toHaveProperty('newapi_api_key')
    expect(api.testNewAPISyncConnection).toHaveBeenCalledWith(7)
    expect(api.syncNewAPIRatio).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('GPT Lite大户组')
    expect(wrapper.text()).toContain('0.065x')
  })

  it('emits the synchronized ratio after an immediate sync', async () => {
    api.syncNewAPIRatio.mockResolvedValue({
      account_id: 7,
      status: 'ok',
      changed: true,
      old_ratio: 0.04,
      new_ratio: 0.065
    })
    api.getNewAPISyncConfig
      .mockResolvedValueOnce(config())
      .mockResolvedValueOnce(config({
        current_ratio: 0.065,
        newapi_last_sync_status: 'ok',
        newapi_resolved_user_group: 'GPT Lite',
        newapi_resolved_token_group: 'GPT Lite大户组',
        newapi_resolved_actual_group: 'GPT Lite大户组',
        newapi_ratio_source: 'configured_group'
      }))
    const wrapper = mount(NewAPISyncSettings, { props: { accountId: 7, enabled: true } })
    await flushPromises()

    await wrapper.get('[data-testid="newapi-sync-run"]').trigger('click')
    await flushPromises()

    expect(api.syncNewAPIRatio).toHaveBeenCalledWith(7)
    expect(wrapper.emitted('synced')?.at(-1)?.[0]).toBe(0.065)
    expect(wrapper.text()).toContain('0.065x')
  })
})
