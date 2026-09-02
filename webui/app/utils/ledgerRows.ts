import type { Account, AccountBalance, Money, Posting, Transaction } from '../types/ledger'
import { ACCOUNT_TYPES } from './constants'
import { formatAmount, formatMoney, isNegativeMoney, moneyToNumber } from './ledger'

/**
 * The wire read back into finished view rows. Building a request already has a
 * module each way (`toListAccountBalancesRequest` and friends); this is the
 * return trip, so a template holds strings and never a wire shape.
 *
 * Every rule the wire imposes lands here once: enum names rather than labels,
 * int64 as a string inside Money, RFC3339 timestamps, and an id long enough
 * that a table shows only its head.
 */

/**
 * The three parts of an Account, laid out for the caller. There is deliberately
 * no joined string: ':' is a literal character in a name, not a separator
 * (docs/adr/0002-flat-account-names.md), so a joined form cannot be split back.
 * An empty user or name stays empty — '*' is a real value, not a wildcard.
 */
export interface AccountCell {
  type: string
  user: string
  name: string
}

/** An amount ready to print, with the sign pulled out so a template can colour it. */
export interface MoneyCell {
  text: string
  negative: boolean
}

export interface BalanceRow {
  account: AccountCell
  balance: MoneyCell
  updatedAt: string
}

/** The three Overview cards, already formatted in the currency on screen. */
export interface BalanceTotals {
  assets: string
  revenue: string
  expenses: string
}

export interface PostingRow {
  id: string
  transactionShortId: string
  date: string
  account: AccountCell
  amount: MoneyCell
  balance: MoneyCell
}

export interface TransactionRow {
  id: string
  shortId: string
  date: string
  note: string
  metadata: Array<{ key: string, value: string }>
  postings: PostingRow[]
}

/** The five labels already live in ACCOUNT_TYPES, so the wire name reads from there. */
const toAccountCell = (account?: Account | null): AccountCell => ({
  type: ACCOUNT_TYPES.find(t => t.value === account?.type)?.label.toUpperCase() ?? 'UNKNOWN',
  user: account?.user ?? '',
  name: account?.name ?? ''
})

const toMoneyCell = (money?: Money | null): MoneyCell => ({
  text: formatMoney(money),
  negative: isNegativeMoney(money)
})

/** Empty rather than 'Invalid Date' when a row arrives without its timestamp. */
const toLocalDateTime = (timestamp?: string | null): string =>
  timestamp ? new Date(timestamp).toLocaleString() : ''

/** Enough of a UUID to tell two rows apart, which is all a table needs. */
const toShortId = (id?: string | null): string => (id ?? '').substring(0, 8)

export const toBalanceRows = (balances: AccountBalance[]): BalanceRow[] =>
  balances.map(balance => ({
    account: toAccountCell(balance.account),
    balance: toMoneyCell(balance.balance),
    updatedAt: toLocalDateTime(balance.updatedAt)
  }))

/**
 * An income account holds a credit balance, which is negative in this book, so
 * the revenue card flips the sign to show what was earned. Zero is flipped back
 * to zero: -0 formats as '-$0.00'.
 */
export const toBalanceTotals = (balances: AccountBalance[], currencyCode: string): BalanceTotals => {
  let assets = 0, revenue = 0, expenses = 0
  for (const balance of balances) {
    const amount = moneyToNumber(balance.balance)
    switch (balance.account?.type) {
      case 'ACCOUNT_TYPE_ASSETS':
        assets += amount
        break
      case 'ACCOUNT_TYPE_INCOMES':
        revenue += amount
        break
      case 'ACCOUNT_TYPE_EXPENSES':
        expenses += amount
        break
    }
  }
  return {
    assets: formatAmount(assets, currencyCode),
    revenue: formatAmount(revenue === 0 ? 0 : -revenue, currencyCode),
    expenses: formatAmount(expenses, currencyCode)
  }
}

export const toPostingRows = (postings: Posting[]): PostingRow[] =>
  postings.map(posting => ({
    id: posting.id,
    transactionShortId: toShortId(posting.transactionId),
    date: toLocalDateTime(posting.date),
    account: toAccountCell(posting.account),
    amount: toMoneyCell(posting.amount),
    balance: toMoneyCell(posting.balance)
  }))

export const toTransactionRows = (transactions: Transaction[]): TransactionRow[] =>
  transactions.map(transaction => ({
    id: transaction.id,
    shortId: toShortId(transaction.id),
    date: toLocalDateTime(transaction.date),
    note: transaction.note,
    metadata: Object.entries(transaction.metadata ?? {}).map(([key, value]) => ({ key, value })),
    postings: toPostingRows(transaction.postings ?? [])
  }))
