# Go Ledger Microservice

A double-entry accounting ledger service.
It records balanced, immutable transactions and answers balance and register
queries over gRPC and REST, with Postgres behind it.

It holds a **single book**.
There is no tenant partition and no end-user identity.
The only caller it accepts is a **Service caller**: a trusted backend service
holding a service token.
Every valid token therefore reaches every account in the book, so the ledger must
not be exposed to end users.
See [`docs/adr/0003-trusted-service-callers-only.md`](docs/adr/0003-trusted-service-callers-only.md).

The words used here are defined once in [`CONTEXT.md`](CONTEXT.md).

## Inspired by ledger-cli

This project takes two things from [ledger-cli](https://ledger-cli.org/):

- **The double-entry principle.** A Transaction is an immutable set of Postings
  that must sum to exactly zero. A mistake is corrected by recording a reversing
  Transaction, never by editing what is already there.
- **The command names.** The client tool keeps `post`, `balance` and `register`,
  so the shape of a session is familiar.

It deliberately does **not** take ledger-cli's account hierarchy.
ledger-cli reads `Assets:Checking` as a path and rolls balances up the prefix
tree.
This service does not: an Account name is flat and opaque, and there is no
roll-up of any kind.
That reverses what this README used to promise, and the reasons are in
[`docs/adr/0002-flat-account-names.md`](docs/adr/0002-flat-account-names.md).

The other difference is the storage.
ledger-cli edits a local text file; this service appends to Postgres and serves
many callers at once.

## What an Account is

An Account is identified by the exact composite `(type, user, name)`.

- **Account type** is one of the five double-entry categories: `Assets`,
  `Liabilities`, `Equities`, `Incomes`, `Expenses`. There is no sixth value and
  no match-anything type.
- **User** is a free-form owner label supplied by the caller. It is part of the
  account's identity and is matched exactly. It is **not** an authenticated
  identity — the ledger never authorizes on it.
- **Name** is an opaque, flat label such as `Checking`. It is never split on a
  separator, never treated as a path, and never rolled up into a parent. A `:`
  inside a name is a literal character of that name, nothing more.

All three fields are matched exactly.
There are no wildcards and no prefixes.
Filtering a read by `Assets` filters on the account **type**; it is not the sum
of everything under a prefix.
A caller that wants a hierarchy builds it above this service out of the three
dimensions it already has.

An Account has no row of its own.
It comes into existence the moment a Posting references it.

Amounts are Money — `{ currencyCode, units, nanos }` — and a single Transaction
carries a single currency.

## Running the server

Postgres is the only datastore.

```bash
go run ./cmd/server \
  --port 8080 \
  --database-url "postgres://postgres:postgres@localhost:5432/ledger?sslmode=disable" \
  --jwt-secret super-secret-key
```

The schema is applied at startup, before the server accepts traffic, so
deploying is one step.
One port serves both gRPC and REST, and Prometheus metrics are on `/metrics`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--port` | `8080` | Port serving gRPC and REST together |
| `--database-url` | `postgres://postgres:postgres@localhost:5432/ledger?sslmode=disable` | Postgres connection URL |
| `--jwt-secret` | `super-secret-key` | Symmetric key a service token is verified with |
| `--cors-origins` | `*` | Comma-separated allowed CORS origins |

## Tokens and scopes

Every RPC except the gRPC health check needs a bearer token in the
`authorization` header.
The token is a JWT signed with HS256 using `--jwt-secret`.

Permissions are read from the standard `scope` claim, one space-delimited
string:

```json
{
  "scope": "ledger:read ledger:write"
}
```

An integrating service needs:

- `ledger:write` to record a Transaction.
- `ledger:read` for the three queries: account balances, transactions and
  postings.

`ledger:write` does **not** imply `ledger:read`.
The two scopes stand alone, so a service that both records and reads is issued
both.
A read attempted with a write-only token is refused:

```
the ledger rejected the request: missing required scope [ledger:read] [PermissionDenied]
```

That check is the whole of the access control.
The ledger does not narrow what a caller may see, so a valid token reaches every
Account in the book.

For local testing you can mint a token at [jwt.io](https://jwt.io/): algorithm
`HS256`, the payload above, and the server's `--jwt-secret` as the signing
secret.

## The CLI

`cmd/cli` is a gRPC client of a running server.
It keeps no datastore of its own and never opens the database: every command
dials the server and calls the same RPCs as any other Service caller, so it goes
through the ledger's own validation and its scope checks.

```bash
go build -o ledger-cli ./cmd/cli
export LEDGER_ADDR=localhost:8080       # or the -addr flag; this is the default
export LEDGER_TOKEN="your_token_here"   # or the -token flag; no default
```

The commands:

- `post NOTE POSTING POSTING...` — record a Transaction, one posting per leg.
- `balance [TYPE]` — print the Trial balance, or one account type's balances.
- `register TYPE:USER:NAME` — print one Account's Register in date order.

A posting argument is `TYPE:USER:NAME:AMOUNT+CURRENCY`, four exact parts, as in
`ASSETS:alice:Checking:-150.50+USD`.
The type is matched case-insensitively against the five categories.
The user and the name are opaque and taken exactly as written.

### Record a Transaction

```bash
./ledger-cli post "Opening balance" \
  ASSETS:alice:Checking:1000+USD \
  EQUITIES:alice:OpeningBalance:-1000+USD
```

```
recorded 01a05fb9-3acb-7ef8-8ae8-7a9fa1e46204  2026-09-02T09:26:09+08:00  Opening balance
  ASSETS:alice:Checking                    1000 USD  balance 1000 USD
  EQUITIES:alice:OpeningBalance           -1000 USD  balance -1000 USD
```

Each line shows the Posting's amount and the running balance of its Account
after that leg.
`post` generates an idempotency key when you pass none; the next section covers
passing your own.

```bash
./ledger-cli post -idempotency-key groceries-1 Groceries \
  EXPENSES:alice:Grocery:150.50+USD \
  ASSETS:alice:Checking:-150.50+USD
```

```
recorded 01a05fb9-3b05-7ce3-b0f0-ca79d8cd9ebf  2026-09-02T09:26:09+08:00  Groceries
  EXPENSES:alice:Grocery                  150.5 USD  balance 150.5 USD
  ASSETS:alice:Checking                  -150.5 USD  balance 849.5 USD
```

Postings that do not sum to zero are refused, and nothing is written:

```bash
./ledger-cli post "Unbalanced" ASSETS:alice:Checking:100+USD EXPENSES:alice:Food:-50+USD
```

```
the ledger rejected the request: postings do not sum to zero (sum is 50) [InvalidArgument]
```

### Read the balances

`balance` with no argument prints the Trial balance: every Account's current
balance, flat, across all account types.

```bash
./ledger-cli balance
```

```
ASSETS:alice:Checking                   849.5 USD
EQUITIES:alice:OpeningBalance           -1000 USD
EXPENSES:alice:Grocery                  150.5 USD
```

The optional argument is an account type, not a prefix:

```bash
./ledger-cli balance Assets
```

```
ASSETS:alice:Checking                   849.5 USD
```

`-user`, `-name` and `-currency` narrow it further, each on an exact value.

### Read one Account's Register

`register` takes one complete Account as `TYPE:USER:NAME`.
It prints that Account's Postings in Transaction date order, each with its
running balance and the Transaction it belongs to.

```bash
./ledger-cli register ASSETS:alice:Checking
```

```
2026-09-02T09:26:09+08:00         1000 USD  balance         1000 USD  01a05fb9-3acb-7ef8-8ae8-7a9fa1e46204
2026-09-02T09:26:09+08:00       -150.5 USD  balance        849.5 USD  01a05fb9-3b05-7ce3-b0f0-ca79d8cd9ebf
```

`-reverse` lists the newest Transaction date first.

## Transaction dates

A Transaction date is the single instant a Transaction is treated as having
occurred.
Every Posting in it carries that date, and it fixes their place in the Register.
It is either **supplied** by the caller or **stamped** by the ledger when the
caller omits one.

Dates are **forward-only**.
Every Posting stores the running balance of its Account after that leg, and that
balance is only truthful if Postings are applied in date order.

- **Backdating is rejected.** A supplied date earlier than the latest Posting
  date of any Account the Transaction touches is refused, and nothing is
  written. Correct a mistake with a reversing Transaction; there is no way to
  insert a Transaction into the past.
- **The future is capped at five minutes.** A supplied date more than five
  minutes ahead of now is refused. Without that tolerance one wrong client clock
  could park a Posting far in the future, and every later write to that Account
  would then be refused as backdated — unrecoverable in an append-only ledger.
  Inside the five minutes a date is accepted, which absorbs ordinary clock skew.
- **A stamped date advances instead of failing.** When the caller supplies no
  date the ledger assigns one. That date is not a claim about the world but the
  Transaction's position in the affected Accounts' order, so the ledger owns it:
  the stamped date is advanced strictly past the latest Posting date of those
  Accounts whenever now is not already past it. A caller who supplied no date is
  never told its date was wrong.

See [`docs/adr/0001-forward-only-transaction-dates.md`](docs/adr/0001-forward-only-transaction-dates.md).

## Idempotency replay

Every Transaction carries an `idempotencyKey`.
Sending the same key again with **identical content** is a **replay**: the
original Transaction is returned and nothing new is written, so a retried
request records the money exactly once.

```bash
./ledger-cli post -idempotency-key groceries-1 Groceries \
  EXPENSES:alice:Grocery:150.50+USD \
  ASSETS:alice:Checking:-150.50+USD
```

```
replayed 01a05fb9-3b05-7ce3-b0f0-ca79d8cd9ebf  2026-09-02T09:26:09+08:00  Groceries
  EXPENSES:alice:Grocery                  150.5 USD  balance 150.5 USD
  ASSETS:alice:Checking                  -150.5 USD  balance 849.5 USD
```

Note the same Transaction id and the same balances as the original record above.
A replay is a success, not a failure.
Over the API the response carries the original Transaction and a `replayed`
boolean: `true` here, `false` on a fresh record.

Sending the same key with **different content** is a different request wearing a
used key, so it is rejected with `AlreadyExists` and nothing is written:

```bash
./ledger-cli post -idempotency-key groceries-1 Groceries \
  EXPENSES:alice:Grocery:200+USD \
  ASSETS:alice:Checking:-200+USD
```

```
idempotency key "groceries-1" was already recorded with different content, so nothing was recorded
```

## The HTTP API

The same RPCs are reachable over REST, on the same port.

| RPC | Path | Scope |
| --- | --- | --- |
| `RecordTransaction` | `POST /v1/ledger/transactions` | `ledger:write` |
| `ListAccountBalances` | `POST /v1/ledger/accounts/balance` | `ledger:read` |
| `ListTransactions` | `POST /v1/ledger/transactions/query` | `ledger:read` |
| `ListPostings` | `POST /v1/ledger/postings/query` | `ledger:read` |

### Record a Transaction

```bash
curl -X POST http://localhost:8080/v1/ledger/transactions \
  -H "Authorization: Bearer $LEDGER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "idempotencyKey": "salary-2026-09",
    "date": "2026-09-02T01:26:24Z",
    "note": "Salary",
    "postings": [
      {"account": {"type": "ACCOUNT_TYPE_ASSETS", "user": "alice", "name": "Checking"},
       "amount": {"currencyCode": "USD", "units": 2000}},
      {"account": {"type": "ACCOUNT_TYPE_INCOMES", "user": "alice", "name": "Salary"},
       "amount": {"currencyCode": "USD", "units": -2000}}
    ]
  }'
```

The response is the recorded Transaction, carrying the running balance on each
Posting and the `replayed` flag (shown here pretty-printed):

```json
{
    "transaction": {
        "id": "01a05fb9-72c3-7eca-8a5f-a7870a9d03b3",
        "idempotencyKey": "salary-2026-09",
        "date": "2026-09-02T01:26:24Z",
        "note": "Salary",
        "metadata": {},
        "postings": [
            {
                "id": "01a05fb9-72c5-7c24-9c6b-2656679c11df",
                "transactionId": "01a05fb9-72c3-7eca-8a5f-a7870a9d03b3",
                "account": {
                    "type": "ACCOUNT_TYPE_ASSETS",
                    "user": "alice",
                    "name": "Checking"
                },
                "amount": {
                    "currencyCode": "USD",
                    "units": "2000",
                    "nanos": 0
                },
                "balance": {
                    "currencyCode": "USD",
                    "units": "2849",
                    "nanos": 500000000
                },
                "createdAt": "2026-09-02T01:26:24.196084Z",
                "date": "2026-09-02T01:26:24Z"
            },
            {
                "id": "01a05fb9-72c6-794c-8fd1-5a946458e8a6",
                "transactionId": "01a05fb9-72c3-7eca-8a5f-a7870a9d03b3",
                "account": {
                    "type": "ACCOUNT_TYPE_INCOMES",
                    "user": "alice",
                    "name": "Salary"
                },
                "amount": {
                    "currencyCode": "USD",
                    "units": "-2000",
                    "nanos": 0
                },
                "balance": {
                    "currencyCode": "USD",
                    "units": "-2000",
                    "nanos": 0
                },
                "createdAt": "2026-09-02T01:26:24.196084Z",
                "date": "2026-09-02T01:26:24Z"
            }
        ],
        "createdAt": "2026-09-02T01:26:24.196084Z"
    },
    "replayed": false
}
```

Two optional fields on the request are worth knowing:

- `metadata` — string pairs stored with the Transaction, which the two listing
  RPCs can filter on.
- `verifyNonNegativeBalances` — complete Accounts, each one touched by this
  Transaction, whose balance must not end up below zero. The whole Transaction
  is refused if one would.

Leaving `date` out is the usual case: the ledger stamps it.
A date behind an affected Account is refused:

```
{"code":9, "message":"transaction is backdated: ASSETS:alice:Checking already has a posting dated 2026-09-02T01:26:24Z", "details":[]}
```

A date beyond the five-minute tolerance is refused too:

```
{"code":3, "message":"date is more than five minutes in the future", "details":[]}
```

### Read the balances

An empty `account` filter returns the Trial balance.
Each field set on the filter narrows the read on an exact value, never a prefix:

```bash
curl -X POST http://localhost:8080/v1/ledger/accounts/balance \
  -H "Authorization: Bearer $LEDGER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"account": {"type": "ACCOUNT_TYPE_ASSETS", "user": "alice"}}'
```

```json
{
    "balances": [
        {
            "account": {
                "type": "ACCOUNT_TYPE_ASSETS",
                "user": "alice",
                "name": "Checking"
            },
            "balance": {
                "currencyCode": "USD",
                "units": "2849",
                "nanos": 500000000
            },
            "updatedAt": "2026-09-02T01:26:24.196084Z"
        }
    ]
}
```

A balance read costs one lookup: the balance snapshot is kept consistent with
the Postings on every write.

## Web UI

`webui/` holds a Nuxt 3 browser UI that calls the service directly
(`npm install && npm run dev`, then http://localhost:3000, with a Nitro proxy to
the backend on port 8080).

It was not part of this rework, and its screens are not covered by the
walkthrough above.
It also holds a service token in the browser, and a service token in a browser
is a service token anyone can read, so it is acceptable only while the whole
deployment is private.

## Further reading

- [`CONTEXT.md`](CONTEXT.md) — the domain model and the exact vocabulary used in
  the code, the API and this README.
- [`docs/adr/0001-forward-only-transaction-dates.md`](docs/adr/0001-forward-only-transaction-dates.md)
  — why dates only move forward.
- [`docs/adr/0002-flat-account-names.md`](docs/adr/0002-flat-account-names.md)
  — why account names are flat and opaque.
- [`docs/adr/0003-trusted-service-callers-only.md`](docs/adr/0003-trusted-service-callers-only.md)
  — why there is no tenancy and no end-user authorization.
