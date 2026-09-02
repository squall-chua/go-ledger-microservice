// Hand-written against api/proto/v1/ledger.proto.
//
// The gateway uses the default protojson marshaler, so the wire is
// lowerCamelCase (currencyCode, orderByAscending, pageSize, totalCount, ...),
// enums are their string names, timestamps are RFC3339 strings, and 64-bit
// integers (Money.units, totalCount) are JSON strings.
//
// Unknown fields are silently dropped by the server, so a misspelt field name
// never fails at runtime — these types are the only thing that catches it.

/** RFC3339 UTC timestamp, e.g. '2026-09-02T01:26:24.196084Z'. */
export type Timestamp = string

export type AccountType
  = | 'ACCOUNT_TYPE_UNSPECIFIED'
    | 'ACCOUNT_TYPE_ASSETS'
    | 'ACCOUNT_TYPE_LIABILITIES'
    | 'ACCOUNT_TYPE_EQUITIES'
    | 'ACCOUNT_TYPE_INCOMES'
    | 'ACCOUNT_TYPE_EXPENSES'

/**
 * An Account is the exact composite (type, user, name). `name` is opaque: it is
 * never split, never a path, never rolled up. See docs/adr/0002-flat-account-names.md.
 */
export interface Account {
  type: AccountType
  user: string
  name: string
}

/** google.type.Money. `units` is int64, so protojson sends it as a string. */
export interface Money {
  currencyCode: string
  units: string
  nanos: number
}

/**
 * Narrows a read to Accounts whose fields match exactly. An omitted field is
 * not filtered on at all; a field set to '' matches only the empty string.
 * Nothing here is ever a wildcard.
 */
export interface AccountFilter {
  type?: AccountType
  user?: string
  name?: string
}

export interface AccountBalance {
  account: Account
  balance: Money
  updatedAt: Timestamp
}

export interface Posting {
  id: string
  transactionId: string
  account: Account
  amount: Money
  balance: Money
  createdAt: Timestamp
  /** Transaction date: supplied by the caller or stamped by the ledger. */
  date: Timestamp
}

export interface Transaction {
  id: string
  idempotencyKey: string
  date: Timestamp
  note: string
  metadata: Record<string, string>
  postings: Posting[]
  createdAt: Timestamp
}

/** Date ranges are half-open: startDate is included, endDate is excluded. */
export interface TransactionFilter {
  idempotencyKey?: string
  startDate?: Timestamp
  endDate?: Timestamp
  metadata?: Record<string, string>
}

export interface PostingFilter {
  account?: AccountFilter
  currencyCode?: string
  startDate?: Timestamp
  endDate?: Timestamp
  metadata?: Record<string, string>
}

export interface ListAccountBalancesRequest {
  account?: AccountFilter
  currencyCode?: string
}

export interface ListAccountBalancesResponse {
  balances: AccountBalance[]
}

export interface ListTransactionsRequest {
  filter?: TransactionFilter
  /** Defaults to 10, clamped to at most 100. */
  pageSize?: number
  /** One-based; below one is the first page. */
  pageNumber?: number
  /** Oldest transaction date first. Default is newest first. */
  orderByAscending?: boolean
}

export interface ListTransactionsResponse {
  transactions: Transaction[]
  /** int64 on the wire, so a string. */
  totalCount: string
}

export interface ListPostingsRequest {
  filter?: PostingFilter
  pageSize?: number
  pageNumber?: number
  orderByAscending?: boolean
}

export interface ListPostingsResponse {
  postings: Posting[]
  totalCount: string
}

export interface PostingInput {
  account: Account
  amount: Money
}

export interface RecordTransactionRequest {
  idempotencyKey: string
  /** Omitted means the ledger stamps now. More than 5 minutes ahead is refused. */
  date?: Timestamp
  note: string
  metadata?: Record<string, string>
  postings: PostingInput[]
  /** Complete accounts this transaction touches; anything else is refused. */
  verifyNonNegativeBalances?: Account[]
}

export interface RecordTransactionResponse {
  transaction: Transaction
  /** True when the idempotency key replayed an already-recorded transaction. */
  replayed: boolean
}
