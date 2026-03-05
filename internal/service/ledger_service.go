package service

import (
	"context"

	"github.com/shopspring/decimal"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/middleware"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type ledgerService struct {
	pb.UnimplementedLedgerServiceServer
	repo repository.LedgerRepository
}

func getUserFromContext(ctx context.Context) (userID string, role string) {
	info, ok := middleware.TokenInfoFromContext(ctx)
	if !ok {
		// Default securely if no token info passed
		return "", "user"
	}

	if len(info.Roles) > 0 {
		role = info.Roles[0]
	} else {
		role = "user"
	}

	if info.UserID != "" {
		userID = info.UserID
	}

	return userID, role
}

func NewLedgerService(repo repository.LedgerRepository) pb.LedgerServiceServer {
	return &ledgerService{repo: repo}
}

func (s *ledgerService) RecordTransaction(ctx context.Context, req *pb.RecordTransactionRequest) (*pb.RecordTransactionResponse, error) {
	if req.IdempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}

	if req.Date == nil {
		req.Date = timestamppb.Now()
	}

	if len(req.Postings) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one posting is required")
	}

	var sum decimal.Decimal
	var currency string
	var pbPostings []*pb.Posting

	for _, p := range req.Postings {
		if p.Amount == nil {
			return nil, status.Error(codes.InvalidArgument, "posting amount is required")
		}
		if currency == "" {
			currency = p.Amount.CurrencyCode
		} else if currency != p.Amount.CurrencyCode {
			return nil, status.Error(codes.InvalidArgument, "all postings must have the same currency")
		}

		dec, err := moneyfmt.ToDecimal(p.Amount)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid amount format: %v", err)
		}

		if dec.IsZero() {
			return nil, status.Error(codes.InvalidArgument, "posting amount cannot be zero")
		}

		sum = sum.Add(dec)

		pbPostings = append(pbPostings, &pb.Posting{
			Account: p.Account,
			Amount:  p.Amount,
		})
	}

	if !sum.IsZero() {
		return nil, status.Errorf(codes.InvalidArgument, "transaction postings do not balance (sum is %s, expected 0)", sum.String())
	}

	txn := &pb.Transaction{
		IdempotencyKey: req.IdempotencyKey,
		Date:           req.Date,
		Note:           req.Note,
		Metadata:       req.Metadata,
		Postings:       pbPostings,
	}

	var verifyNames []string
	for _, acc := range req.VerifyNonNegativeBalances {
		verifyNames = append(verifyNames, accountfmt.BuildString(acc))
	}

	err := s.repo.RecordTransaction(ctx, txn, verifyNames)
	if err != nil {
		if err == repository.ErrIdempotentHit {
			return nil, status.Error(codes.AlreadyExists, "idempotency key already exists")
		}
		return nil, status.Errorf(codes.Internal, "failed to record transaction: %v", err)
	}

	return &pb.RecordTransactionResponse{
		Transaction: txn,
	}, nil
}

func (s *ledgerService) GetAccountBalance(ctx context.Context, req *pb.GetAccountBalanceRequest) (*pb.GetAccountBalanceResponse, error) {
	if req.Account == nil {
		return nil, status.Error(codes.InvalidArgument, "account is required")
	}

	userID, role := getUserFromContext(ctx)
	if role == "user" && req.Account.User != userID {
		return nil, status.Error(codes.PermissionDenied, "users can only query their own account balances")
	}

	accStr := accountfmt.BuildString(req.Account)
	balances, err := s.repo.GetAccountBalance(ctx, accStr, req.Currency)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get account balance: %v", err)
	}

	return &pb.GetAccountBalanceResponse{
		Balances: balances,
	}, nil
}

func (s *ledgerService) ListTransactions(ctx context.Context, req *pb.ListTransactionsRequest) (*pb.ListTransactionsResponse, error) {
	txns, count, err := s.repo.ListTransactions(ctx, req.Filter, req.PageSize, req.PageNumber, req.OrderByDesc)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list transactions: %v", err)
	}

	return &pb.ListTransactionsResponse{
		Transactions: txns,
		TotalCount:   count,
	}, nil
}

func (s *ledgerService) ListPostings(ctx context.Context, req *pb.ListPostingsRequest) (*pb.ListPostingsResponse, error) {
	userID, role := getUserFromContext(ctx)
	if role == "user" {
		if req.Filter == nil {
			req.Filter = &pb.PostingFilter{}
		}
		if req.Filter.Account == nil {
			req.Filter.Account = &pb.AccountName{}
		}
		// Force override the user dimension so the search resolves natively to their scope
		req.Filter.Account.User = userID
	}

	postings, count, err := s.repo.ListPostings(ctx, req.Filter, req.PageSize, req.PageNumber, req.OrderByDesc)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list postings: %v", err)
	}

	return &pb.ListPostingsResponse{
		Postings:   postings,
		TotalCount: count,
	}, nil
}
