package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

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

	transaction, replayed, err := s.repo.RecordTransaction(ctx, draft)
	if err != nil {
		if errors.Is(err, repository.ErrBalanceWouldGoNegative) || errors.Is(err, repository.ErrBackdated) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		if errors.Is(err, repository.ErrIdempotencyKeyReused) {
			return nil, status.Error(codes.AlreadyExists, err.Error())
		}
		// A deadlock or a serialization failure wrote nothing and is worth
		// retrying, so the caller is told that rather than "internal".
		if repository.IsRetryable(err) {
			return nil, status.Errorf(codes.Aborted, "the transaction was not recorded, try again: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to record transaction: %v", err)
	}

	return &pb.RecordTransactionResponse{Transaction: transaction, Replayed: replayed}, nil
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

	// A supplied date is a claim about when the event happened, and is refused
	// more than five minutes ahead of now: one bad client clock would otherwise
	// park a posting in the future and refuse every later write to that account
	// as backdated. An omitted date is left for the ledger to stamp, under the
	// row locks where it can see what it has to advance past.
	date := dateOrNil(req.Date)
	if date != nil && date.After(time.Now().Add(5*time.Minute)) {
		return draft, status.Error(codes.InvalidArgument, "date is more than five minutes in the future")
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

	touched := make(map[repository.Account]bool, len(postings))
	for _, posting := range postings {
		touched[posting.Account] = true
	}

	// Accounts to verify are matched exactly, so each one has to be a complete
	// account: nothing here is a pattern. An account no posting touches is
	// refused rather than passed over — a typo in the name would otherwise turn
	// the overdraft guard off silently and record the transaction anyway.
	verify := make([]repository.Account, 0, len(req.VerifyNonNegativeBalances))
	for i, toVerify := range req.VerifyNonNegativeBalances {
		if toVerify == nil || toVerify.Type == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
			return draft, status.Errorf(codes.InvalidArgument,
				"verify_non_negative_balances %d is not a complete account", i)
		}
		toGuard := repository.Account{
			Type: accountfmt.AccountTypeToString(toVerify.Type),
			User: toVerify.User,
			Name: toVerify.Name,
		}
		if !touched[toGuard] {
			return draft, status.Errorf(codes.InvalidArgument,
				"verify_non_negative_balances %d names %s:%s:%s, which no posting of this transaction touches",
				i, toGuard.Type, toGuard.User, toGuard.Name)
		}
		verify = append(verify, toGuard)
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
	accountType, user, name := accountFilter(req.Account)
	balances, err := s.repo.ListAccountBalances(ctx, repository.BalanceFilter{
		Type:         accountType,
		User:         user,
		Name:         name,
		CurrencyCode: strings.ToUpper(req.CurrencyCode),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read account balances: %v", err)
	}

	return &pb.ListAccountBalancesResponse{Balances: balances}, nil
}

func (s *ledgerService) ListTransactions(ctx context.Context, req *pb.ListTransactionsRequest) (*pb.ListTransactionsResponse, error) {
	if err := checkMetadataFilter(req.Filter.GetMetadata()); err != nil {
		return nil, err
	}
	filter := repository.TransactionFilter{
		IdempotencyKey: req.Filter.GetIdempotencyKey(),
		StartDate:      dateOrNil(req.Filter.GetStartDate()),
		EndDate:        dateOrNil(req.Filter.GetEndDate()),
		Metadata:       req.Filter.GetMetadata(),
	}

	transactions, total, err := s.repo.ListTransactions(ctx, filter, pageOf(req.PageSize, req.PageNumber, req.OrderByAscending))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list transactions: %v", err)
	}

	return &pb.ListTransactionsResponse{Transactions: transactions, TotalCount: total}, nil
}

func (s *ledgerService) ListPostings(ctx context.Context, req *pb.ListPostingsRequest) (*pb.ListPostingsResponse, error) {
	if err := checkMetadataFilter(req.Filter.GetMetadata()); err != nil {
		return nil, err
	}
	accountType, user, name := accountFilter(req.Filter.GetAccount())
	filter := repository.RegisterFilter{
		Type:         accountType,
		User:         user,
		Name:         name,
		CurrencyCode: strings.ToUpper(req.Filter.GetCurrencyCode()),
		StartDate:    dateOrNil(req.Filter.GetStartDate()),
		EndDate:      dateOrNil(req.Filter.GetEndDate()),
		Metadata:     req.Filter.GetMetadata(),
	}

	postings, total, err := s.repo.ListRegister(ctx, filter, pageOf(req.PageSize, req.PageNumber, req.OrderByAscending))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to read the register: %v", err)
	}

	return &pb.ListPostingsResponse{Postings: postings, TotalCount: total}, nil
}

// checkMetadataFilter refuses a malformed filter pair. A pair is malformed when
// its key is empty: there is nothing to match on, so the caller is told rather
// than handed a listing that quietly filtered on nothing useful. An empty value
// is well formed — it asks for a key stored with an empty value.
func checkMetadataFilter(pairs map[string]string) error {
	if _, malformed := pairs[""]; malformed {
		return status.Error(codes.InvalidArgument, "a metadata filter pair needs a non-empty key")
	}
	return nil
}

// accountFilter unpacks an account filter. The user and the name keep their
// presence: unset is "not filtered", while empty matches only the empty string.
func accountFilter(filter *pb.AccountFilter) (accountType string, user, name *string) {
	if filter == nil {
		return "", nil, nil
	}
	return accountfmt.AccountTypeToString(filter.Type), filter.User, filter.Name
}

func pageOf(size, number int32, ascending bool) repository.Page {
	return repository.Page{Size: size, Number: number, Ascending: ascending}
}

func dateOrNil(date *timestamppb.Timestamp) *time.Time {
	if date == nil {
		return nil
	}
	moment := date.AsTime()
	return &moment
}
