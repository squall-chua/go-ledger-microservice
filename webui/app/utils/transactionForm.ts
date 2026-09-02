import type { AccountType, Money, RecordTransactionRequest } from '../types/ledger'

/**
 * One posting row as the form holds it. `user` and `name` are exact values: an
 * empty user is a real, exactly-matched account segment, never a wildcard
 * (docs/adr/0002-flat-account-names.md).
 */
export interface PostingRow {
  type: AccountType
  user: string
  name: string
  amount: number
  currency: string
}

/** One metadata row. A blank key is dropped: the server refuses an empty key. */
export interface MetadataRow {
  key: string
  value: string
}

export interface TransactionFormValues {
  note: string
  /** A `datetime-local` value. Blank leaves the date for the ledger to stamp. */
  date: string
  idempotencyKey: string
  postings: PostingRow[]
  metadata: MetadataRow[]
}

/**
 * The amount as whole nanos, which is the ledger's resolution
 * (internal/moneyfmt/moneyfmt.go). Rounding the whole amount once avoids the
 * float error in taking the fractional part first.
 *
 * ponytail: a number over ~9,007,199 units cannot hold nine decimals exactly;
 * move to BigInt or a decimal string if the book ever carries amounts that big.
 */
export const amountToNanos = (amount: number): number => Math.round((amount || 0) * 1e9)

/**
 * Splits an amount into google.type.Money. `units` is int64 on the wire, so a
 * string, and both parts carry the sign — the server refuses a Money whose
 * units and nanos disagree, so -2.50 is units '-2' with nanos -500000000.
 */
export const amountToMoney = (amount: number, currencyCode: string): Money => {
  const total = amountToNanos(amount)
  return {
    currencyCode,
    units: String(Math.trunc(total / 1e9)),
    nanos: total % 1e9
  }
}

/**
 * The reasons the ledger would refuse this entry, read straight off the form.
 * Empty means nothing local objects — the ledger is still the authority.
 *
 * The sum is checked on the same whole nanos that are sent, so the form and the
 * server cannot disagree about what zero is.
 */
export const validateTransactionForm = (values: TransactionFormValues): string[] => {
  const reasons: string[] = []

  if (values.note.trim() === '') {
    reasons.push('A note is required.')
  }

  if (values.postings.length < 2) {
    reasons.push('A transaction needs at least two postings.')
  }

  if (values.postings.some(posting => amountToNanos(posting.amount) === 0)) {
    reasons.push('Every posting needs an amount other than zero.')
  }

  if (new Set(values.postings.map(posting => posting.currency)).size > 1) {
    reasons.push('A transaction carries a single currency.')
  }

  const sum = values.postings.reduce((total, posting) => total + amountToNanos(posting.amount), 0)
  if (sum !== 0) {
    reasons.push(`The postings must sum to zero. They sum to ${sum / 1e9}.`)
  }

  return reasons
}

/**
 * The form's values as the request the ledger reads. A blank date is omitted so
 * the ledger stamps it, and a metadata row with a blank key is dropped.
 */
export const toRecordTransactionRequest = (values: TransactionFormValues): RecordTransactionRequest => {
  const metadata: Record<string, string> = {}
  for (const row of values.metadata) {
    const key = row.key.trim()
    if (key !== '') {
      metadata[key] = row.value.trim()
    }
  }

  return {
    idempotencyKey: values.idempotencyKey.trim(),
    ...(values.date ? { date: new Date(values.date).toISOString() } : {}),
    note: values.note.trim(),
    ...(Object.keys(metadata).length > 0 ? { metadata } : {}),
    postings: values.postings.map(posting => ({
      account: {
        type: posting.type,
        user: posting.user,
        name: posting.name
      },
      amount: amountToMoney(posting.amount, posting.currency)
    }))
  }
}

/**
 * A rejection from the ledger as something the operator can read. The gateway
 * puts the gRPC status body on `err.response._data`, which ofetch hands back on
 * the thrown error.
 */
export const describeLedgerError = (err: any): string => {
  return err?.response?._data?.message || err?.message || 'The ledger refused the transaction.'
}
