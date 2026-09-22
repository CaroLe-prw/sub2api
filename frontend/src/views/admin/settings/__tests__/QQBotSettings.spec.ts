import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import QQBotSettings from '../QQBotSettings.vue'

const mocks = vi.hoisted(() => ({ get: vi.fn(), update: vi.fn(), preview: vi.fn(), groups: vi.fn(), error: vi.fn(), success: vi.fn(), createURL: vi.fn(), revokeURL: vi.fn() }))
vi.mock('@/api/admin/qqBot', () => ({ qqBotAPI: { get: mocks.get, update: mocks.update, preview: mocks.preview } }))
vi.mock('@/api/admin/groups', () => ({ groupsAPI: { getAllIncludingInactive: mocks.groups } }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: mocks.error, showSuccess: mocks.success }) }))

function fixture() {
  return { enabled: false, app_id: '123', groups: ['group-a'], admins: [], monitor_ids: [], group_ids: [7], allow_probe: false,
    secret_configured: true, monitor_mode: 'v2', status: { state: 'disabled', detail: '未启用', updated_at: '' } }
}

async function render() {
  const wrapper = mount(QQBotSettings, { global: { plugins: [createI18n({ legacy: false, locale: 'zh', messages: {} })] } })
  await flushPromises()
  return wrapper
}

describe('QQ bot settings', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.get.mockResolvedValue(fixture())
    mocks.groups.mockResolvedValue([{ id: 7, name: '公开分组' }, { id: 8, name: '备用分组' }])
    mocks.update.mockResolvedValue(fixture())
    mocks.preview.mockResolvedValue({ image: new Blob(['png'], { type: 'image/png' }), pages: 2 })
    mocks.createURL.mockReturnValue('blob:board-preview')
    vi.stubGlobal('URL', Object.assign(class extends URL {}, { createObjectURL: mocks.createURL, revokeObjectURL: mocks.revokeURL }))
  })
  afterEach(() => vi.unstubAllGlobals())

  it('previews saved images without saving drafts or sending group messages', async () => {
    const wrapper = await render()
    await wrapper.find('#qq-bot-groups').setValue('unsaved-group')
    await wrapper.findAll('button').find(button => button.text() === '预览已保存的看板')!.trigger('click')
    await flushPromises()
    expect(mocks.preview).toHaveBeenCalledWith(1)
    expect(mocks.update).not.toHaveBeenCalled()
    expect(wrapper.find('img').attributes('src')).toBe('blob:board-preview')
    expect(wrapper.text()).toContain('不向 QQ 群发送')
    await wrapper.findAll('button').find(button => button.text() === '下一页')!.trigger('click')
    await flushPromises()
    expect(mocks.preview).toHaveBeenLastCalledWith(2)
    wrapper.unmount()
    expect(mocks.revokeURL).toHaveBeenCalledWith('blob:board-preview')
  })

  it('saves only configuration and preserves an existing secret when left blank', async () => {
    const wrapper = await render()
    expect(wrapper.text()).toContain('公开分组')
    expect(wrapper.find('#qq-bot-secret').attributes('type')).toBe('password')
    await wrapper.find('#qq-bot-groups').setValue('group-a\ngroup-b')
    await wrapper.findAll('button').find(button => button.text() === '保存 QQ 机器人设置')!.trigger('click')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(expect.objectContaining({ groups: ['group-a', 'group-b'], app_secret: undefined, group_ids: [7] }))
    expect(mocks.update.mock.calls[0][0]).not.toHaveProperty('status')
    wrapper.unmount()
  })

  it('clears newly typed secrets after saving', async () => {
    const wrapper = await render()
    await wrapper.find('#qq-bot-secret').setValue('new-test-secret')
    await wrapper.findAll('button').find(button => button.text() === '保存 QQ 机器人设置')!.trigger('click')
    await flushPromises()
    expect(mocks.update.mock.calls[0][0].app_secret).toBe('new-test-secret')
    expect((wrapper.find('#qq-bot-secret').element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })

  it('refreshes status without discarding unsaved form edits', async () => {
    const wrapper = await render()
    await wrapper.find('#qq-bot-appid').setValue('456')
    mocks.get.mockResolvedValue({ ...fixture(), status: { state: 'online', detail: '已连接 QQ', updated_at: '' } })
    await wrapper.findAll('button').find(button => button.text() === '刷新连接状态')!.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('已连接 QQ')
    expect((wrapper.find('#qq-bot-appid').element as HTMLInputElement).value).toBe('456')
    wrapper.unmount()
  })

  it('does not allow saving empty defaults after a load failure', async () => {
    mocks.get.mockRejectedValue(new Error('offline'))
    const wrapper = await render()
    expect(wrapper.text()).toContain('重新加载')
    expect(wrapper.text()).not.toContain('保存 QQ 机器人设置')
    expect(mocks.update).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
