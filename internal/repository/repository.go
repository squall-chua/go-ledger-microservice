package repository

import (
	"context"
	"errors"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

var (
	ErrNotFound         = errors.New("record not found")
	ErrIdempotentHit    = errors.New("idempotency key already exists")
	ErrBalanceMismatch  = errors.New("double-entry invariant failed")
	ErrCurrencyMismatch = errors.New("multiple currencies found in single transaction")
)

type LedgerRepository interface {
	RecordTransaction(ctx context.Context, txn *pb.Transaction, verifyNonNegativeBalances []string) error
	GetAccountBalance(ctx context.Context, accountName string, currency string) ([]*pb.AccountBalance, error)
	ListTransactions(ctx context.Context, filter *pb.TransactionFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Transaction, int64, error)
	ListPostings(ctx context.Context, filter *pb.PostingFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Posting, int64, error)
}
