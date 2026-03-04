package service

import (
	"context"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/stretchr/testify/mock"
)

type MockLedgerRepository struct {
	mock.Mock
}

func (m *MockLedgerRepository) RecordTransaction(ctx context.Context, txn *pb.Transaction, verifyNonNegativeBalances []string) error {
	args := m.Called(ctx, txn, verifyNonNegativeBalances)
	return args.Error(0)
}

func (m *MockLedgerRepository) GetAccountBalance(ctx context.Context, accountName string, currencyFilter string) ([]*pb.AccountBalance, error) {
	args := m.Called(ctx, accountName, currencyFilter)
	if args.Get(0) != nil {
		return args.Get(0).([]*pb.AccountBalance), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockLedgerRepository) ListTransactions(ctx context.Context, filter *pb.TransactionFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Transaction, int64, error) {
	args := m.Called(ctx, filter, pageSize, pageNumber, orderByDesc)
	if args.Get(0) != nil {
		return args.Get(0).([]*pb.Transaction), args.Get(1).(int64), args.Error(2)
	}
	return nil, args.Get(1).(int64), args.Error(2)
}

func (m *MockLedgerRepository) ListPostings(ctx context.Context, filter *pb.PostingFilter, pageSize, pageNumber int32, orderByDesc bool) ([]*pb.Posting, int64, error) {
	args := m.Called(ctx, filter, pageSize, pageNumber, orderByDesc)
	if args.Get(0) != nil {
		return args.Get(0).([]*pb.Posting), args.Get(1).(int64), args.Error(2)
	}
	return nil, args.Get(1).(int64), args.Error(2)
}
