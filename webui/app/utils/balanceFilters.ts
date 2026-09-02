import type { AccountFilter, AccountType, ListAccountBalancesRequest } from '../types/ledger'

/** What the four filter controls hold. 'ALL' is the not-filtered option. */
export interface BalanceFilterControls {
  type: AccountType | 'ALL'
  currency: string
  user: string
  name: string
}

/**
 * Maps the filter controls to a request. Every filter is optional and the
 * server matches what is present exactly, so a control left alone must leave
 * its key out: sending '' would ask for accounts whose user or name is the
 * empty string. No filters at all is the Trial balance.
 */
export const toListAccountBalancesRequest = (controls: BalanceFilterControls): ListAccountBalancesRequest => {
  const account: AccountFilter = {}
  if (controls.type !== 'ALL' && controls.type !== 'ACCOUNT_TYPE_UNSPECIFIED') {
    account.type = controls.type
  }
  if (controls.user) {
    account.user = controls.user
  }
  if (controls.name) {
    account.name = controls.name
  }

  const request: ListAccountBalancesRequest = {}
  if (Object.keys(account).length > 0) {
    request.account = account
  }
  if (controls.currency && controls.currency !== 'ALL') {
    request.currencyCode = controls.currency
  }
  return request
}
