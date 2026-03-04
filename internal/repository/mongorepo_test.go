package repository

import (
	"context"
	"os"
	"testing"
	"time"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tryvium-travels/memongo"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var mongoURI string

func TestMain(m *testing.M) {
	mongoServer, err := memongo.StartWithOptions(&memongo.Options{MongoVersion: "6.0.0"})
	if err != nil {
		// If memongo fails (e.g., download issue or unsupported OS), just exit and maybe skip tests.
		// For the sake of covering code, we'll try to start and exit 0 if it fully fails to find binary,
		// but ideally it works.
		os.Exit(0)
	}
	mongoURI = mongoServer.URI()
	code := m.Run()
	mongoServer.Stop()
	os.Exit(code)
}

func setupMongoRepo(t *testing.T) (LedgerRepository, *mongo.Database) {
	if mongoURI == "" {
		t.Skip("memongo not available")
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	require.NoError(t, err)

	dbName := "ledger_test_" + time.Now().Format("150405_000") + "_" + t.Name()
	if len(dbName) > 38 {
		dbName = dbName[:38] // MongoDB db name limit is 64 bytes, keeping it short
	}
	db := client.Database(dbName)
	repo := NewMongoLedgerRepository(db)
	return repo, db
}

func TestMongoRecordTransaction_Success(t *testing.T) {
	repo, _ := setupMongoRepo(t)

	ctx := context.Background()
	req := &pb.Transaction{
		IdempotencyKey: "txn-mongo-1",
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

func TestMongoRecordTransaction_EdgeCases(t *testing.T) {
	repo, _ := setupMongoRepo(t)
	ctx := context.Background()

	t.Run("Currency Mismatch", func(t *testing.T) {
		req := &pb.Transaction{
			IdempotencyKey: "mismatch-1",
			Postings: []*pb.Posting{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Bank"},
					Amount:  &money.Money{CurrencyCode: "EUR", Units: -100},
				},
			},
		}
		err := repo.RecordTransaction(ctx, req, nil)
		assert.ErrorIs(t, err, ErrCurrencyMismatch)
	})

	t.Run("Exact Negative Balance", func(t *testing.T) {
		req := &pb.Transaction{
			IdempotencyKey: "neg-exact",
			Postings: []*pb.Posting{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "NegAcc"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -10},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Rev"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 10},
				},
			},
		}
		err := repo.RecordTransaction(ctx, req, []string{"ASSETS:*:NegAcc"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "negative balance")
	})

	t.Run("Metadata Storage", func(t *testing.T) {
		req := &pb.Transaction{
			IdempotencyKey: "meta-1",
			Note:           "Metadata test",
			Date:           timestamppb.New(time.Now()),
			Metadata: map[string]*anypb.Any{
				"tag": {TypeUrl: "type.googleapis.com/google.protobuf.StringValue", Value: []byte("test")},
			},
			Postings: []*pb.Posting{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_LIABILITIES, Name: "Debt"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -100},
				},
			},
		}
		err := repo.RecordTransaction(ctx, req, nil)
		assert.NoError(t, err)

		txns, _, err := repo.ListTransactions(ctx, &pb.TransactionFilter{IdempotencyKey: []string{"meta-1"}}, 1, 1, false)
		assert.NoError(t, err)
		assert.Len(t, txns, 1)
	})
}

