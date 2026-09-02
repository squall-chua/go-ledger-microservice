import { describe, expect, it } from 'vitest'
import type { Money } from '../types/ledger'
import { formatMoney, isNegativeMoney, moneyToNumber } from './ledger'

const usd = (units: string, nanos: number): Money => ({ currencyCode: 'USD', units, nanos })

describe('moneyToNumber', () => {
  it('adds the fractional part carried in nanos', () => {
    expect(moneyToNumber(usd('2849', 500000000))).toBe(2849.5)
  })

  it('parses units from a string', () => {
    expect(moneyToNumber(usd('2000', 0))).toBe(2000)
  })

  it('is correct for a negative amount, where nanos is also negative', () => {
    expect(moneyToNumber(usd('-2', -500000000))).toBe(-2.5)
  })

  it('is zero for a missing money', () => {
    expect(moneyToNumber(undefined)).toBe(0)
  })
})

describe('isNegativeMoney', () => {
  it('is true when only nanos is negative', () => {
    expect(isNegativeMoney(usd('0', -500000000))).toBe(true)
  })

  it('is true for a negative units', () => {
    expect(isNegativeMoney(usd('-2', -500000000))).toBe(true)
  })

  it('is false for zero and for a positive amount', () => {
    expect(isNegativeMoney(usd('0', 0))).toBe(false)
    expect(isNegativeMoney(usd('0', 500000000))).toBe(false)
  })
})

describe('formatMoney', () => {
  it('shows the cents carried in nanos', () => {
    expect(formatMoney(usd('2849', 500000000))).toBe('$2,849.50')
  })

  it('formats a negative amount', () => {
    expect(formatMoney(usd('-2', -500000000))).toBe('-$2.50')
  })

  it('uses the currency code of the money itself', () => {
    expect(formatMoney({ currencyCode: 'EUR', units: '10', nanos: 250000000 })).toBe('€10.25')
  })
})
