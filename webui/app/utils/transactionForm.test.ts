import { describe, expect, it } from 'vitest'
import type { TransactionFormValues } from './transactionForm'
import { amountToMoney, describeLedgerError, toRecordTransactionRequest, validateTransactionForm } from './transactionForm'

const valuesWith = (overrides: Partial<TransactionFormValues> = {}): TransactionFormValues => ({
  note: 'Correcting entry',
  date: '',
  idempotencyKey: 'key-1',
  postings: [
    { type: 'ACCOUNT_TYPE_ASSETS', user: 'alice', name: 'Checking', amount: 2.5, currency: 'USD' },
    { type: 'ACCOUNT_TYPE_EXPENSES', user: 'alice', name: 'Groceries', amount: -2.5, currency: 'USD' }
  ],
  metadata: [],
  ...overrides
})

describe('validateTransactionForm', () => {
  it('has nothing to say about a balanced entry', () => {
    expect(validateTransactionForm(valuesWith())).toEqual([])
  })

  it('refuses fewer than two postings', () => {
    const values = valuesWith({
      postings: [{ type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'Checking', amount: 0, currency: 'USD' }]
    })
    expect(validateTransactionForm(values)).toContain('A transaction needs at least two postings.')
  })

  it('refuses an empty note', () => {
    expect(validateTransactionForm(valuesWith({ note: '   ' }))).toContain('A note is required.')
  })

  it('refuses a mixture of currencies', () => {
    const values = valuesWith({
      postings: [
        { type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'Checking', amount: 2.5, currency: 'USD' },
        { type: 'ACCOUNT_TYPE_EXPENSES', user: '', name: 'Groceries', amount: -2.5, currency: 'EUR' }
      ]
    })
    expect(validateTransactionForm(values)).toContain('A transaction carries a single currency.')
  })

  it('refuses a sum that is not zero', () => {
    const values = valuesWith({
      postings: [
        { type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'Checking', amount: 2.5, currency: 'USD' },
        { type: 'ACCOUNT_TYPE_EXPENSES', user: '', name: 'Groceries', amount: -2.25, currency: 'USD' }
      ]
    })
    expect(validateTransactionForm(values)).toContain('The postings must sum to zero. They sum to 0.25.')
  })

  it('refuses a posting with a zero amount', () => {
    const values = valuesWith({
      postings: [
        { type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'Checking', amount: 0, currency: 'USD' },
        { type: 'ACCOUNT_TYPE_EXPENSES', user: '', name: 'Groceries', amount: 0, currency: 'USD' }
      ]
    })
    expect(validateTransactionForm(values)).toContain('Every posting needs an amount other than zero.')
  })

  it('sees a residue below a cent that a two-decimal sum would call zero', () => {
    const values = valuesWith({
      postings: [
        { type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'Checking', amount: 2.5, currency: 'USD' },
        { type: 'ACCOUNT_TYPE_EXPENSES', user: '', name: 'Groceries', amount: -2.499999999, currency: 'USD' }
      ]
    })
    expect(validateTransactionForm(values)).toContain('The postings must sum to zero. They sum to 1e-9.')
  })
})

describe('amountToMoney', () => {
  it('carries the fractional part in nanos', () => {
    expect(amountToMoney(2.5, 'USD')).toEqual({ currencyCode: 'USD', units: '2', nanos: 500000000 })
  })

  it('gives units and nanos the same sign for a negative amount', () => {
    expect(amountToMoney(-2.5, 'USD')).toEqual({ currencyCode: 'USD', units: '-2', nanos: -500000000 })
  })

  it('keeps a fraction the float arithmetic would otherwise spoil', () => {
    expect(amountToMoney(0.1, 'USD')).toEqual({ currencyCode: 'USD', units: '0', nanos: 100000000 })
    expect(amountToMoney(-0.29, 'EUR')).toEqual({ currencyCode: 'EUR', units: '0', nanos: -290000000 })
  })

  it('sends units as a string, because it is int64 on the wire', () => {
    expect(amountToMoney(2000, 'USD').units).toBe('2000')
  })
})

describe('toRecordTransactionRequest', () => {
  it('sends an empty user as the empty string rather than a star', () => {
    const values = valuesWith({
      postings: [
        { type: 'ACCOUNT_TYPE_ASSETS', user: '', name: 'Checking', amount: 1, currency: 'USD' },
        { type: 'ACCOUNT_TYPE_EXPENSES', user: '', name: 'Groceries', amount: -1, currency: 'USD' }
      ]
    })
    expect(toRecordTransactionRequest(values).postings[0]!.account).toEqual({
      type: 'ACCOUNT_TYPE_ASSETS',
      user: '',
      name: 'Checking'
    })
  })

  it('drops a metadata row with a blank key and sends the rest as string pairs', () => {
    const values = valuesWith({
      metadata: [
        { key: 'source', value: 'manual' },
        { key: '  ', value: 'ignored' }
      ]
    })
    expect(toRecordTransactionRequest(values).metadata).toEqual({ source: 'manual' })
  })

  it('omits metadata and date entirely when there is none', () => {
    const request = toRecordTransactionRequest(valuesWith())
    expect(request.metadata).toBeUndefined()
    expect(request.date).toBeUndefined()
  })

  it('sends a supplied date as an RFC3339 timestamp', () => {
    const request = toRecordTransactionRequest(valuesWith({ date: '2026-09-02T10:30' }))
    expect(request.date).toBe(new Date('2026-09-02T10:30').toISOString())
  })
})

describe('describeLedgerError', () => {
  it('reads the gateway status message', () => {
    const err = { response: { _data: { code: 3, message: 'postings do not sum to zero (sum is 0.25)' } } }
    expect(describeLedgerError(err)).toBe('postings do not sum to zero (sum is 0.25)')
  })

  it('falls back to the thrown error, then to a plain sentence', () => {
    expect(describeLedgerError(new Error('Failed to fetch'))).toBe('Failed to fetch')
    expect(describeLedgerError({})).toBe('The ledger refused the transaction.')
  })
})
