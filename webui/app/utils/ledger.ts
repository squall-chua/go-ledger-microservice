import type { Money } from '../types/ledger'

/**
 * The decimal value of a Money: units + nanos / 1e9, exactly as
 * internal/moneyfmt/moneyfmt.go computes it. Both parts carry the sign, so
 * -2.50 arrives as units '-2' and nanos -500000000.
 */
export const moneyToNumber = (money?: Money | null): number => {
  return Number(money?.units ?? 0) + (money?.nanos ?? 0) / 1e9
}

export const isNegativeMoney = (money?: Money | null): boolean => {
  return moneyToNumber(money) < 0
}

/** A plain decimal as currency. The Overview cards total up before formatting. */
export const formatAmount = (amount: number, currencyCode: string): string => {
  return new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: currencyCode || 'USD'
  }).format(amount)
}

export const formatMoney = (money?: Money | null): string => {
  return formatAmount(moneyToNumber(money), money?.currencyCode ?? '')
}
