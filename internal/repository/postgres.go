// Package repository is the ledger's Postgres persistence layer. It runs on
// database/sql with pgx; Postgres is the only datastore.
package repository

import (
	"cmp"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
)

//go:embed schema.sql
var schemaSQL string

// ErrIdempotencyKeyExists reports that the idempotency key of the transaction
// being recorded is already held by another transaction.
var ErrIdempotencyKeyExists = errors.New("idempotency key already recorded")

// ErrBalanceWouldGoNegative reports that the transaction would have left an
// account the caller asked to verify with a negative balance.
var ErrBalanceWouldGoNegative = errors.New("account would go negative")

// ApplySchema creates the ledger schema if it is not already present. It is
// safe to run on every startup.
func ApplySchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	return err
}

// Account is the exact composite that identifies an account. Its parts are
// matched exactly; none of them is ever a pattern.
type Account struct {
	Type string
	User string
	Name string
}

// balanceKey identifies one balance snapshot row: an account and one currency.
type balanceKey struct {
	Account
	CurrencyCode string
}

// PostingDraft is one leg of a transaction about to be recorded.
type PostingDraft struct {
	Account      Account
	CurrencyCode string
	Amount       decimal.Decimal
}

// TransactionDraft is a validated transaction about to be recorded.
type TransactionDraft struct {
	IdempotencyKey string
	Date           time.Time
	Note           string
	Metadata       map[string]string
	Postings       []PostingDraft
	// VerifyNonNegative are the accounts, matched exactly, that the whole
	// transaction must not leave with a negative balance.
	VerifyNonNegative []Account
}

// BalanceFilter narrows a balance read. Every field is optional; no fields at
// all reads the trial balance. A nil User or Name is not filtered on, while a
// pointer to the empty string matches only the empty string.
type BalanceFilter struct {
	Type         string
	User         *string
	Name         *string
	CurrencyCode string
}

// TransactionFilter narrows a transaction listing. Every field is optional and
// the date range is half-open: StartDate is included, EndDate excluded.
type TransactionFilter struct {
	IdempotencyKey string
	StartDate      *time.Time
	EndDate        *time.Time
}

// RegisterFilter narrows a register read. It matches account fields exactly,
// with the same nil-versus-empty rule as BalanceFilter.
type RegisterFilter struct {
	Type         string
	User         *string
	Name         *string
	CurrencyCode string
	StartDate    *time.Time
	EndDate      *time.Time
}

// Page is one bounded window of a listing, newest first unless asked otherwise.
type Page struct {
	Size      int32
	Number    int32
	Ascending bool
}

// bounds are the page size and the row offset it starts at, with the size
// defaulted and clamped so no caller can ask for the whole table.
func (p Page) bounds() (limit, offset int32) {
	limit = p.Size
	switch {
	case limit <= 0:
		limit = 10
	case limit > 100:
		limit = 100
	}
	number := p.Number
	if number < 1 {
		number = 1
	}
	return limit, (number - 1) * limit
}

