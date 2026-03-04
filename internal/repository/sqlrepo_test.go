package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupSQLRepo(t *testing.T) (LedgerRepository, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", uuid.New().String())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	repo := NewSQLLedgerRepository(db)
	return repo, db
}

func TestSQLRecordTransaction_Success(t *testing.T) {
	repo, _ := setupSQLRepo(t)

	ctx := context.Background()
	req := &pb.Transaction{
		IdempotencyKey: "txn-1",
		Note:           "Initial funding",
		Date:           timestamppb.New(time.Now()),
		Postings: []*pb.Posting{
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
			},
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Funding"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: -100},
			},
		},
	}

	err := repo.RecordTransaction(ctx, req, nil)
	assert.NoError(t, err)

	// Idempotency check
	err = repo.RecordTransaction(ctx, req, nil)
	assert.ErrorIs(t, err, ErrIdempotentHit)
}

func TestSQLRecordTransaction_EdgeCases(t *testing.T) {
	repo, _ := setupSQLRepo(t)
	ctx := context.Background()

	t.Run("Currency Mismatch", func(t *testing.T) {
		req := &pb.Transaction{
			IdempotencyKey: "err-curr",
			Postings: []*pb.Posting{
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "A"}, Amount: &money.Money{CurrencyCode: "USD", Units: 100}},
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "B"}, Amount: &money.Money{CurrencyCode: "EUR", Units: -100}},
			},
		}
		err := repo.RecordTransaction(ctx, req, nil)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})

	t.Run("Negative Balance Wildcard Regex", func(t *testing.T) {
		req := &pb.Transaction{
			IdempotencyKey: "neg-regex",
			Postings: []*pb.Posting{
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Overdraw", User: "alice"}, Amount: &money.Money{CurrencyCode: "USD", Units: -10}},
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Rev"}, Amount: &money.Money{CurrencyCode: "USD", Units: 10}},
			},
		}
		err := repo.RecordTransaction(ctx, req, []string{"ASSETS:alice:*"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "negative balance")
	})

	t.Run("Metadata Storage and Retrieval", func(t *testing.T) {
		anyVal, _ := anypb.New(&pb.AccountName{Name: "MetaValue"})
		req := &pb.Transaction{
			IdempotencyKey: "meta-sql-1",
			Metadata: map[string]*anypb.Any{
				"tag": anyVal,
			},
			Postings: []*pb.Posting{
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"}, Amount: &money.Money{CurrencyCode: "USD", Units: 100}},
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_LIABILITIES, Name: "Debt"}, Amount: &money.Money{CurrencyCode: "USD", Units: -100}},
			},
		}
		err := repo.RecordTransaction(ctx, req, nil)
		assert.NoError(t, err)

		txns, _, err := repo.ListTransactions(ctx, &pb.TransactionFilter{IdempotencyKey: []string{"meta-sql-1"}}, 1, 1, false)
		assert.NoError(t, err)
		assert.Len(t, txns, 1)
		assert.NotNil(t, txns[0].Metadata["tag"])

		var decoded pb.AccountName
		err = txns[0].Metadata["tag"].UnmarshalTo(&decoded)
		assert.NoError(t, err)
		assert.Equal(t, "MetaValue", decoded.Name)
	})
}

func TestSQLGetAccountBalance(t *testing.T) {
	repo, _ := setupSQLRepo(t)

	ctx := context.Background()
	req := &pb.Transaction{
		IdempotencyKey: "txn-3",
		Postings: []*pb.Posting{
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash", User: "bob"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
			},
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Reserve", User: "bob"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 50},
			},
		},
	}

	err := repo.RecordTransaction(ctx, req, nil)
	require.NoError(t, err)

	// Exact match
	balances, err := repo.GetAccountBalance(ctx, "ASSETS:bob:Cash", "")
	assert.NoError(t, err)
	assert.Len(t, balances, 1)
	assert.Equal(t, int64(100), balances[0].Balance.Units)

	// Wildcard match
	balances, err = repo.GetAccountBalance(ctx, "ASSETS:bob:*", "USD")
	assert.NoError(t, err)
	assert.Len(t, balances, 2)

	t.Run("No Results", func(t *testing.T) {
		balances, err := repo.GetAccountBalance(ctx, "ASSETS:nonexistent", "")
		assert.NoError(t, err)
		assert.Len(t, balances, 0)
	})
}