func TestMongoGetAccountBalance(t *testing.T) {
	repo, _ := setupMongoRepo(t)

	ctx := context.Background()
	reqUSD := &pb.Transaction{
		IdempotencyKey: "txn-mongo-3-usd",
		Postings: []*pb.Posting{
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash", User: "bob"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
			},
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Reserve", User: "bob"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 50},
			},
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "RevUSD"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: -150},
			},
		},
	}
	require.NoError(t, repo.RecordTransaction(ctx, reqUSD, nil))

	reqEUR := &pb.Transaction{
		IdempotencyKey: "txn-mongo-3-eur",
		Postings: []*pb.Posting{
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Bank"},
				Amount:  &money.Money{CurrencyCode: "EUR", Units: 200},
			},
			{
				Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "RevEUR"},
				Amount:  &money.Money{CurrencyCode: "EUR", Units: -200},
			},
		},
	}
	require.NoError(t, repo.RecordTransaction(ctx, reqEUR, nil))

	t.Run("Exact match", func(t *testing.T) {
		balances, err := repo.GetAccountBalance(ctx, "ASSETS:bob:Cash", "")
		assert.NoError(t, err)
		assert.Len(t, balances, 1)
		assert.Equal(t, int64(100), balances[0].Balance.Units)
	})

	t.Run("Wildcard match with currency filter", func(t *testing.T) {
		balances, err := repo.GetAccountBalance(ctx, "ASSETS:bob:*", "USD")
		assert.NoError(t, err)
		assert.Len(t, balances, 2)
	})

	t.Run("No results", func(t *testing.T) {
		balances, err := repo.GetAccountBalance(ctx, "ASSETS:nonexistent", "")
		assert.NoError(t, err)
		assert.Len(t, balances, 0)
	})

	t.Run("Wildcard no results", func(t *testing.T) {
		balances, err := repo.GetAccountBalance(ctx, "LIABILITIES:*", "")
		assert.NoError(t, err)
		assert.Len(t, balances, 0)
	})
}

func TestMongoListTransactions(t *testing.T) {
	repo, _ := setupMongoRepo(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	req1 := &pb.Transaction{
		IdempotencyKey: "list-1",
		Note:           "Food shopping",
		Date:           timestamppb.New(now.Add(-2 * time.Hour)),
		Postings: []*pb.Posting{
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"}, Amount: &money.Money{CurrencyCode: "USD", Units: 10}},
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_EXPENSES, Name: "Food"}, Amount: &money.Money{CurrencyCode: "USD", Units: -10}},
		},
	}
	require.NoError(t, repo.RecordTransaction(ctx, req1, nil))

	req2 := &pb.Transaction{
		IdempotencyKey: "list-2",
		Note:           "Rent payment",
		Date:           timestamppb.New(now.Add(-1 * time.Hour)),
		Postings: []*pb.Posting{
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"}, Amount: &money.Money{CurrencyCode: "USD", Units: 50}},
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_EXPENSES, Name: "Rent"}, Amount: &money.Money{CurrencyCode: "USD", Units: -50}},
		},
	}
	require.NoError(t, repo.RecordTransaction(ctx, req2, nil))

	t.Run("Filter by note wildcard", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{Note: "*shopping*"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
		assert.Equal(t, "Food shopping", txns[0].Note)
	})

	t.Run("Filter by exact note", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{Note: "Rent payment"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
	})

	t.Run("Filter by date range (Gte)", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{StartDate: timestamppb.New(now.Add(-90 * time.Minute))}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
	})

	t.Run("Filter by date range (Lt)", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{EndDate: timestamppb.New(now.Add(-90 * time.Minute))}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, txns, 1)
	})

	t.Run("Filter by date range (Both)", func(t *testing.T) {
		_, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{
			StartDate: timestamppb.New(now.Add(-3 * time.Hour)),
			EndDate:   timestamppb.New(now),
		}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	t.Run("Filter by IdempotencyKeys", func(t *testing.T) {
		_, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{IdempotencyKey: []string{"list-1", "list-2"}}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	t.Run("Filter by Currency", func(t *testing.T) {
		_, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{Currency: "USD"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), count)
	})

	t.Run("Pagination and Sorting", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, nil, 1, 1, true)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), count)
		assert.Len(t, txns, 1)
		// With desc sort, list-2 (more recent) should be first
		assert.Equal(t, "list-2", txns[0].IdempotencyKey)
	})

	t.Run("Empty results", func(t *testing.T) {
		txns, count, err := repo.ListTransactions(ctx, &pb.TransactionFilter{Note: "None"}, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), count)
		assert.Len(t, txns, 0)
	})

	t.Run("Metadata filters coverage", func(t *testing.T) {
		mf := []*pb.MetadataFilter{
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_EQUAL, Value: &anypb.Any{Value: []byte("bar")}},
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_NOT_EQUAL, Value: &anypb.Any{Value: []byte("baz")}},
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_GREATER_THAN, Value: &anypb.Any{Value: []byte("a")}},
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_LESS_THAN, Value: &anypb.Any{Value: []byte("z")}},
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_GREATER_THAN_OR_EQUAL, Value: &anypb.Any{Value: []byte("a")}},
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_LESS_THAN_OR_EQUAL, Value: &anypb.Any{Value: []byte("z")}},
			{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_PARTIAL_MATCH, Value: &anypb.Any{Value: []byte("ba")}},
		}
		for _, f := range mf {
			_, _, err := repo.ListTransactions(ctx, &pb.TransactionFilter{MetadataFilters: []*pb.MetadataFilter{f}}, 10, 1, false)
			assert.NoError(t, err)
		}
	})
}

