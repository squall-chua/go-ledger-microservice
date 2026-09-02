import { describe, expect, it, beforeEach } from 'vitest'
import { defineComponent, h, nextTick } from 'vue'
import { mountSuspended, registerEndpoint } from '@nuxt/test-utils/runtime'
import { readBody } from 'h3'
import { useLedgerQuery } from './useLedgerQuery'

/**
 * Requests the stub endpoint has received and not yet answered. Holding the
 * resolver lets a test answer them out of order, which is the whole point of
 * the first case: an overtaken read must not land.
 */
let pending: Array<{ body: any, resolve: (value: unknown) => void }> = []

registerEndpoint('/api/v1/ledger/transactions/query', {
  method: 'POST',
  handler: async (event) => {
    const body = await readBody(event)
    return new Promise((resolve) => {
      pending.push({ body, resolve })
    })
  }
})

const rowsFor = (label: string) => ({
  transactions: [{ id: label }],
  totalCount: '1'
})

/** Lets every queued promise settle, including the fetch round trip. */
const settle = async () => {
  for (let i = 0; i < 4; i++) {
    await new Promise(resolve => setTimeout(resolve, 0))
    await nextTick()
  }
}

let keySeed = 0

const mountQuery = async (options: Record<string, unknown> = {}) => {
  let query: any
  await mountSuspended(defineComponent({
    setup() {
      query = useLedgerQuery({
        key: `test-${++keySeed}`,
        path: '/transactions/query',
        body: () => ({}),
        rows: (response: any) => response.transactions,
        pageSize: 50,
        ...options
      })
      return () => h('div')
    }
  }))
  await settle()
  return query
}

beforeEach(() => {
  pending = []
})

describe('useLedgerQuery', () => {
  it('drops the rows of a read that a newer one overtook', async () => {
    const query = await mountQuery({ body: () => ({ filter: {} }) })

    expect(pending).toHaveLength(1)

    query.refresh()
    await settle()
    expect(pending).toHaveLength(2)

    // The newer read answers first, then the one it overtook. The older rows
    // must never reach the table.
    pending[1]!.resolve(rowsFor('newer'))
    await settle()
    pending[0]!.resolve(rowsFor('older'))
    await settle()

    expect(query.rows.value).toEqual([{ id: 'newer' }])
  })

  it('sends one request when Apply resets the page', async () => {
    const query = await mountQuery({ body: (page: number) => ({ pageNumber: page }) })

    query.page.value = 3
    await settle()
    pending[1]!.resolve(rowsFor('page-3'))
    await settle()
    expect(pending).toHaveLength(2)

    // Apply resets the page and refreshes. Doing both separately is what made
    // the pages fire twice from one press.
    query.apply()
    await settle()

    expect(pending).toHaveLength(3)
    expect(query.page.value).toBe(1)
    expect(pending[2]!.body.pageNumber).toBe(1)
  })

  it('reports a failed read and leaves no stale rows behind', async () => {
    const query = await mountQuery({ body: () => ({ filter: {} }) })

    pending[0]!.resolve(rowsFor('first'))
    await settle()
    expect(query.rows.value).toEqual([{ id: 'first' }])

    query.refresh()
    await settle()
    pending[1]!.resolve(createError({ statusCode: 500, statusMessage: 'ledger is down' }))
    await settle()

    // The page shows the error in place of the table, so the rows of the
    // filter the operator has moved off must not stay on screen.
    expect(query.error.value).toBeTruthy()
    expect(query.rows.value).toEqual([])
    expect(query.loading.value).toBe(false)
  })

  it('counts the range from the page and reports whether a filter was sent', async () => {
    const query = await mountQuery({
      body: (page: number) => ({ pageNumber: page, filter: { idempotencyKey: 'k' } })
    })

    pending[0]!.resolve({ transactions: [{ id: 'a' }], totalCount: '120' })
    await settle()

    expect(query.totalCount.value).toBe(120)
    expect(query.range.value).toEqual({ from: 1, to: 50, total: 120 })
    expect(query.filtered.value).toBe(true)

    query.page.value = 3
    await settle()
    pending[1]!.resolve({ transactions: [{ id: 'b' }], totalCount: '120' })
    await settle()

    expect(query.range.value).toEqual({ from: 101, to: 120, total: 120 })
  })

  it('reports an unfiltered read as unfiltered', async () => {
    const query = await mountQuery({ body: (page: number) => ({ pageNumber: page }) })

    pending[0]!.resolve({ transactions: [], totalCount: '0' })
    await settle()

    expect(query.filtered.value).toBe(false)
    expect(query.range.value).toEqual({ from: 0, to: 0, total: 0 })
  })
})
