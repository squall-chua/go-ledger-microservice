# Go Ledger Microservice

A highly reliable, double-entry accounting ledger microservice.

## Inspired by ledger-cli
This project takes significant inspiration from [ledger-cli](https://ledger-cli.org/), the powerful, command-line accounting tool. Specifically, it brings the fundamental philosophies of `ledger-cli` into a modern microservice:

- **Double-Entry Accounting Principles:** Every transaction must perfectly balance to zero (i.e. debits must equal credits).
- **Familiar CLI Experience:** The included client tool deliberately mimics standard `ledger-cli` subcommands like `balance`, `register`, and `post`.
- **Hierarchical Naming Structure:** Accounts use colon-delimited paths (e.g., `Assets:Checking`, `Expenses:Groceries`), allowing hierarchical roll-up of balances.
- **Native Multi-Currency Support:** Amounts are paired with their currency identifiers (e.g., `1000USD`, `250MYR`), ensuring accuracy across varied financial flows.
- **Immutability and Auditability:** Emulating appending to a journal file, transactions are recorded as an immutable sequence of events, accurately affecting a running balance.

While `ledger-cli` operates entirely on local text files, this microservice scales those principles to a gRPC/HTTP backend architecture with robust SQL and MongoDB persistence layers, suited for multi-user distributed systems.

## How to use the CLI

The project includes a CLI located in `./cmd/cli` that can be run natively to interact directly with the datastore. By default, it connects to a local SQLite database (`ledger.db`) but supports PostgreSQL, MySQL, and MongoDB via environment variables.

### Available Commands

*   `post`: Record a new transaction (requires note and at least 2 balanced postings).
*   `balance` / `bal`: Get account balances.
*   `register` / `reg`: List transactions and their running balances.

## Detailed Steps for Manual Tests

You can perform manual testing of the ledger's core functionality via the CLI using the following steps:

### 1. Record Initial Transactions
You can use the `post` command to record your transactions. Pass an arbitrary note/description, followed by the postings. The postings are constructed as `[account_name]:[amount][currency]`.

Create an initial balance:
```bash
go run ./cmd/cli post "Initial Deposit" "Assets:Checking:1000USD" "Equity:OpeningBalances:-1000USD"
```

Record an expense:
```bash
go run ./cmd/cli post "Purchase Groceries" "Expenses:Grocery:150.50USD" "Assets:Checking:-150.50USD"
```

*Note: For these commands to succeed, their values must sum exactly to 0 (Double-entry principle).*

### 2. View Account Balances
Use the `balance` command to view the aggregated sum of all accounts.

```bash
go run ./cmd/cli balance
```

**Expected output:**
```
ASSETS:*:Checking    849.50 USD (Updated: 2026-03-04T15:38:59Z)
EXPENSES:*:Grocery   150.50 USD (Updated: 2026-03-04T15:39:20Z)
*:*:OpeningBalances  -1000 USD (Updated: 2026-03-04T15:38:59Z)
```

You can optionally filter by a specific account prefix or currency:
```bash
go run ./cmd/cli balance Assets
go run ./cmd/cli balance -c USD
```

### 3. Check the Transaction Register
Use the `register` command to see the chronological transaction history. It tracks the running balances for each posting. 

```bash
go run ./cmd/cli register
```

**Output snippet:**
```
2026-03-04 15:38:59+08 - Initial Deposit
    ASSETS:*:Checking          1000 USD   (=       1000 USD)
    *:*:OpeningBalances       -1000 USD   (=      -1000 USD)
2026-03-04 15:39:20+08 - Purchase Groceries
    EXPENSES:*:Grocery       150.50 USD   (=     150.50 USD)
    ASSETS:*:Checking       -150.50 USD   (=     849.50 USD)
```

To list the latest transactions first, use the desc (descending) flag:
```bash
go run ./cmd/cli register -d
```

### 4. Verify Double-Entry Constraint Rejections
You should test failure modes manually to ensure validity checks reject broken entries.

**Unbalanced Transaction error:**
```bash
go run ./cmd/cli post "Unbalanced Post" "Assets:Checking:100USD" "Expenses:Food:-50USD"
# Log Output:
# 2026/03/04 15:40:00 Error: Transaction is unbalanced (sum = 50)
# exit status 1
```

**Parsing errors:**
```bash
go run ./cmd/cli post "Missing Colon" "Assets:Checking100USD"
# Log Output:
# 2026/03/04 15:40:10 Invalid posting format: Assets:Checking100USD
# exit status 1
```

## Server Setup & API Testing

In addition to the CLI, you can run the ledger directly as a gRPC/REST microservice. It uses the same persistent datastore. By default, it runs on port `8080`.

### 1. Start the Server

```bash
go run ./cmd/server/main.go --port 8080 --db-type sqlite --sql-dsn ledger.db
```

*Note: The server uses an inline multiplexer so both gRPC and HTTP/REST requests are served on the same port (`8080`).*

### 2. Manual Testing using curl

The API endpoints are secured by JWT authentication. For local testing, you can manually generate a JWT token at [https://jwt.io/](https://jwt.io/).

1. Go to **jwt.io** and ensure the Algorithm is set to `HS256`.
2. Set the Payload (Data) to include the required roles:
   ```json
   {
     "sub": "test-user",
     "roles": ["admin", "user"]
   }
   ```
3. Set the Verify Signature secret to the server's default symmetric key: `super-secret-key`.
4. Copy the generated encoded token on the left panel and export it in your terminal:

```bash
export LEDGER_TOKEN="your_generated_token_here"
```

#### Record a Transaction

Here is a `curl` example corresponding to the `post` CLI command:

```bash
curl -X POST http://localhost:8080/v1/ledger/transactions \
  -H "Authorization: Bearer $LEDGER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "note": "Online purchase via API",
    "postings": [
      {
        "account": { "type": 5, "name": "OnlineShopping" },
        "amount": { "currencyCode": "MYR", "units": 50, "nanos": 0 }
      },
      {
        "account": { "type": 1, "name": "wallet" },
        "amount": { "currencyCode": "MYR", "units": -50, "nanos": 0 }
      }
    ]
  }'
```

#### Get Account Balance

Retrieve all balances with an empty query struct. 

```bash
curl -X POST http://localhost:8080/v1/ledger/accounts/balance \
  -H "Authorization: Bearer $LEDGER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"account": {}}'
```

To limit it to a specific account root (e.g., all `ASSETS` - numerical enum `1` in proto):
```bash
curl -X POST http://localhost:8080/v1/ledger/accounts/balance \
  -H "Authorization: Bearer $LEDGER_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "account": { "type": 1 }
  }'
```

## Running the Web UI

The project now includes a beautiful, modern Nuxt.js 3 Web UI that allows you to interact with the Ledger microservice dynamically in your browser. It natively supports Dark Mode.

### 1. Setup & Installation
Ensure you have `Node.js` installed. The UI is located in the `webui` folder.

```bash
cd webui
npm install
```

### 2. Start the Development Server
Make sure your Go Ledger microservice backend is running concurrently on port `8080` (as shown above).

```bash
# Inside the webui folder
npm run dev
```

The Web UI will be accessible at **http://localhost:3000**. The Nuxt application is configured with a Nitro proxy (`/api/**` -> `http://127.0.0.1:8080/**`) to seamlessly connect to the backend API without CORS issues.

### 3. Using the Web UI

1. **Authentication:**
   When you first load the App, you will be directed to `/login`. Generate a JWT token via `jwt.io` (as explained in the *API Testing* section) with the `admin` role, and paste it into the UI login form. The UI will store your token securely in `localStorage`.
2. **Dashboard Overview (`/`)**: 
   View high-level totals grouping all Assets, Revenues, and Expenses into intuitive cards.
3. **Account Balances (`/balances`)**:
   View a clean data-table of all hierarchical accounts in your ledger and their current running balances with currency indicators.
4. **Transaction Register (`/transactions`)**:
   A chronological timeline of every transaction recorded, displaying the descriptive note alongside its multi-layered postings.
5. **Record Transaction Form (`/transactions/new`)**:
   An interactive form ensuring double-entry principles. You can add as many debit/credit postings as required, and the UI will validate that the running sum equals zero before allowing you to commit the transaction to the backend.
