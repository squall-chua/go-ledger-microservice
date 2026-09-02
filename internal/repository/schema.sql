-- The whole ledger schema. This is a clean break with no data migration, so the
-- first migration is the entire schema; every statement is written so that
-- applying it again at startup is a no-op.
--
-- Money is NUMERIC(38, 9): nine decimal places, exactly the precision the Money
-- wire type can express. Ids are UUID v7 so they are time-ordered.

CREATE TABLE IF NOT EXISTS transactions (
    id                  UUID PRIMARY KEY,
    idempotency_key     TEXT NOT NULL UNIQUE,
    date                TIMESTAMPTZ NOT NULL,
    note                TEXT NOT NULL,
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Deterministic fingerprint of the financial content, used to tell an
    -- idempotency replay (same key, same content) from a payload mismatch.
    request_fingerprint TEXT NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_transactions_date ON transactions (date);

CREATE TABLE IF NOT EXISTS postings (
    id             UUID PRIMARY KEY,
    transaction_id UUID NOT NULL REFERENCES transactions (id) ON DELETE CASCADE,
    account_type   TEXT NOT NULL,
    account_user   TEXT NOT NULL,
    account_name   TEXT NOT NULL,
    currency_code  TEXT NOT NULL,
    amount         NUMERIC(38, 9) NOT NULL,
    -- Running balance of the account after this leg.
    balance        NUMERIC(38, 9) NOT NULL,
    date           TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_postings_transaction ON postings (transaction_id);
CREATE INDEX IF NOT EXISTS idx_postings_account
    ON postings (account_type, account_user, account_name, currency_code);
CREATE INDEX IF NOT EXISTS idx_postings_date ON postings (date);

-- Balance snapshot: the current balance per account and currency, kept
-- consistent with the postings inside the same database transaction, so a
-- balance read is one lookup. `last_date` is the date of the most recent
-- posting on that account and currency.
CREATE TABLE IF NOT EXISTS account_balances (
    account_type   TEXT NOT NULL,
    account_user   TEXT NOT NULL,
    account_name   TEXT NOT NULL,
    currency_code  TEXT NOT NULL,
    balance        NUMERIC(38, 9) NOT NULL,
    last_date      TIMESTAMPTZ NOT NULL,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_type, account_user, account_name, currency_code)
);
