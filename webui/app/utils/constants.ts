/** The page size the paginated pages ask for. */
export const PAGE_SIZE = 50

/**
 * The server clamps a larger page size down to this
 * (internal/repository/postgres.go), so never ask for more.
 */
export const MAX_PAGE_SIZE = 100

export const CURRENCIES = ['USD', 'EUR', 'GBP', 'MYR', 'SGD', 'JPY', 'AUD', 'CAD']

export const CURRENCY_OPTIONS = [
  { label: 'All Currencies', value: 'ALL' },
  ...CURRENCIES.map(c => ({ label: c, value: c }))
]

export const ACCOUNT_TYPES = [
  { label: 'Assets', value: 'ACCOUNT_TYPE_ASSETS' },
  { label: 'Liabilities', value: 'ACCOUNT_TYPE_LIABILITIES' },
  { label: 'Equities', value: 'ACCOUNT_TYPE_EQUITIES' },
  { label: 'Incomes', value: 'ACCOUNT_TYPE_INCOMES' },
  { label: 'Expenses', value: 'ACCOUNT_TYPE_EXPENSES' }
]
