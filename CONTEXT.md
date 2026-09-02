# Ledger

A double-entry accounting ledger service. It records balanced, immutable
transactions and answers balance and register queries. It is a **single book** —
it holds no tenant partition, owns no end-user identity, and is reached only by
trusted backend services, never by an end user directly.

## Language

**Account**:
An implicit balance bucket identified by the composite `(type, user, name)`. It
has no row of its own — it exists once a posting references it.
_Avoid_: Ledger account, GL account, wallet.

**Account type**:
One of the five double-entry categories: `Assets`, `Liabilities`, `Equities`,
`Incomes`, `Expenses`. Always one of the five — there is no unspecified or
match-anything type.
_Avoid_: Category, class.

**User** (account dimension):
Free-form caller-supplied owner of an account, part of its identity and matched
exactly. **Not** an authenticated identity — the ledger never authorizes on it.
_Avoid_: Owner, end-user, principal.

**Service caller**:
The only kind of caller the ledger accepts: a trusted backend service holding a
service token. The ledger never sees an end user, so every caller reaches the
whole book.
_Avoid_: Client, consumer, user (when you mean the caller).

**Name** (account dimension):
The opaque, flat account label (e.g. `Checking`). Matched exactly; never split,
never rolled up into a parent. Punctuation such as `:` is a literal character.
_Avoid_: Path, hierarchy, sub-account.

**Transaction**:
An immutable, balanced set of postings recorded together. Append-only; correct a
mistake by recording a reversing transaction, never by editing.
_Avoid_: Entry, journal entry, txn (in prose).

**Posting**:
One leg of a transaction: an amount applied to one account, carrying the
account's running `balance` after that leg.
_Avoid_: Line, split, leg.

**Transaction date**:
The single instant a transaction is treated as having occurred. Carried by every
posting in it, and fixes their place in the Register. Either **supplied** by the
caller or **stamped** by the ledger when omitted.
_Avoid_: Timestamp, created_at, effective date, booking date.

**Supplied date**:
A transaction date given by the caller — a claim about when the event happened.
Policed: never before an affected account's latest posting, and never more than a
small clock-skew tolerance into the future.
_Avoid_: Client date, user date, effective date.

**Stamped date**:
A transaction date the ledger assigns when the caller omits one. Not a claim
about the world but the transaction's **position** in the affected accounts'
order — so the ledger owns it, and advances it past any posting it would
otherwise precede.
_Avoid_: Now, wall-clock time, created_at.

**Backdating**:
Supplying a transaction date earlier than the latest posting date of an account
the transaction touches. Rejected, so a posting's stored running balance always
reflects its chronological position.
_Avoid_: Amending, correcting, late entry.

**Register**:
The list of a single account's postings over time, each with its running balance.
_Avoid_: History, statement, ledger (the word for the whole service).

**Balance snapshot**:
The current balance held per account and currency, kept consistent with the
postings on every write so a balance read costs one lookup.
_Avoid_: Cache, materialized view.

**Trial balance**:
The full flat list of every account's current balance across all account types.
_Avoid_: Report, summary.

**Idempotency replay**:
Re-recording a transaction with the same `idempotencyKey` and identical content
returns the original rather than creating a duplicate. The same key with
different content is rejected.
_Avoid_: Retry, dedupe.

**Money**:
An amount as `{ currencyCode, units, nanos }` (Google `type.Money`). A single
transaction carries a single currency.
_Avoid_: Amount, decimal, value.
