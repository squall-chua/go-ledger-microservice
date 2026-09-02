import { describe, expect, it } from 'vitest'
import type { RegisterFilters } from './postingFilters'
import { toListPostingsRequest } from './postingFilters'

const empty: RegisterFilters = {
  accountType: '',
  user: '',
  name: '',
  currency: 'ALL',
  startDate: '',
  endDate: '',
  metadataKey: '',
  metadataValue: '',
  ascending: false
}

const filters = (overrides: Partial<RegisterFilters>): RegisterFilters => ({ ...empty, ...overrides })

describe('toListPostingsRequest', () => {
  it('sends no filter at all when nothing is selected', () => {
    expect(toListPostingsRequest(empty, 1, 50)).toEqual({
      pageSize: 50,
      pageNumber: 1,
      orderByAscending: false
    })
  })

  it('filters by account type alone', () => {
    const request = toListPostingsRequest(filters({ accountType: 'ACCOUNT_TYPE_ASSETS' }), 1, 50)
    expect(request.filter).toEqual({ account: { type: 'ACCOUNT_TYPE_ASSETS' } })
  })

  it('filters by user alone', () => {
    const request = toListPostingsRequest(filters({ user: 'alice' }), 1, 50)
    expect(request.filter).toEqual({ account: { user: 'alice' } })
  })

  it('filters by name alone', () => {
    const request = toListPostingsRequest(filters({ name: 'Checking' }), 1, 50)
    expect(request.filter).toEqual({ account: { name: 'Checking' } })
  })

  it('names one account with all three controls together', () => {
    const request = toListPostingsRequest(
      filters({ accountType: 'ACCOUNT_TYPE_ASSETS', user: 'alice', name: 'Checking' }),
      1,
      50
    )
    expect(request.filter).toEqual({
      account: { type: 'ACCOUNT_TYPE_ASSETS', user: 'alice', name: 'Checking' }
    })
  })

  it('omits an empty text box rather than sending an empty string', () => {
    const request = toListPostingsRequest(filters({ accountType: 'ACCOUNT_TYPE_ASSETS' }), 1, 50)
    expect(request.filter?.account).not.toHaveProperty('user')
    expect(request.filter?.account).not.toHaveProperty('name')
  })

  it('treats a whitespace-only text box as empty and trims the rest', () => {
    const request = toListPostingsRequest(filters({ user: '   ', name: '  Checking  ' }), 1, 50)
    expect(request.filter?.account).toEqual({ name: 'Checking' })
  })

  it('sends the currency at the top level of the filter', () => {
    const request = toListPostingsRequest(filters({ currency: 'EUR' }), 1, 50)
    expect(request.filter).toEqual({ currencyCode: 'EUR' })
  })

  it('does not filter on currency when all currencies are wanted', () => {
    expect(toListPostingsRequest(filters({ currency: 'ALL' }), 1, 50).filter).toBeUndefined()
  })

  it('sends the date range as half-open timestamps', () => {
    const request = toListPostingsRequest(
      filters({ startDate: '2026-01-01T00:00', endDate: '2026-02-01T00:00' }),
      1,
      50
    )
    expect(request.filter?.startDate).toBe(new Date('2026-01-01T00:00').toISOString())
    expect(request.filter?.endDate).toBe(new Date('2026-02-01T00:00').toISOString())
  })

  it('sends one end of the date range on its own', () => {
    const request = toListPostingsRequest(filters({ startDate: '2026-01-01T00:00' }), 1, 50)
    expect(request.filter?.startDate).toBe(new Date('2026-01-01T00:00').toISOString())
    expect(request.filter).not.toHaveProperty('endDate')
  })

  it('sends the metadata pair exactly as typed', () => {
    const request = toListPostingsRequest(
      filters({ metadataKey: 'orderId', metadataValue: 'A-1' }),
      1,
      50
    )
    expect(request.filter?.metadata).toEqual({ orderId: 'A-1' })
  })

  it('sends no metadata when the key is blank, because the server refuses one', () => {
    const request = toListPostingsRequest(filters({ metadataKey: '  ', metadataValue: 'A-1' }), 1, 50)
    expect(request.filter).toBeUndefined()
  })

  it('switches the date order both ways', () => {
    expect(toListPostingsRequest(filters({ ascending: true }), 1, 50).orderByAscending).toBe(true)
    expect(toListPostingsRequest(filters({ ascending: false }), 1, 50).orderByAscending).toBe(false)
  })

  it('never asks for a page size above 100', () => {
    expect(toListPostingsRequest(empty, 1, 500).pageSize).toBe(100)
    expect(toListPostingsRequest(empty, 1, 100).pageSize).toBe(100)
    expect(toListPostingsRequest(empty, 1, 25).pageSize).toBe(25)
  })

  it('passes the page number through', () => {
    expect(toListPostingsRequest(empty, 3, 50).pageNumber).toBe(3)
  })
})
