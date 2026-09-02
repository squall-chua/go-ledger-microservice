import { describe, expect, it } from 'vitest'
import type { AccountBalance, Money, Posting, Transaction } from '../types/ledger'
import { toBalanceRows, toBalanceTotals, toPostingRows, toTransactionRows } from './ledgerRows'

const usd = (units: string, nanos = 0): Money => ({ currencyCode: 'USD', units, nanos })

const balance = (type: string, units: string): AccountBalance => ({
  account: { type: type as AccountBalance['account']['type'], user: 'alice', name: 'Checking' },
  balance: usd(units),
  updatedAt: '2026-09-02T01:26:24Z'
})

const posting = (over: Partial<Posting> = {}): Posting => ({
  id: 'p1',
  transactionId: 'abcdef0123456789',
  account: { type: 'ACCOUNT_TYPE_ASSETS', user: 'alice', name: 'Checking' },
  amount: usd('-2', -500000000),
  balance: usd('100'),
  createdAt: '2026-09-02T01:26:24Z',
  date: '2026-09-02T01:26:24Z',
  ...over
})

describe('toBalanceRows', () => {
  it('lays the account out in three parts with the type label resolved', () => {
    const [row] = toBalanceRows([balance('ACCOUNT_TYPE_ASSETS', '2849')])
    expect(row!.account).toEqual({ type: 'ASSETS', user: 'alice', name: 'Checking' })
  })

  it('leaves a name containing a colon untouched and never joins the parts', () => {
    // ':' is a literal character in a name, not a separator.
    // See docs/adr/0002-flat-account-names.md.
    const row = toBalanceRows([{
      ...balance('ACCOUNT_TYPE_ASSETS', '1'),
      account: { type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'a:b' }
    }])[0]!
    expect(row.account).toEqual({ type: 'ASSETS', user: '', name: 'a:b' })
  })

  it('formats the balance and says whether it is negative', () => {
    const [row] = toBalanceRows([balance('ACCOUNT_TYPE_LIABILITIES', '-2')])
    expect(row!.balance).toEqual({ text: '-$2.00', negative: true })
  })

  it('shows the update time as a local date and time', () => {
    const [row] = toBalanceRows([balance('ACCOUNT_TYPE_ASSETS', '1')])
    expect(row!.updatedAt).toBe(new Date('2026-09-02T01:26:24Z').toLocaleString())
  })

  it('shows nothing rather than Invalid Date when a row carries no time', () => {
    const [row] = toBalanceRows([{ ...balance('ACCOUNT_TYPE_ASSETS', '1'), updatedAt: '' }])
    expect(row!.updatedAt).toBe('')
  })
})

describe('toBalanceTotals', () => {
  it('adds up assets, incomes and expenses and ignores the other two types', () => {
    const totals = toBalanceTotals([
      balance('ACCOUNT_TYPE_ASSETS', '2000'),
      balance('ACCOUNT_TYPE_ASSETS', '849'),
      balance('ACCOUNT_TYPE_INCOMES', '-500'),
      balance('ACCOUNT_TYPE_EXPENSES', '120'),
      balance('ACCOUNT_TYPE_LIABILITIES', '900'),
      balance('ACCOUNT_TYPE_EQUITIES', '700')
    ], 'USD')
    // An income account carries a credit balance, which is negative in this
    // book, so the card shows the amount earned rather than the sign.
    expect(totals).toEqual({ assets: '$2,849.00', revenue: '$500.00', expenses: '$120.00' })
  })

  it('shows no revenue as zero rather than minus zero', () => {
    expect(toBalanceTotals([], 'USD').revenue).toBe('$0.00')
  })

  it('formats in the currency the page asked for', () => {
    expect(toBalanceTotals([balance('ACCOUNT_TYPE_ASSETS', '10')], 'EUR').assets).toBe('€10.00')
  })
})

describe('toPostingRows', () => {
  it('shortens the transaction id to eight characters', () => {
    const [row] = toPostingRows([posting()])
    expect(row!.transactionShortId).toBe('abcdef01')
  })

  it('formats the amount and the running balance', () => {
    const [row] = toPostingRows([posting()])
    expect(row!.amount).toEqual({ text: '-$2.50', negative: true })
    expect(row!.balance).toEqual({ text: '$100.00', negative: false })
  })

  it('shows the transaction date as a local date and time', () => {
    const [row] = toPostingRows([posting()])
    expect(row!.date).toBe(new Date('2026-09-02T01:26:24Z').toLocaleString())
  })
})

describe('toTransactionRows', () => {
  const transaction = (over: Partial<Transaction> = {}): Transaction => ({
    id: '0123456789abcdef',
    idempotencyKey: 'k',
    date: '2026-09-02T01:26:24Z',
    note: 'Coffee',
    metadata: { order_id: 'A-42' },
    postings: [posting()],
    createdAt: '2026-09-02T01:26:24Z',
    ...over
  })

  it('shortens the id, keeps the note, and maps the postings', () => {
    const [row] = toTransactionRows([transaction()])
    expect(row!.shortId).toBe('01234567')
    expect(row!.note).toBe('Coffee')
    expect(row!.postings[0]!.account).toEqual({ type: 'ASSETS', user: 'alice', name: 'Checking' })
  })

  it('lays metadata out as pairs so the template does not count keys', () => {
    const [row] = toTransactionRows([transaction()])
    expect(row!.metadata).toEqual([{ key: 'order_id', value: 'A-42' }])
  })

  it('has no metadata pairs when the transaction carries none', () => {
    const [row] = toTransactionRows([transaction({ metadata: undefined as unknown as Record<string, string> })])
    expect(row!.metadata).toEqual([])
  })
})
