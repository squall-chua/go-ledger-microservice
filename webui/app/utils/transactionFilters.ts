import type { ListTransactionsRequest, TransactionFilter } from '../types/ledger'
import { MAX_PAGE_SIZE } from './constants'

/**
 * The transaction list's filter controls, held exactly as the page holds them.
 * Every field is optional and an empty one is not filtered on at all.
 */
export interface TransactionFilterControls {
  /** A `datetime-local` value. Inclusive lower bound on the transaction date. */
  startDate?: string
  /** A `datetime-local` value. Exclusive upper bound on the transaction date. */
  endDate?: string
  /** Exact. Matches at most one transaction, or none if that write never landed. */
  idempotencyKey?: string
  /** Blank sends no metadata at all: the server refuses an empty key. */
  metadataKey?: string
  /** Exact. Blank alongside a filled key searches for the empty string. */
  metadataValue?: string
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
 *
 * A blank metadata key sends no metadata at all, because the server refuses an
 * empty key and there is no key-only search. A filled key with a blank value is
 * a real search for the empty string, which is what the proto's exact-pair
 * match means.
 */
export const toListTransactionsRequest = (controls: TransactionFilterControls = {}): ListTransactionsRequest => {
  const startDate = toTimestamp(controls.startDate)
  const endDate = toTimestamp(controls.endDate)
  const idempotencyKey = controls.idempotencyKey?.trim()
  const metadataKey = controls.metadataKey?.trim()

  const filter: TransactionFilter = {
    ...(idempotencyKey ? { idempotencyKey } : {}),
    ...(startDate ? { startDate } : {}),
    ...(endDate ? { endDate } : {}),
    ...(metadataKey ? { metadata: { [metadataKey]: controls.metadataValue?.trim() ?? '' } } : {})
  }

  return {
    ...(Object.keys(filter).length > 0 ? { filter } : {}),
    pageSize: Math.min(controls.pageSize ?? 10, MAX_PAGE_SIZE),
    pageNumber: controls.page ?? 1,
    orderByAscending: controls.sort === 'oldest'
  }
}
