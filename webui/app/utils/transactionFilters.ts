import type { ListTransactionsRequest, TransactionFilter } from '../types/ledger'

/**
 * The server clamps anything larger down to this
 * (internal/repository/postgres.go), so the page never asks for more.
 */
export const MAX_TRANSACTION_PAGE_SIZE = 100

/**
 * The transaction list's filter controls, held exactly as the page holds them.
 * Every field is optional and an empty one is not filtered on at all.
 */
export interface TransactionFilterControls {
  /** A `datetime-local` value. Inclusive lower bound on the transaction date. */
  startDate?: string
  /** A `datetime-local` value. Exclusive upper bound on the transaction date. */
  endDate?: string
  /** Newest transaction date first is the default. */
  sort?: 'newest' | 'oldest'
  /** One-based. */
  page?: number
  pageSize?: number
}

const toTimestamp = (value?: string): string | undefined => {
  return value ? new Date(value).toISOString() : undefined
}

/**
 * Maps the filter controls onto the request the ledger reads. Note that the
 * sort flag is inverted: the wire field says ascending, the control says
 * newest-first.
 */
export const toListTransactionsRequest = (controls: TransactionFilterControls = {}): ListTransactionsRequest => {
  const startDate = toTimestamp(controls.startDate)
  const endDate = toTimestamp(controls.endDate)

  const filter: TransactionFilter = {
    ...(startDate ? { startDate } : {}),
    ...(endDate ? { endDate } : {})
  }

  return {
    ...(Object.keys(filter).length > 0 ? { filter } : {}),
    pageSize: Math.min(controls.pageSize ?? 10, MAX_TRANSACTION_PAGE_SIZE),
    pageNumber: controls.page ?? 1,
    orderByAscending: controls.sort === 'oldest'
  }
}
