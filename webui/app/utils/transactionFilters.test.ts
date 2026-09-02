import { describe, expect, it } from 'vitest'
import { toListTransactionsRequest } from './transactionFilters'
import { MAX_PAGE_SIZE } from './constants'

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
    expect(toListTransactionsRequest({ pageSize: 500 }).pageSize).toBe(MAX_PAGE_SIZE)
    expect(toListTransactionsRequest({ pageSize: MAX_PAGE_SIZE }).pageSize).toBe(100)
    expect(toListTransactionsRequest({ pageSize: 50 }).pageSize).toBe(50)
  })

  it('passes the page number through', () => {
    expect(toListTransactionsRequest({ page: 3 }).pageNumber).toBe(3)
  })

  it('sends a metadata key and value as one exact pair', () => {
    const request = toListTransactionsRequest({ metadataKey: 'order_id', metadataValue: 'A-42' })
    expect(request.filter).toEqual({ metadata: { order_id: 'A-42' } })
  })

  it('sends no metadata at all when the key is blank', () => {
    expect(toListTransactionsRequest({ metadataValue: 'A-42' }).filter).toBeUndefined()
    expect(toListTransactionsRequest({ metadataKey: '   ', metadataValue: 'A-42' }).filter).toBeUndefined()
  })

  it('treats a blank value under a filled key as a search for the empty string', () => {
    const request = toListTransactionsRequest({ metadataKey: 'order_id', metadataValue: '' })
    expect(request.filter).toEqual({ metadata: { order_id: '' } })
  })

  it('sends the idempotency key, and nothing when it is blank', () => {
    expect(toListTransactionsRequest({ idempotencyKey: 'req-7' }).filter).toEqual({ idempotencyKey: 'req-7' })
    expect(toListTransactionsRequest({ idempotencyKey: '  ' }).filter).toBeUndefined()
  })

  it('combines the new filters with the date range, sort and paging', () => {
    expect(toListTransactionsRequest({
      idempotencyKey: 'req-7',
      metadataKey: 'order_id',
      metadataValue: 'A-42',
      startDate: '2026-01-01T00:00:00Z',
      endDate: '2026-02-01T00:00:00Z',
      sort: 'oldest',
      page: 2,
      pageSize: 25
    })).toEqual({
      filter: {
        idempotencyKey: 'req-7',
        startDate: '2026-01-01T00:00:00.000Z',
        endDate: '2026-02-01T00:00:00.000Z',
        metadata: { order_id: 'A-42' }
      },
      pageSize: 25,
      pageNumber: 2,
      orderByAscending: true
    })
  })
})
