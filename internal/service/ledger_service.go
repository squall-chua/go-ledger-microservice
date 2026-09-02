package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/squall-chua/go-ledger-microservice/internal/accountfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/moneyfmt"
	"github.com/squall-chua/go-ledger-microservice/internal/repository"
)

type ledgerService struct {
	pb.UnimplementedLedgerServiceServer
	repo *repository.Repository
}

func NewLedgerService(repo *repository.Repository) pb.LedgerServiceServer {
	return &ledgerService{repo: repo}
}

func (s *ledgerService) RecordTransaction(ctx context.Context, req *pb.RecordTransactionRequest) (*pb.RecordTransactionResponse, error) {
	draft, err := draftFromRequest(req)
	if err != nil {
		return nil, err
	}

	transaction, err := s.repo.RecordTransaction(ctx, draft)
	if err != nil {
		if errors.Is(err, repository.ErrBalanceWouldGoNegative) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		if errors.Is(err, repository.ErrIdempotencyKeyExists) {
			return nil, status.Error(codes.AlreadyExists, "idempotency key already recorded")
		}
		// A deadlock or a serialization failure wrote nothing and is worth
		// retrying, so the caller is told that rather than "internal".
		if repository.IsRetryable(err) {
			return nil, status.Errorf(codes.Aborted, "the transaction was not recorded, try again: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to record transaction: %v", err)
	}

	return &pb.RecordTransactionResponse{Transaction: transaction}, nil
}

// draftFromRequest validates a record request and turns it into a draft the
// repository can write. Every refusal here is InvalidArgument.
func draftFromRequest(req *pb.RecordTransactionRequest) (repository.TransactionDraft, error) {
	var draft repository.TransactionDraft

	if req.IdempotencyKey == "" {
		return draft, status.Error(codes.InvalidArgument, "idempotency_key is required")
	}
	if req.Note == "" {
		return draft, status.Error(codes.InvalidArgument, "note is required")
	}
	if len(req.Postings) < 2 {
		return draft, status.Error(codes.InvalidArgument, "a transaction needs at least two postings")
	}

	date := time.Now().UTC()
	if req.Date != nil {
		date = req.Date.AsTime()
	}

	sum := decimal.Zero
	currencyCode := ""
	postings := make([]repository.PostingDraft, 0, len(req.Postings))

	for i, posting := range req.Postings {
		if posting.Account == nil {
			return draft, status.Errorf(codes.InvalidArgument, "posting %d has no account", i)
		}
		if posting.Account.Type == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
			return draft, status.Errorf(codes.InvalidArgument, "posting %d has an unspecified account type", i)
		}

		amount, err := moneyfmt.ToDecimal(posting.Amount)
		if err != nil {
			return draft, status.Errorf(codes.InvalidArgument, "posting %d has a malformed amount: %v", i, err)
		}
		if amount.IsZero() {
			return draft, status.Errorf(codes.InvalidArgument, "posting %d has a zero amount", i)
		}
		// The currency code is case-insensitive, so it is normalised here at the
		// write boundary and only ever stored upper case.
		currency := strings.ToUpper(posting.Amount.CurrencyCode)
		if currencyCode == "" {
			currencyCode = currency
		} else if currencyCode != currency {
			return draft, status.Error(codes.InvalidArgument, "a transaction carries a single currency")
		}

		sum = sum.Add(amount)
		postings = append(postings, repository.PostingDraft{
			Account: repository.Account{
				Type: accountfmt.AccountTypeToString(posting.Account.Type),
				User: posting.Account.User,
				Name: posting.Account.Name,
			},
			CurrencyCode: currency,
			Amount:       amount,
		})
	}

	if !sum.IsZero() {
		return draft, status.Errorf(codes.InvalidArgument, "postings do not sum to zero (sum is %s)", sum.String())
	}

	// Accounts to verify are matched exactly, so each one has to be a complete
	// account: nothing here is a pattern.
	verify := make([]repository.Account, 0, len(req.VerifyNonNegativeBalances))
	for i, toVerify := range req.VerifyNonNegativeBalances {
		if toVerify == nil || toVerify.Type == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
			return draft, status.Errorf(codes.InvalidArgument,
				"verify_non_negative_balances %d is not a complete account", i)
		}
		verify = append(verify, repository.Account{
			Type: accountfmt.AccountTypeToString(toVerify.Type),
			User: toVerify.User,
			Name: toVerify.Name,
		})
	}

	return repository.TransactionDraft{
		IdempotencyKey:    req.IdempotencyKey,
		Date:              date,
		Note:              req.Note,
		Metadata:          req.Metadata,
		Postings:          postings,
		VerifyNonNegative: verify,
	}, nil
}

func (s *ledgerService) ListAccountBalances(ctx context.Context, req *pb.ListAccountBalancesRequest) (*pb.ListAccountBalancesResponse, error) {
	filter := repository.BalanceFilter{CurrencyCode: req.CurrencyCode}
	if req.Account != nil {
		filter.Type = accountfmt.AccountTypeToString(req.Account.Type)
		filter.User = req.Account.User
		filter.Name = req.Account.Name
	}

	balances, err := s.repo.ListAccountBalances(ctx, filter)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read account balances: %v", err)
	}

	return &pb.ListAccountBalancesResponse{Balances: balances}, nil
}

func (s *ledgerService) ListTransactions(context.Context, *pb.ListTransactionsRequest) (*pb.ListTransactionsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListTransactions is not implemented on the new schema yet")
}

func (s *ledgerService) ListPostings(context.Context, *pb.ListPostingsRequest) (*pb.ListPostingsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ListPostings is not implemented on the new schema yet")
}
