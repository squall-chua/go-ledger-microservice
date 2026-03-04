package service

import (
	"context"
	"testing"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/middleware"
	"github.com/squall-chua/go-ledger-microservice/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRecordTransaction(t *testing.T) {
	repo := new(MockLedgerRepository)
	srv := NewLedgerService(repo)

	ctx := context.Background()

	t.Run("Missing Idempotency Key", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
			},
		}

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("Missing Postings", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-1",
		}

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("Currency Mismatch", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-1",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"},
					Amount:  &money.Money{CurrencyCode: "EUR", Units: -100},
				},
			},
		}

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("Unbalanced Transaction", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-1",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -90},
				},
			},
		}

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("Success path", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-success",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -100},
				},
			},
		}

		repo.On("RecordTransaction", ctx, mock.AnythingOfType("*v1.Transaction"), mock.Anything).Return(nil).Once()

		res, err := srv.RecordTransaction(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Equal(t, "key-success", res.Transaction.IdempotencyKey)
		repo.AssertExpectations(t)
	})

	t.Run("Idempotency violation", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-idem",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -100},
				},
			},
		}

		repo.On("RecordTransaction", ctx, mock.AnythingOfType("*v1.Transaction"), mock.Anything).Return(repository.ErrIdempotentHit).Once()

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.AlreadyExists, s.Code())
		repo.AssertExpectations(t)
	})

	t.Run("Missing Posting Amount", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-missing-amount",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  nil,
				},
			},
		}

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("Zero Posting Amount", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-zero-amount",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 0, Nanos: 0},
				},
			},
		}

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("Repository Error", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-repo-error",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -100},
				},
			},
		}

		repo.On("RecordTransaction", ctx, mock.AnythingOfType("*v1.Transaction"), mock.Anything).Return(status.Error(codes.Internal, "db error")).Once()

		res, err := srv.RecordTransaction(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, s.Code())
		repo.AssertExpectations(t)
	})

	t.Run("Verify Non-Negative Balances", func(t *testing.T) {
		req := &pb.RecordTransactionRequest{
			IdempotencyKey: "key-verify",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Cash"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: 100},
				},
				{
					Account: &pb.AccountName{Type: pb.AccountType_ACCOUNT_TYPE_INCOMES, Name: "Sales"},
					Amount:  &money.Money{CurrencyCode: "USD", Units: -100},
				},
			},
			VerifyNonNegativeBalances: []*pb.AccountName{
				{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, Name: "Bank"},
			},
		}

		repo.On("RecordTransaction", ctx, mock.AnythingOfType("*v1.Transaction"), []string{"ASSETS:*:Bank"}).Return(nil).Once()

		res, err := srv.RecordTransaction(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		repo.AssertExpectations(t)
	})
}

func TestGetAccountBalance(t *testing.T) {
	repo := new(MockLedgerRepository)
	srv := NewLedgerService(repo)

	ctxAdmin := middleware.ContextWithTokenInfo(context.Background(), &middleware.TokenInfo{
		UserID: "admin-1",
		Roles:  []string{"admin"},
	})

	ctxUser := middleware.ContextWithTokenInfo(context.Background(), &middleware.TokenInfo{
		UserID: "user-1",
		Roles:  []string{"user"},
	})

	t.Run("Missing Account", func(t *testing.T) {
		req := &pb.GetAccountBalanceRequest{}
		res, err := srv.GetAccountBalance(ctxAdmin, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, s.Code())
	})

	t.Run("User queries mismatched user account", func(t *testing.T) {
		req := &pb.GetAccountBalanceRequest{
			Account: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "user-2",
				Name: "Test",
			},
		}
		res, err := srv.GetAccountBalance(ctxUser, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.PermissionDenied, s.Code())
	})

	t.Run("Admin queries mismatched user account (success)", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		req := &pb.GetAccountBalanceRequest{
			Account: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "user-2",
				Name: "Test",
			},
			Currency: "USD",
		}

		balances := []*pb.AccountBalance{
			{
				Account: req.Account,
				Balance: &money.Money{CurrencyCode: "USD", Units: 100},
			},
		}
		repo.On("GetAccountBalance", ctxAdmin, "ASSETS:user-2:Test", "USD").Return(balances, nil).Once()

		res, err := srv.GetAccountBalance(ctxAdmin, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Len(t, res.Balances, 1)
		repo.AssertExpectations(t)
	})

	t.Run("User queries own account (success)", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		req := &pb.GetAccountBalanceRequest{
			Account: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "user-1",
				Name: "Test",
			},
			Currency: "USD",
		}

		balances := []*pb.AccountBalance{
			{
				Account: req.Account,
				Balance: &money.Money{CurrencyCode: "USD", Units: 50},
			},
		}
		repo.On("GetAccountBalance", ctxUser, "ASSETS:user-1:Test", "USD").Return(balances, nil).Once()

		res, err := srv.GetAccountBalance(ctxUser, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Len(t, res.Balances, 1)
		repo.AssertExpectations(t)
	})

	t.Run("Repository returns error", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		req := &pb.GetAccountBalanceRequest{
			Account: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				Name: "Cash",
			},
		}
		repo.On("GetAccountBalance", ctxAdmin, "ASSETS:*:Cash", "").Return(nil, status.Error(codes.Internal, "db error")).Once()

		res, err := srv.GetAccountBalance(ctxAdmin, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, s.Code())
		repo.AssertExpectations(t)
	})
}

