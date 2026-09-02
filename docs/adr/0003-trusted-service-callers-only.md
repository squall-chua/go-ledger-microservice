# Trusted service callers only: no tenancy, no end-user authorization

The ledger holds a single book. It has no tenant partition, and the `user`
dimension of an account is a free-form label the ledger never authorizes on. The
only access control is a JWT scope check per RPC: `ledger:write` to record,
`ledger:read` to query. Any caller holding a valid token therefore reaches every
account in the book.

This is a deliberate deviation from the reference ledger this service is modelled
on, which partitions every row by a `namespace` claim taken from the token. We
dropped it because this service holds one book for one product, and isolation is
expected to live in the wrapping services that call it. Earlier in the same
design we tried making the account's `user` dimension an authenticated identity
instead; that was rejected because double-entry needs two legs, so any rule
permissive enough to allow a transfer between two users is also permissive enough
to let one debit the other.

## Consequences

- The ledger must not be exposed to end users. It expects a service token, and a
  service token in a browser is a service token anyone can read. The web UI
  currently calls it directly, which is accepted only while the whole deployment
  is private.
- Adding a tenant partition later is expensive: a column on all three tables,
  part of every primary key and index, and a backfill that must invent a value
  for existing rows. If a second product or a second customer's books are ever
  likely, that is the moment to reconsider — not after.
