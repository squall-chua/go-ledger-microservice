package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

var (
	cash    = account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Checking")
	savings = account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Savings")
	opening = account(pb.AccountType_ACCOUNT_TYPE_EQUITIES, "", "Opening")
	rent    = account(pb.AccountType_ACCOUNT_TYPE_EXPENSES, "alice", "Rent")
)

func TestRecordTransactionStoresPostingsWithRunningBalances(t *testing.T) {
	h := newHarness(t)

	first := h.mustRecord(transfer("key-1", "opening deposit", opening, cash, usd(100, 0)))
	require.Len(t, first.Postings, 2)
	assert.Equal(t, cash.Name, first.Postings[0].Account.Name)
	assertMoney(t, usd(100, 0), first.Postings[0].Amount)
	assertMoney(t, usd(100, 0), first.Postings[0].Balance)
	assertMoney(t, usd(-100, 0), first.Postings[1].Balance)
	assert.NotEmpty(t, first.Id)
	assert.Equal(t, first.Id, first.Postings[0].TransactionId)

	// The running balance of an account carries on across transactions.
	second := h.mustRecord(transfer("key-2", "another deposit", opening, cash, usd(50, 0)))
	assertMoney(t, usd(150, 0), second.Postings[0].Balance)
	assertMoney(t, usd(-150, 0), second.Postings[1].Balance)
}

func TestListAccountBalancesWithAnExactAccountReturnsOneBalancePerCurrency(t *testing.T) {
	h := newHarness(t)

	h.mustRecord(transfer("key-usd", "usd deposit", opening, cash, usd(100, 0)))
	h.mustRecord(transfer("key-eur", "eur deposit", opening, cash, amount("EUR", 20, 0)))
	h.mustRecord(transfer("key-other", "someone else", opening, savings, usd(7, 0)))

	balances := h.balances(&pb.ListAccountBalancesRequest{Account: cash})
	require.Len(t, balances, 2)
	assert.Equal(t, cash.Name, balances[0].Account.Name)
	assertMoney(t, amount("EUR", 20, 0), balances[0].Balance)
	assertMoney(t, usd(100, 0), balances[1].Balance)

	// Narrowing by currency leaves one.
	balances = h.balances(&pb.ListAccountBalancesRequest{Account: cash, CurrencyCode: "USD"})
	require.Len(t, balances, 1)
	assertMoney(t, usd(100, 0), balances[0].Balance)

	// The account is matched exactly: nothing rolls up and nothing is a pattern.
	balances = h.balances(&pb.ListAccountBalancesRequest{
		Account: account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Check"),
	})
	assert.Empty(t, balances)
}

func TestListAccountBalancesWithNoFilterReturnsTheTrialBalance(t *testing.T) {
	h := newHarness(t)

	h.mustRecord(transfer("key-1", "opening deposit", opening, cash, usd(100, 0)))
	h.mustRecord(transfer("key-2", "rent", cash, rent, usd(30, 0)))

	balances := h.balances(&pb.ListAccountBalancesRequest{})
	require.Len(t, balances, 3)

	sum := int64(0)
	for _, balance := range balances {
		sum += balance.Balance.Units
	}
	assert.Zero(t, sum, "a trial balance sums to zero")

	assertMoney(t, usd(70, 0), balanceOf(t, balances, cash))
	assertMoney(t, usd(30, 0), balanceOf(t, balances, rent))
	assertMoney(t, usd(-100, 0), balanceOf(t, balances, opening))
}

func TestRecordTransactionRefusesAMalformedTransaction(t *testing.T) {
	h := newHarness(t)

	cases := map[string]*pb.RecordTransactionRequest{
		"postings do not sum to zero": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, 0)),
				posting(opening, usd(-99, 0)),
			},
		},
		"fewer than two postings": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{posting(cash, usd(100, 0))},
		},
		"no postings at all": {IdempotencyKey: "key", Note: "note"},
		"a zero amount": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(0, 0)),
				posting(opening, usd(0, 0)),
			},
		},
		"differing currencies": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, 0)),
				posting(opening, amount("EUR", -100, 0)),
			},
		},
		"a missing amount": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, nil),
				posting(opening, usd(-100, 0)),
			},
		},
		"an empty currency code": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, amount("", 100, 0)),
				posting(opening, amount("", -100, 0)),
			},
		},
		"out of range nanos": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, 1000000000)),
				posting(opening, usd(-100, 0)),
			},
		},
		"units and nanos disagreeing on sign": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, -500000000)),
				posting(opening, usd(-100, 0)),
			},
		},
		"an empty note": {
			IdempotencyKey: "key",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, 0)),
				posting(opening, usd(-100, 0)),
			},
		},
		"an empty idempotency key": {
			Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, 0)),
				posting(opening, usd(-100, 0)),
			},
		},
		"a missing account": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(nil, usd(100, 0)),
				posting(opening, usd(-100, 0)),
			},
		},
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := h.record(request)
			requireCode(t, err, codes.InvalidArgument)
			assert.Empty(t, h.balances(&pb.ListAccountBalancesRequest{}), "a refused transaction changes nothing")
		})
	}
}

