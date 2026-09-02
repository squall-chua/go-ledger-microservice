import { describe, expect, it } from 'vitest'
import { MAX_TRANSACTION_PAGE_SIZE, toListTransactionsRequest } from './transactionFilters'

describe('toListTransactionsRequest', () => {
  it('sends no filter at all when nothing is filled in', () => {
    expect(toListTransactionsRequest({})).toEqual({
      pageSize: 10,
      pageNumber: 1,
      orderByAscending: false
    })
  })

  it('sends both ends of a date range as RFC3339 strings', () => {
    const request = toListTransactionsRequest({
      startDate: '2026-01-01T00:00:00Z',
      endDate: '2026-02-01T00:00:00Z'
    })
    expect(request.filter).toEqual({
      startDate: '2026-01-01T00:00:00.000Z',
      endDate: '2026-02-01T00:00:00.000Z'
    })
  })

  it('sends only the end when only the end is filled in', () => {
    const request = toListTransactionsRequest({ endDate: '2026-02-01T00:00:00Z' })
    expect(request.filter).toEqual({ endDate: '2026-02-01T00:00:00.000Z' })
  })

  it('is newest-first by default', () => {
    expect(toListTransactionsRequest({}).orderByAscending).toBe(false)
    expect(toListTransactionsRequest({ sort: 'newest' }).orderByAscending).toBe(false)
  })

  it('asks for ascending order only when the control says oldest first', () => {
    expect(toListTransactionsRequest({ sort: 'oldest' }).orderByAscending).toBe(true)
  })

  it('never asks for a page larger than the server allows', () => {
    expect(toListTransactionsRequest({ pageSize: 500 }).pageSize).toBe(MAX_TRANSACTION_PAGE_SIZE)
    expect(toListTransactionsRequest({ pageSize: MAX_TRANSACTION_PAGE_SIZE }).pageSize).toBe(100)
    expect(toListTransactionsRequest({ pageSize: 50 }).pageSize).toBe(50)
  })

  it('passes the page number through', () => {
    expect(toListTransactionsRequest({ page: 3 }).pageNumber).toBe(3)
  })
})