func TestListTransactions(t *testing.T) {
	repo := new(MockLedgerRepository)
	srv := NewLedgerService(repo)

	ctx := context.Background()

	t.Run("Success path", func(t *testing.T) {
		req := &pb.ListTransactionsRequest{
			PageSize:   10,
			PageNumber: 1,
		}

		txns := []*pb.Transaction{
			{Id: "txn-1"},
		}
		repo.On("ListTransactions", ctx, (*pb.TransactionFilter)(nil), int32(10), int32(1), false).Return(txns, int64(1), nil).Once()

		res, err := srv.ListTransactions(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Equal(t, int64(1), res.TotalCount)
		assert.Len(t, res.Transactions, 1)
		repo.AssertExpectations(t)
	})

	t.Run("Repository returns error", func(t *testing.T) {
		req := &pb.ListTransactionsRequest{}
		repo.On("ListTransactions", ctx, (*pb.TransactionFilter)(nil), int32(0), int32(0), false).Return(nil, int64(0), status.Error(codes.Internal, "db error")).Once()

		res, err := srv.ListTransactions(ctx, req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, s.Code())
		repo.AssertExpectations(t)
	})
}

func TestListPostings(t *testing.T) {
	repo := new(MockLedgerRepository)
	srv := NewLedgerService(repo)

	ctxUser := middleware.ContextWithTokenInfo(context.Background(), &middleware.TokenInfo{
		UserID: "user-1",
		Roles:  []string{"user"},
	})

	t.Run("User filters own postings", func(t *testing.T) {
		req := &pb.ListPostingsRequest{
			PageSize: 10,
		}

		postings := []*pb.Posting{
			{Id: "pst-1"},
		}

		// The service modifies the request to enforce the user ID
		repo.On("ListPostings", ctxUser, mock.MatchedBy(func(f *pb.PostingFilter) bool {
			return f != nil && f.Account != nil && f.Account.User == "user-1"
		}), int32(10), int32(0), false).Return(postings, int64(1), nil).Once()

		res, err := srv.ListPostings(ctxUser, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Equal(t, int64(1), res.TotalCount)
		assert.Len(t, res.Postings, 1)
		repo.AssertExpectations(t)
	})

	t.Run("Admin filters postings", func(t *testing.T) {
		ctxAdmin := middleware.ContextWithTokenInfo(context.Background(), &middleware.TokenInfo{
			UserID: "admin-1",
			Roles:  []string{"admin"},
		})
		req := &pb.ListPostingsRequest{
			PageSize: 10,
		}

		postings := []*pb.Posting{
			{Id: "pst-1"},
		}

		repo.On("ListPostings", ctxAdmin, (*pb.PostingFilter)(nil), int32(10), int32(0), false).Return(postings, int64(1), nil).Once()

		res, err := srv.ListPostings(ctxAdmin, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		assert.Equal(t, int64(1), res.TotalCount)
		assert.Len(t, res.Postings, 1)
		repo.AssertExpectations(t)
	})

	t.Run("Repository returns error", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		req := &pb.ListPostingsRequest{}
		repo.On("ListPostings", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, int64(0), status.Error(codes.Internal, "db error")).Once()

		res, err := srv.ListPostings(context.Background(), req)
		assert.Error(t, err)
		assert.Nil(t, res)
		s, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, s.Code())
		repo.AssertExpectations(t)
	})

	t.Run("No token info in context", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		req := &pb.ListPostingsRequest{
			PageSize: 10,
		}

		postings := []*pb.Posting{
			{Id: "pst-1"},
		}

		// Default role is "user", but userID is ""
		repo.On("ListPostings", context.Background(), mock.MatchedBy(func(f *pb.PostingFilter) bool {
			return f != nil && f.Account != nil && f.Account.User == ""
		}), int32(10), int32(0), false).Return(postings, int64(1), nil).Once()

		res, err := srv.ListPostings(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		repo.AssertExpectations(t)
	})

	t.Run("Token with no roles", func(t *testing.T) {
		repo := new(MockLedgerRepository)
		srv := NewLedgerService(repo)
		ctxNoRoles := middleware.ContextWithTokenInfo(context.Background(), &middleware.TokenInfo{
			UserID: "user-1",
			Roles:  []string{},
		})
		req := &pb.ListPostingsRequest{
			PageSize: 10,
		}

		postings := []*pb.Posting{
			{Id: "pst-1"},
		}

		// Defaults to "user" role
		repo.On("ListPostings", ctxNoRoles, mock.MatchedBy(func(f *pb.PostingFilter) bool {
			return f != nil && f.Account != nil && f.Account.User == "user-1"
		}), int32(10), int32(0), false).Return(postings, int64(1), nil).Once()

		res, err := srv.ListPostings(ctxNoRoles, req)
		assert.NoError(t, err)
		assert.NotNil(t, res)
		repo.AssertExpectations(t)
	})
}