func TestRecordTransactionRefusesAnUnspecifiedAccountType(t *testing.T) {
	h := newHarness(t)

	_, err := h.record(transfer("key", "note",
		account(pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED, "alice", "Checking"), opening, usd(100, 0)))

	requireCode(t, err, codes.InvalidArgument)
	assert.Empty(t, h.balances(&pb.ListAccountBalancesRequest{}))
}

func TestMoneyRoundTripsExactlyAtNineDigits(t *testing.T) {
	h := newHarness(t)

	transaction := h.mustRecord(transfer("key", "precise", opening, cash, usd(1, 123456789)))
	assertMoney(t, usd(1, 123456789), transaction.Postings[0].Amount)
	assertMoney(t, usd(1, 123456789), transaction.Postings[0].Balance)

	balances := h.balances(&pb.ListAccountBalancesRequest{Account: cash})
	require.Len(t, balances, 1)
	assertMoney(t, usd(1, 123456789), balances[0].Balance)
}

func TestIdsAreTimeOrdered(t *testing.T) {
	h := newHarness(t)

	first := h.mustRecord(transfer("key-1", "first", opening, cash, usd(1, 0)))
	second := h.mustRecord(transfer("key-2", "second", opening, cash, usd(1, 0)))

	for _, id := range []string{first.Id, second.Id, first.Postings[0].Id, first.Postings[1].Id} {
		parsed, err := uuid.Parse(id)
		require.NoError(t, err)
		assert.Equal(t, uuid.Version(7), parsed.Version(), "ids are UUID v7 so they sort by time")
	}
	assert.Less(t, first.Id, second.Id)
	assert.Less(t, first.Postings[0].Id, first.Postings[1].Id)
}

func TestMetadataAndSuppliedDateRoundTrip(t *testing.T) {
	h := newHarness(t)

	date := time.Now().UTC().Truncate(time.Millisecond)
	request := transfer("key", "with metadata", opening, cash, usd(5, 0))
	request.Date = timestamppb.New(date)
	request.Metadata = map[string]string{"order": "42", "source": "checkout"}

	transaction := h.mustRecord(request)
	assert.Equal(t, map[string]string{"order": "42", "source": "checkout"}, transaction.Metadata)
	assert.True(t, date.Equal(transaction.Date.AsTime()))

	// A transaction with no metadata comes back as an empty map, not a null.
	plain := h.mustRecord(transfer("key-2", "plain", opening, cash, usd(5, 0)))
	assert.Empty(t, plain.Metadata)
}

func TestTheListingRPCsAreNotImplementedYet(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListTransactions(h.ctx, &pb.ListTransactionsRequest{})
	requireCode(t, err, codes.Unimplemented)

	_, err = h.client.ListPostings(h.ctx, &pb.ListPostingsRequest{})
	requireCode(t, err, codes.Unimplemented)
}

func TestACallerWithoutATokenIsRefused(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListAccountBalances(t.Context(), &pb.ListAccountBalancesRequest{})
	requireCode(t, err, codes.Unauthenticated)
}

func assertMoney(t *testing.T, want, got *money.Money) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.CurrencyCode, got.CurrencyCode)
	assert.Equal(t, want.Units, got.Units)
	assert.Equal(t, want.Nanos, got.Nanos)
}

func balanceOf(t *testing.T, balances []*pb.AccountBalance, want *pb.Account) *money.Money {
	t.Helper()
	for _, balance := range balances {
		if balance.Account.Type == want.Type && balance.Account.User == want.User && balance.Account.Name == want.Name {
			return balance.Balance
		}
	}
	t.Fatalf("no balance for account %v", want)
	return nil
}