func (p Page) direction() string {
	if p.Ascending {
		return "ASC"
	}
	return "DESC"
}

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// RecordTransaction writes the transaction, its postings and the resulting
// balance snapshots in one database transaction.
func (r *Repository) RecordTransaction(ctx context.Context, draft TransactionDraft) (*pb.Transaction, error) {
	transactionID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}

	metadata := draft.Metadata
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// Take every balance row this transaction touches up front, in one order
	// all writers agree on, so two transactions moving money between the same
	// accounts in opposite directions cannot deadlock on each other's rows. The
	// upsert seeds the row if the account is new and locks it either way; it
	// leaves the balance alone, the postings loop below applies the amounts.
	for _, key := range lockOrder(draft.Postings) {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO account_balances
				(account_type, account_user, account_name, currency_code, balance, last_date)
			VALUES ($1, $2, $3, $4, 0, $5)
			ON CONFLICT (account_type, account_user, account_name, currency_code)
			DO UPDATE SET balance = account_balances.balance`,
			key.Type, key.User, key.Name, key.CurrencyCode, draft.Date,
		)
		if err != nil {
			return nil, err
		}
	}

	// The unique constraint on idempotency_key is what serializes concurrent
	// duplicates.
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `
		INSERT INTO transactions (id, idempotency_key, date, note, metadata, request_fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at`,
		transactionID, draft.IdempotencyKey, draft.Date, draft.Note, metadataJSON, "",
	).Scan(&createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrIdempotencyKeyExists
		}
		return nil, err
	}

	postings := make([]*pb.Posting, 0, len(draft.Postings))
	// The balance each touched account is left with once every posting has been
	// applied, which is what the non-negative verification below judges.
	finalBalances := make(map[Account]decimal.Decimal, len(draft.Postings))
	for _, draftPosting := range draft.Postings {
		// The upsert both applies the amount and returns the running balance of
		// the account after this leg, under the row lock it takes itself.
		var balance decimal.Decimal
		err = tx.QueryRowContext(ctx, `
			INSERT INTO account_balances
				(account_type, account_user, account_name, currency_code, balance, last_date)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (account_type, account_user, account_name, currency_code)
			DO UPDATE SET
				balance    = account_balances.balance + EXCLUDED.balance,
				last_date  = EXCLUDED.last_date,
				updated_at = now()
			RETURNING balance`,
			draftPosting.Account.Type, draftPosting.Account.User, draftPosting.Account.Name,
			draftPosting.CurrencyCode, draftPosting.Amount, draft.Date,
		).Scan(&balance)
		if err != nil {
			return nil, err
		}
		finalBalances[draftPosting.Account] = balance

		postingID, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}

		var postingCreatedAt time.Time
		err = tx.QueryRowContext(ctx, `
			INSERT INTO postings
				(id, transaction_id, account_type, account_user, account_name,
				 currency_code, amount, balance, date)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING created_at`,
			postingID, transactionID, draftPosting.Account.Type, draftPosting.Account.User,
			draftPosting.Account.Name, draftPosting.CurrencyCode, draftPosting.Amount,
			balance, draft.Date,
		).Scan(&postingCreatedAt)
		if err != nil {
			return nil, err
		}

		postings = append(postings, &pb.Posting{
			Id:            postingID.String(),
			TransactionId: transactionID.String(),
			Account: &pb.Account{
				Type: accountfmt.StringToAccountType(draftPosting.Account.Type),
				User: draftPosting.Account.User,
				Name: draftPosting.Account.Name,
			},
			Amount:    moneyfmt.FromDecimal(draftPosting.Amount, draftPosting.CurrencyCode),
			Balance:   moneyfmt.FromDecimal(balance, draftPosting.CurrencyCode),
			CreatedAt: timestamppb.New(postingCreatedAt),
		})
	}

	// Verification happens once, after every posting has been applied, so a
	// transaction that dips below zero and recovers within itself is accepted.
	// An account named here but not touched cannot have moved, so it is skipped.
	for _, account := range draft.VerifyNonNegative {
		if balance, touched := finalBalances[account]; touched && balance.IsNegative() {
			return nil, fmt.Errorf("%w: %s:%s:%s would be %s",
				ErrBalanceWouldGoNegative, account.Type, account.User, account.Name, balance)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &pb.Transaction{
		Id:             transactionID.String(),
		IdempotencyKey: draft.IdempotencyKey,
		Date:           timestamppb.New(draft.Date),
		Note:           draft.Note,
		Metadata:       metadata,
		Postings:       postings,
		CreatedAt:      timestamppb.New(createdAt),
	}, nil
}

// ListAccountBalances reads the balance snapshot, filtered exactly. With an
// empty filter it returns the trial balance.
func (r *Repository) ListAccountBalances(ctx context.Context, filter BalanceFilter) ([]*pb.AccountBalance, error) {
	var where filterBuilder
	where.account(filter.Type, filter.User, filter.Name, filter.CurrencyCode)

	query := `
		SELECT account_type, account_user, account_name, currency_code, balance, updated_at
		FROM account_balances` + where.clause() +
		" ORDER BY account_type, account_user, account_name, currency_code"

	rows, err := r.db.QueryContext(ctx, query, where.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	balances := []*pb.AccountBalance{}
	for rows.Next() {
		var accountType, accountUser, accountName, currencyCode string
		var balance decimal.Decimal
		var updatedAt time.Time
		if err := rows.Scan(&accountType, &accountUser, &accountName, &currencyCode, &balance, &updatedAt); err != nil {
			return nil, err
		}
		balances = append(balances, &pb.AccountBalance{
			Account: &pb.Account{
				Type: accountfmt.StringToAccountType(accountType),
				User: accountUser,
				Name: accountName,
			},
			Balance:   moneyfmt.FromDecimal(balance, currencyCode),
			UpdatedAt: timestamppb.New(updatedAt),
		})
	}
	return balances, rows.Err()
}

// lockOrder is the distinct balance rows a transaction touches, in the single
// total order every writer locks them in.
func lockOrder(postings []PostingDraft) []balanceKey {
	keys := make([]balanceKey, 0, len(postings))
	for _, posting := range postings {
		keys = append(keys, balanceKey{Account: posting.Account, CurrencyCode: posting.CurrencyCode})
	}
	slices.SortFunc(keys, func(a, b balanceKey) int {
		return cmp.Or(
			strings.Compare(a.Type, b.Type),
			strings.Compare(a.User, b.User),
			strings.Compare(a.Name, b.Name),
			strings.Compare(a.CurrencyCode, b.CurrencyCode),
		)
	})
	return slices.Compact(keys)
}

// ListTransactions returns one page of transactions, each with its postings,
// alongside the total number of transactions the filter matches.
func (r *Repository) ListTransactions(ctx context.Context, filter TransactionFilter, page Page) ([]*pb.Transaction, int64, error) {
	var where filterBuilder
	where.equalIfSet("idempotency_key", filter.IdempotencyKey)
	where.dateRange("date", filter.StartDate, filter.EndDate)

	var total int64
	if err := r.db.QueryRowContext(ctx,
		"SELECT count(*) FROM transactions"+where.clause(), where.args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	// The page is taken over transactions, then joined to their postings, so
	// a transaction's leg count never eats into the page size.
	limit, offset := page.bounds()
	direction := page.direction()
	query := `
		SELECT t.id, t.idempotency_key, t.date, t.note, t.metadata, t.created_at,
		       p.id, p.account_type, p.account_user, p.account_name, p.currency_code,
		       p.amount, p.balance, p.created_at
		FROM (
			SELECT id, idempotency_key, date, note, metadata, created_at
			FROM transactions` + where.clause() + `
			ORDER BY date ` + direction + ", id " + direction +
		" LIMIT " + where.bind(limit) + " OFFSET " + where.bind(offset) + `
		) t
		JOIN postings p ON p.transaction_id = t.id
		ORDER BY t.date ` + direction + ", t.id " + direction + ", p.id"

	rows, err := r.db.QueryContext(ctx, query, where.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	transactions := []*pb.Transaction{}
	seen := map[string]*pb.Transaction{}
	for rows.Next() {
		var id, idempotencyKey, note string
		var date, createdAt time.Time
		var metadataJSON []byte
		var posting postingRow

		fields := append([]any{&id, &idempotencyKey, &date, &note, &metadataJSON, &createdAt}, posting.fields()...)
		if err := rows.Scan(fields...); err != nil {
			return nil, 0, err
		}

		transaction := seen[id]
		if transaction == nil {
			metadata := map[string]string{}
			if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
				return nil, 0, err
			}
			transaction = &pb.Transaction{
				Id:             id,
				IdempotencyKey: idempotencyKey,
				Date:           timestamppb.New(date),
				Note:           note,
				Metadata:       metadata,
				CreatedAt:      timestamppb.New(createdAt),
			}
			seen[id] = transaction
			transactions = append(transactions, transaction)
		}
		transaction.Postings = append(transaction.Postings, posting.proto(id))
	}
	return transactions, total, rows.Err()
}

// ListRegister returns one page of an account's register — its postings in
// transaction date order, each carrying the running balance after it — along
// with the total number of postings the filter matches.
func (r *Repository) ListRegister(ctx context.Context, filter RegisterFilter, page Page) ([]*pb.Posting, int64, error) {
	var where filterBuilder
	where.account(filter.Type, filter.User, filter.Name, filter.CurrencyCode)
	where.dateRange("date", filter.StartDate, filter.EndDate)

	var total int64
	if err := r.db.QueryRowContext(ctx,
		"SELECT count(*) FROM postings"+where.clause(), where.args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit, offset := page.bounds()
	direction := page.direction()
	query := `
		SELECT transaction_id, id, account_type, account_user, account_name,
		       currency_code, amount, balance, created_at
		FROM postings` + where.clause() + `
		ORDER BY date ` + direction + ", id " + direction +
		" LIMIT " + where.bind(limit) + " OFFSET " + where.bind(offset)

	rows, err := r.db.QueryContext(ctx, query, where.args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	postings := []*pb.Posting{}
	for rows.Next() {
		var transactionID string
		var posting postingRow
		if err := rows.Scan(append([]any{&transactionID}, posting.fields()...)...); err != nil {
			return nil, 0, err
		}
		postings = append(postings, posting.proto(transactionID))
	}
	return postings, total, rows.Err()
}

// postingRow is one postings row as it is read back.
type postingRow struct {
	id           string
	accountType  string
	accountUser  string
	accountName  string
	currencyCode string
	amount       decimal.Decimal
	balance      decimal.Decimal
	createdAt    time.Time
}

// fields are the scan targets for `id, account_type, account_user,
// account_name, currency_code, amount, balance, created_at`, in that order.
func (row *postingRow) fields() []any {
	return []any{&row.id, &row.accountType, &row.accountUser, &row.accountName,
		&row.currencyCode, &row.amount, &row.balance, &row.createdAt}
}

func (row *postingRow) proto(transactionID string) *pb.Posting {
	return &pb.Posting{
		Id:            row.id,
		TransactionId: transactionID,
		Account: &pb.Account{
			Type: accountfmt.StringToAccountType(row.accountType),
			User: row.accountUser,
			Name: row.accountName,
		},
		Amount:    moneyfmt.FromDecimal(row.amount, row.currencyCode),
		Balance:   moneyfmt.FromDecimal(row.balance, row.currencyCode),
		CreatedAt: timestamppb.New(row.createdAt),
	}
}

// filterBuilder collects the conditions of a read and the values they bind.
type filterBuilder struct {
	conditions []string
	args       []any
}

// bind records a value and returns the placeholder standing for it.
func (b *filterBuilder) bind(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *filterBuilder) compare(column, operator string, value any) {
	b.conditions = append(b.conditions, column+" "+operator+" "+b.bind(value))
}

// equalIfSet filters on a column only when the value is non-empty, for the
// fields where the empty string cannot be a stored value.
func (b *filterBuilder) equalIfSet(column, value string) {
	if value != "" {
		b.compare(column, "=", value)
	}
}

// equalIfGiven filters on a column only when a value was given at all, so that
// an empty string filters for the empty string rather than for anything.
func (b *filterBuilder) equalIfGiven(column string, value *string) {
	if value != nil {
		b.compare(column, "=", *value)
	}
}

func (b *filterBuilder) account(accountType string, user, name *string, currencyCode string) {
	b.equalIfSet("account_type", accountType)
	b.equalIfGiven("account_user", user)
	b.equalIfGiven("account_name", name)
	b.equalIfSet("currency_code", currencyCode)
}

// dateRange is half-open: the start is included, the end is excluded.
func (b *filterBuilder) dateRange(column string, start, end *time.Time) {
	if start != nil {
		b.compare(column, ">=", *start)
	}
	if end != nil {
		b.compare(column, "<", *end)
	}
}

func (b *filterBuilder) clause() string {
	if len(b.conditions) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(b.conditions, " AND ")
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsRetryable reports whether the database refused a write for a reason that
// goes away on its own: a deadlock or a serialization failure. The same call
// made again is expected to succeed.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40P01" || pgErr.Code == "40001")
}
