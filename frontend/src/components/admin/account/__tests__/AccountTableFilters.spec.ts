import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AccountTableFilters from '../AccountTableFilters.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const commonProps = {
  searchQuery: '',
  filters: { platform: '', type: '', status: '', privacy_mode: '', group: '' },
  groups: []
}

describe('AccountTableFilters selected group score toggle', () => {
  it('only shows the toggle when a concrete group is selected', () => {
    const hidden = mount(AccountTableFilters, {
      props: { ...commonProps, selectedGroupId: null },
      global: { stubs: { Select: true, SearchInput: true, Toggle: true } }
    })
    expect(hidden.text()).not.toContain('admin.accounts.schedulerScore.onlySelectedGroup')

    const visible = mount(AccountTableFilters, {
      props: { ...commonProps, selectedGroupId: 5 },
      global: { stubs: { Select: true, SearchInput: true, Toggle: true } }
    })
    expect(visible.text()).toContain('admin.accounts.schedulerScore.onlySelectedGroup')
  })
})
