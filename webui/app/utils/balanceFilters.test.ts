import { describe, expect, it } from 'vitest'
import type { BalanceFilterControls } from './balanceFilters'
import { toListAccountBalancesRequest } from './balanceFilters'

const noFilters: BalanceFilterControls = { type: 'ALL', currency: 'ALL', user: '', name: '' }

describe('toListAccountBalancesRequest', () => {
  it('asks for the trial balance when nothing is selected', () => {
    expect(toListAccountBalancesRequest(noFilters)).toEqual({})
  })

  it('sends the account type', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, type: 'ACCOUNT_TYPE_ASSETS' }))
      .toEqual({ account: { type: 'ACCOUNT_TYPE_ASSETS' } })
  })

  it('leaves the type out for the ALL option', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, type: 'ALL' })).not.toHaveProperty('account')
  })

  it('leaves the type out for ACCOUNT_TYPE_UNSPECIFIED', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, type: 'ACCOUNT_TYPE_UNSPECIFIED' }))
      .not.toHaveProperty('account')
  })

  it('sends the user', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, user: 'alice' }))
      .toEqual({ account: { user: 'alice' } })
  })

  it('sends the name', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, name: 'Checking' }))
      .toEqual({ account: { name: 'Checking' } })
  })

  it('sends the currency as currencyCode', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, currency: 'EUR' }))
      .toEqual({ currencyCode: 'EUR' })
  })

  it('leaves the currency out for the ALL option', () => {
    expect(toListAccountBalancesRequest({ ...noFilters, currency: 'ALL' }))
      .not.toHaveProperty('currencyCode')
  })

  it('omits an empty text box rather than sending an empty string', () => {
    const request = toListAccountBalancesRequest({ ...noFilters, type: 'ACCOUNT_TYPE_ASSETS' })
    expect(request.account).not.toHaveProperty('user')
    expect(request.account).not.toHaveProperty('name')
  })

  it('sends all four filters together', () => {
    expect(toListAccountBalancesRequest({
      type: 'ACCOUNT_TYPE_EXPENSES',
      currency: 'MYR',
      user: 'bob',
      name: 'Groceries'
    })).toEqual({
      account: { type: 'ACCOUNT_TYPE_EXPENSES', user: 'bob', name: 'Groceries' },
      currencyCode: 'MYR'
    })
  })
})
