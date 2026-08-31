# Forward-only transaction dates

Every posting stores the running balance of its account after that leg, so that
balance is only truthful if postings are applied in date order. We therefore
reject any transaction whose date falls before the latest posting date of an
account it touches (tracked as `last_date` on the balance snapshot and checked
under a row lock during the write), rather than recomputing history behind it.

## Consequences

- A mistake is corrected with a reversing transaction, never a backdated one.
  There is no way to insert a transaction into the past.
- A supplied date more than five minutes ahead of now is also rejected. Without
  that cap one bad client clock parks a posting far in the future and every
  later write to that account is rejected as backdated — unrecoverable in an
  append-only ledger.
- A stamped date (the caller omitted one) advances past `last_date` rather than
  being rejected. A caller who supplied no date must never be told its date was
  wrong, and this absorbs clock skew between ledger instances.
