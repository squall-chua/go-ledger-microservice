# Flat account names

An account is identified by the composite `(type, user, name)`, stored as three
exact-match columns. `name` is opaque: it is never split on a separator, never
treated as a path, and never rolled up into a parent. Punctuation such as `:` is
a literal character in the name.

This reverses what the README previously promised. The service began as a
ledger-cli work-alike with colon-delimited hierarchical accounts and wildcard
matching, which meant `*` was stored inside the data as both a real value and a
match-anything — so a balance read became a `LIKE` scan and the non-negative
balance check became in-memory regex matching. Exact columns turn both into
index lookups.

## Consequences

- There is no sub-tree roll-up. Asking for `Assets` returns balances filtered by
  account **type**, not the sum of everything beneath a prefix.
- A caller that wants hierarchy builds it above this service, out of the three
  dimensions it already has.