func TestSQLListTransactions(t *testing.T) {
	repo, _ := setupSQLRepo(t)

	ctx := context.Background()
	req := &pb.Transaction{
		IdempotencyKey: "txn-4",
		Note:           "Test match note",
		Date:           timestamppb.New(time.Now()),
		Postings: []*pb.Posting{
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
			},
		},
	}

	err := repo.RecordTransaction(ctx, req, nil)
	require.NoError(t, err)

	t.Run("Filter by note", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{Note: "Test match note"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)

		txns, count, err = repo.ListTransactions(ctx, &pb.TransactionFilter{Note: "*match*"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
	})

	t.Run("Filter by Date and IdempotencyKey", func(t *testing.T) {
		now := time.Now()
		filter := &pb.TransactionFilter{
			StartDate:      timestamppb.New(now.Add(-1 * time.Hour)),
			EndDate:        timestamppb.New(now.Add(1 * time.Hour)),
			IdempotencyKey: []string{"txn-4"},
		}
		txns, count, err := repo.ListTransactions(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
	})

	t.Run("Filter by Currency", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{Currency: "USD"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
	})

	t.Run("Metadata Filter Operators", func(t *testing.T) {
		anyB, _ := anypb.New(&pb.AccountName{Name: "MetaB"})
		reqB := &pb.Transaction{
			IdempotencyKey: "meta-sql-op",
			Metadata:       map[string]*anypb.Any{"foo": anyB},
			Postings:       []*pb.Posting{{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "A"}, Amount: &money.Money{CurrencyCode: "USD", Units: 100}}, {Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "B"}, Amount: &money.Money{CurrencyCode: "USD", Units: -100}}},
		}
		require.NoError(t, repo.RecordTransaction(ctx, reqB, nil))

		bVal, _ := proto.Marshal(anyB)
		operators := []pb.MetadataFilter_Operator{
			pb.MetadataFilter_OPERATOR_EQUAL,
			pb.MetadataFilter_OPERATOR_NOT_EQUAL,
			pb.MetadataFilter_OPERATOR_PARTIAL_MATCH,
			pb.MetadataFilter_OPERATOR_GREATER_THAN,
			pb.MetadataFilter_OPERATOR_LESS_THAN,
			pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL,
			pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL,
		}

		for _, op := range operators {
			filter := &pb.TransactionFilter{
				MetadataFilters: []*pb.MetadataFilter{
					{Key: "foo", Operator: op, Value: &anypb.Any{Value: bVal}},
				},
			}
			_, _, err := repo.ListTransactions(ctx, filter, 10, 1, false)
			assert.NoError(t, err, "Operator %v should not error", op)
		}
	})

	t.Run("Pagination and Sorting", func(t *testing.T) {
		txns, _, err := repo.ListTransactions(ctx, nil, 10, 1, true)
		assert.NoError(t, err)
		// Should be at least 2 txns now (txn-4 and meta-sql-op)
		assert.True(t, len(txns) >= 2)
		assert.True(t, txns[0].Date.AsTime().After(txns[1].Date.AsTime()) || txns[0].Date.AsTime().Equal(txns[1].Date.AsTime()))
	})
}

