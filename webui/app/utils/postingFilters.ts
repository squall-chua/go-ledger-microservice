import type { AccountFilter, AccountType, ListPostingsRequest, PostingFilter } from '../types/ledger'

/** The register's filter controls, exactly as the page holds them. */
export interface RegisterFilters {
  /** An AccountType name, or 'ALL' for not filtered. */
  accountType: string
  user: string
  name: string
  /** A currency code, or 'ALL' for not filtered. */
  currency: string
  /** datetime-local value; the transaction date the register starts at, inclusive. */
  startDate: string
  /** datetime-local value; the transaction date the register stops at, exclusive. */
  endDate: string
  metadataKey: string
  metadataValue: string
  /** Oldest transaction date first. */
  ascending: boolean
}

/** The server clamps a page size here, so never ask for more. */
export const MAX_PAGE_SIZE = 100

/**
 * Maps the filter controls onto a ListPostingsRequest.
 *
 * An empty control omits its key rather than sending '': AccountFilter.user and
 * .name are optional in the proto, so '' matches only the empty string. A blank
 * metadata key sends no metadata at all, because the server refuses one.
 */
export const toListPostingsRequest = (
  filters: RegisterFilters,
  pageNumber: number,
  pageSize: number
): ListPostingsRequest => {
  const account: AccountFilter = {}
  if (filters.accountType && filters.accountType !== 'ALL') {
    account.type = filters.accountType as AccountType
  }
  const user = filters.user.trim()
  if (user) {
    account.user = user
  }
  const name = filters.name.trim()
  if (name) {
    account.name = name
  }

  const filter: PostingFilter = {}
  if (Object.keys(account).length > 0) {
    filter.account = account
  }
  if (filters.currency && filters.currency !== 'ALL') {
    filter.currencyCode = filters.currency
  }
  if (filters.startDate) {
    filter.startDate = new Date(filters.startDate).toISOString()
  }
  if (filters.endDate) {
    filter.endDate = new Date(filters.endDate).toISOString()
  }
  const metadataKey = filters.metadataKey.trim()
  if (metadataKey) {
    filter.metadata = { [metadataKey]: filters.metadataValue.trim() }
  }

  const request: ListPostingsRequest = {
    pageSize: Math.min(pageSize, MAX_PAGE_SIZE),
    pageNumber,
    orderByAscending: filters.ascending
  }
  if (Object.keys(filter).length > 0) {
    request.filter = filter
  }
  return request
}