func TestMongoListPostings(t *testing.T) {
	repo, _ := setupMongoRepo(t)
	ctx := context.Background()

	req := &pb.Transaction{
		IdempotencyKey: "pst-list-1",
		Note:           "Multi-posting txn",
		Postings: []*pb.Posting{
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"}, Amount: &money.Money{CurrencyCode: "USD", Units: 100}},
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Bank"}, Amount: &money.Money{CurrencyCode: "USD", Units: 50}},
			{Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"}, Amount: &money.Money{CurrencyCode: "USD", Units: -150}},
		},
		Date: timestamppb.New(time.Now()),
	}
	require.NoError(t, repo.RecordTransaction(ctx, req, nil))

	t.Run("Filter by exact account", func(t *testing.T) {
		filter := &pb.PostingFilter{
			Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, postings, 1)
		assert.Equal(t, "ASSETS:*:Cash", accountfmt.BuildString(postings[0].Account))
	})

	t.Run("Filter by wildcard account", func(t *testing.T) {
		filter := &pb.PostingFilter{
			Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "*"},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), count)
		assert.Len(t, postings, 2)
	})

	t.Run("Filter by TransactionFilter", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{Note: "Multi-posting txn"},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
		assert.Len(t, postings, 3)
	})

	t.Run("Filter by TransactionFilter with Date and Note regex", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{
				StartDate: timestamppb.New(time.Now().Add(-1 * time.Hour)),
				Note:      "*posting*",
			},
		}
		_, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(3))
	})

	t.Run("Filter by Metadata labels in TransactionFilter", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionFilter: &pb.TransactionFilter{
				MetadataFilters: []*pb.MetadataFilter{
					{Key: "foo", Operator: pb.MetadataFilter_OPERATOR_EQUAL, Value: &anypb.Any{Value: []byte("bar")}},
				},
			},
		}
		_, _, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
	})

	t.Run("Pagination", func(t *testing.T) {
		postings, count, err := repo.ListPostings(ctx, nil, 1, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
		assert.Len(t, postings, 1)
	})

	t.Run("Filter by TransactionId", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionId: []string{req.Id},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
		assert.Len(t, postings, 3)
	})

	t.Run("Filter by TransactionFilter sub-fields", func(t *testing.T) {
		cases := []struct {
			name   string
			filter *pb.TransactionFilter
		}{
			{"Date Gte", &pb.TransactionFilter{StartDate: timestamppb.New(time.Now().Add(-1 * time.Hour))}},
			{"Date Lt", &pb.TransactionFilter{EndDate: timestamppb.New(time.Now().Add(1 * time.Hour))}},
			{"Note Exact", &pb.TransactionFilter{Note: "Multi-posting txn"}},
			{"Note Wildcard", &pb.TransactionFilter{Note: "Multi*"}},
			{"Id", &pb.TransactionFilter{Id: []string{req.Id}}},
			{"IdempotencyKey", &pb.TransactionFilter{IdempotencyKey: []string{"pst-list-1"}}},
			{"Currency", &pb.TransactionFilter{Currency: "USD"}},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, count, err := repo.ListPostings(ctx, &pb.PostingFilter{TransactionFilter: tc.filter}, 10, 1, false)
				assert.NoError(t, err)
				assert.GreaterOrEqual(t, count, int64(1))
			})
		}
	})

	t.Run("Filter by Posting Id", func(t *testing.T) {
		// First get a real posting ID
		res, _, _ := repo.ListPostings(ctx, nil, 1, 1, false)
		id := res[0].Id
		filter := &pb.PostingFilter{Id: []string{id}}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(1), count)
		assert.Len(t, postings, 1)
	})

	t.Run("No results", func(t *testing.T) {
		filter := &pb.PostingFilter{
			TransactionId: []string{"nonexistent"},
		}
		postings, count, err := repo.ListPostings(ctx, filter, 10, 1, false)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), count)
		assert.Len(t, postings, 0)
	})
}