func TestSQLListPostings(t *testing.T) {
	repo, _ := setupSQLRepo(t)

	ctx := context.Background()
	req := &pb.Transaction{
		IdempotencyKey: "txn-5",
		Postings: []*pb.Posting{
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "CashListPostingsSQL"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
			},
		},
	}

	err := repo.RecordTransaction(ctx, req, nil)
	require.NoError(t, err)

	t.Run("Filter by Account", func(t *testing.T) {
		filter := &pb.PostingFilter{
			Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "CashListPostingsSQL"},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, postings, 1)

		// Wildcard
		filterWild := &pb.PostingFilter{
			Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash*"},
		}
		_, count, err = repo.ListPostings(ctx, filterWild, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))
	})

	t.Run("Filter by TransactionFilter", func(t *testing.T) {
		req := &pb.Transaction{
			IdempotencyKey: "txn-sql-filter",
			Note:           "SpecialNote",
			Date:           timestamppb.New(time.Now()),
			Postings: []*pb.Posting{
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "A"}, Amount: &money.Money{CurrencyCode: "USD", Units: 100}},
				{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "B"}, Amount: &money.Money{CurrencyCode: "USD", Units: -100}},
			},
		}
		require.NoError(t, repo.RecordTransaction(ctx, req, nil))

		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{
				Note: "SpecialNote",
			},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(2))
		assert.Len(t, postings, 2)
	})

	t.Run("Filter by Ids", func(t *testing.T) {
		res, _, _ := repo.ListPostings(ctx, nil, 1, 1, false)
		require.NotEmpty(t, res)
		id := res[0].Id
		idFilter := &pb.PostingFilter{Id: []string{id}}
		postings, count, err := repo.ListPostings(ctx, idFilter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, postings, 1)

		txnIdFilter := &pb.PostingFilter{TransactionId: []string{res[0].TransactionId}}
		_, count, err = repo.ListPostings(ctx, txnIdFilter, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))
	})

	t.Run("Filter by TransactionFilter Complex", func(t *testing.T) {
		now := time.Now()
		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{
				StartDate: timestamppb.New(now.Add(-1 * time.Hour)),
				EndDate:   timestamppb.New(now.Add(1 * time.Hour)),
				Note:      "Special*",
			},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(2))
		assert.Len(t, postings, 2)
	})

	t.Run("Filter by TransactionFilter EndDate and Id", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{
				EndDate: timestamppb.New(time.Now().Add(1 * time.Hour)),
				Note:    "SpecialNote",
			},
		}
		_, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))
	})

	t.Run("Metadata Filter Operators in Postings", func(t *testing.T) {
		anyVal, _ := anypb.New(&pb.AccountName{Name: "MetaOp"})
		bVal, _ := proto.Marshal(anyVal)

		operators := []pb.MetadataFilter_Operator{
			pb.MetadataFilter_OPERATOR_EQUAL,
			pb.MetadataFilter_OPERATOR_NOT_EQUAL,
			pb.MetadataFilter_OPERATOR_PARTIAL_MATCH,
			pb.MetadataFilter_OPERATOR_GREATER_THAN,
			pb.MetadataFilter_OPERATOR_LESS_THAN,
			pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL,
			pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL,
		}

		for _, op := range operators {
			filter := &pb.PostingFilter{
				TransactionFilter: &pb.TransactionFilter{
					MetadataFilters: []*pb.MetadataFilter{
						{Key: "tag", Operator: op, Value: &anypb.Any{Value: bVal}},
					},
				},
			}
			_, _, err := repo.ListPostings(ctx, filter, 10, 1, false)
			assert.NoError(t, err)
		}
	})

	t.Run("Filter by Currency in Postings", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{Currency: "USD"},
		}
		_, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))
	})

	t.Run("Pagination and Sorting", func(t *testing.T) {
		// Test desc sort
		postings, _, err := repo.ListPostings(ctx, nil, 1, 1, true)
		assert.NoError(t, err)
		assert.Len(t, postings, 1)

		// Offset
		postings2, _, err := repo.ListPostings(ctx, nil, 1, 2, true)
		assert.NoError(t, err)
		assert.Len(t, postings2, 1)
		assert.NotEqual(t, postings[0].Id, postings2[0].Id)
	})
}
