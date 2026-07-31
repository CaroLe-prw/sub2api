import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { defineComponent, ref } from 'vue'
import OpsTelegramNotificationFields from '../OpsTelegramNotificationFields.vue'
import type { OpsTelegramNotificationDraft } from '../../types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const Host = defineComponent({
  components: { OpsTelegramNotificationFields },
  setup() {
    const config = ref<OpsTelegramNotificationDraft>({
      id: 'alerts',
      name: 'Ops alerts',
      enabled: true,
      bot_token_configured: true,
      bot_token: '',
      chat_id: '-1001234567890',
      topic_id: null,
      base_url: 'https://api.telegram.org',
      disable_notification: false,
      protect_content: false
    })

    return { config }
  },
  template: '<OpsTelegramNotificationFields v-model="config" :testing="false" />'
})

describe('OpsTelegramNotificationFields', () => {
  it('writes a numeric Topic ID to the form model and resets it to null when cleared', async () => {
    const wrapper = mount(Host)
    const input = wrapper.get<HTMLInputElement>('#ops-telegram-topic-id')

    await input.setValue('42')
    expect(wrapper.vm.config.topic_id).toBe(42)

    await input.setValue('')
    expect(wrapper.vm.config.topic_id).toBeNull()
  })
})
