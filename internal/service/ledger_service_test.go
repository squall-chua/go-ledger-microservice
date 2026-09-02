package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
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

	balances := h.balances(&pb.ListAccountBalancesRequest{Account: exactly(cash)})
	require.Len(t, balances, 2)
	assert.Equal(t, cash.Name, balances[0].Account.Name)
	assertMoney(t, amount("EUR", 20, 0), balances[0].Balance)
	assertMoney(t, usd(100, 0), balances[1].Balance)

	// Narrowing by currency leaves one.
	balances = h.balances(&pb.ListAccountBalancesRequest{Account: exactly(cash), CurrencyCode: "USD"})
	require.Len(t, balances, 1)
	assertMoney(t, usd(100, 0), balances[0].Balance)

	// The account is matched exactly: nothing rolls up and nothing is a pattern.
	balances = h.balances(&pb.ListAccountBalancesRequest{
		Account: exactly(account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Check")),
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
		"an incomplete account to verify": {
			IdempotencyKey: "key", Note: "note",
			Postings: []*pb.RecordTransactionRequest_PostingInput{
				posting(cash, usd(100, 0)),
				posting(opening, usd(-100, 0)),
			},
			VerifyNonNegativeBalances: []*pb.Account{
				account(pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED, "alice", "Checking"),
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

	balances := h.balances(&pb.ListAccountBalancesRequest{Account: exactly(cash)})
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

	// A transaction with no metadata is an empty map, never a null. The wire
	// cannot carry that distinction — proto3 encodes an empty map field and an
	// absent one identically, so the map arrives nil and asserting it is empty
	// would pass on a null too — so the assertion is on the stored JSON.
	plain := h.mustRecord(transfer("key-2", "plain", opening, cash, usd(5, 0)))
	var stored string
	require.NoError(t, h.db.QueryRow(
		"SELECT metadata FROM transactions WHERE id = $1", plain.Id).Scan(&stored))
	assert.Equal(t, "{}", stored)
}

func TestEveryPostingCarriesTheTransactionDate(t *testing.T) {
	h := newHarness(t)
	recorded := h.mustRecord(onDay(1, "key-1", "opening deposit", opening, cash, usd(100, 0)))

	for _, posting := range recorded.Postings {
		assert.True(t, day(1).Equal(posting.Date.AsTime()), "RecordTransaction response")
	}

	listed := h.transactions(&pb.ListTransactionsRequest{}).Transactions[0]
	for _, posting := range listed.Postings {
		assert.True(t, day(1).Equal(posting.Date.AsTime()), "ListTransactions")
	}

	register := h.register(&pb.ListPostingsRequest{Filter: &pb.PostingFilter{Account: exactly(cash)}})
	require.Len(t, register.Postings, 1)
	assert.True(t, day(1).Equal(register.Postings[0].Date.AsTime()), "ListPostings")
}

// The date a transaction reports does not depend on which RPC is asked for it.
// Storage resolves to the microsecond, so a supplied date is truncated once on
// the way in and that single value reaches the record response, the listing and
// the register alike.
func TestASuppliedDateReadsTheSameFromEveryRPC(t *testing.T) {
	h := newHarness(t)

	supplied := time.Date(2026, time.March, 10, 12, 0, 0, 123456789, time.UTC)
	want := supplied.Truncate(time.Microsecond)
	require.NotEqual(t, supplied, want, "the supplied date carries nanoseconds storage cannot hold")

	request := transfer("nanoseconds", "a deposit", opening, cash, usd(100, 0))
	request.Date = timestamppb.New(supplied)

	recorded, err := h.record(request)
	require.NoError(t, err)
	assertDate(t, want, recorded.Transaction.Date, "RecordTransaction")
	for _, posting := range recorded.Transaction.Postings {
		assertDate(t, want, posting.Date, "RecordTransaction posting")
	}

	listed := h.transactions(&pb.ListTransactionsRequest{}).Transactions[0]
	assertDate(t, want, listed.Date, "ListTransactions")
	for _, posting := range listed.Postings {
		assertDate(t, want, posting.Date, "ListTransactions posting")
	}

	register := h.register(&pb.ListPostingsRequest{Filter: &pb.PostingFilter{Account: exactly(cash)}})
	require.Len(t, register.Postings, 1)
	assertDate(t, want, register.Postings[0].Date, "ListPostings")

	// The replay is answered out of the listing query, so the truncation has to
	// have happened before the fingerprint: were the hash taken over the
	// nanoseconds, the same request sent twice would still replay, but it would
	// stand for a date that was never stored.
	replay, err := h.record(request)
	require.NoError(t, err)
	assert.True(t, replay.Replayed, "a retry with identical content is a replay")
	assertDate(t, want, replay.Transaction.Date, "the replay")
}

// TestASuppliedDateDiffersFromCreatedAt pins the distinction CONTEXT.md draws
// between the Transaction date and created_at: a Posting carries both, and
// they are not the same field wearing two names.
func TestASuppliedDateDiffersFromCreatedAt(t *testing.T) {
	h := newHarness(t)

	// Both accounts are brand new, so a date years old is a supplied date, not
	// a backdated one: nothing precedes this transaction.
	old := transfer("old", "an old event", opening, cash, usd(10, 0))
	suppliedDate := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	old.Date = timestamppb.New(suppliedDate)

	transaction := h.mustRecord(old)
	for _, posting := range transaction.Postings {
		assert.True(t, suppliedDate.Equal(posting.Date.AsTime()), "posting date is the supplied date")
		assert.WithinDuration(t, time.Now(), posting.CreatedAt.AsTime(), time.Minute, "created_at is roughly now")
	}
}

func TestListTransactionsPagesNewestFirstWithATotalCount(t *testing.T) {
	h := newHarness(t)
	h.recordDays(3, opening, cash)

	page := h.transactions(&pb.ListTransactionsRequest{PageSize: 2})
	assert.EqualValues(t, 3, page.TotalCount, "the total counts every match, not only the page")
	assert.Equal(t, []string{"note 3", "note 2"}, notesOf(page.Transactions), "newest first by default")

	// Every transaction comes back with its own postings.
	require.Len(t, page.Transactions[0].Postings, 2)
	assertMoney(t, usd(3, 0), page.Transactions[0].Postings[0].Amount)
	assertMoney(t, usd(6, 0), page.Transactions[0].Postings[0].Balance, "1 + 2 + 3 landed in cash")
	assert.Equal(t, page.Transactions[0].Id, page.Transactions[0].Postings[0].TransactionId)

	// The page is taken over transactions, so the second page holds the rest.
	next := h.transactions(&pb.ListTransactionsRequest{PageSize: 2, PageNumber: 2})
	assert.EqualValues(t, 3, next.TotalCount)
	assert.Equal(t, []string{"note 1"}, notesOf(next.Transactions))

	// Ascending walks the same rows from the other end, and pages the same way:
	// the second page picks up where the first left off rather than restarting.
	ascending := h.transactions(&pb.ListTransactionsRequest{PageSize: 2, OrderByAscending: true})
	assert.EqualValues(t, 3, ascending.TotalCount)
	assert.Equal(t, []string{"note 1", "note 2"}, notesOf(ascending.Transactions), "oldest first when asked")

	nextAscending := h.transactions(&pb.ListTransactionsRequest{
		PageSize: 2, PageNumber: 2, OrderByAscending: true})
	assert.EqualValues(t, 3, nextAscending.TotalCount)
	assert.Equal(t, []string{"note 3"}, notesOf(nextAscending.Transactions))
}

func TestListTransactionsUsesAHalfOpenDateRange(t *testing.T) {
	h := newHarness(t)
	h.recordDays(3, opening, cash)

	page := h.transactions(&pb.ListTransactionsRequest{Filter: &pb.TransactionFilter{
		StartDate: timestamppb.New(day(1)),
		EndDate:   timestamppb.New(day(3)),
	}})

	assert.EqualValues(t, 2, page.TotalCount)
	assert.Equal(t, []string{"note 2", "note 1"}, notesOf(page.Transactions),
		"the start of the range is included and the end excluded")
}

func TestListTransactionsFindsOneTransactionByItsIdempotencyKey(t *testing.T) {
	h := newHarness(t)
	h.recordDays(3, opening, cash)

	page := h.transactions(&pb.ListTransactionsRequest{
		Filter: &pb.TransactionFilter{IdempotencyKey: "key-2"},
	})
	assert.EqualValues(t, 1, page.TotalCount)
	assert.Equal(t, []string{"note 2"}, notesOf(page.Transactions))

	unknown := h.transactions(&pb.ListTransactionsRequest{
		Filter: &pb.TransactionFilter{IdempotencyKey: "never-recorded"},
	})
	assert.Zero(t, unknown.TotalCount)
	assert.Empty(t, unknown.Transactions)
}

func TestPageSizeDefaultsToTenAndIsClampedAtAHundred(t *testing.T) {
	h := newHarness(t)
	// A hundred and one transactions, two hundred and two postings: with fewer
	// rows than the maximum a clamped page and an unclamped one hold the same
	// thing, so the clamp would pass this test even if it were gone.
	h.recordDays(101, opening, cash)

	page := h.transactions(&pb.ListTransactionsRequest{})
	assert.Len(t, page.Transactions, 10, "a caller that asks for no page size gets ten")
	assert.EqualValues(t, 101, page.TotalCount)

	// A caller asking for more than the maximum is clamped rather than refused.
	clamped := h.transactions(&pb.ListTransactionsRequest{PageSize: 1000})
	assert.Len(t, clamped.Transactions, 100, "a page of transactions stops at a hundred")
	assert.EqualValues(t, 101, clamped.TotalCount, "the total still counts every match")

	// The register is bounded by the same page, and never returns the lot.
	register := h.register(&pb.ListPostingsRequest{})
	assert.Len(t, register.Postings, 10, "a caller that asks for no page size gets ten")
	assert.EqualValues(t, 202, register.TotalCount)

	clampedRegister := h.register(&pb.ListPostingsRequest{PageSize: 1000})
	assert.Len(t, clampedRegister.Postings, 100, "a page of postings stops at a hundred")
	assert.EqualValues(t, 202, clampedRegister.TotalCount)
}

// A page number past the end is an empty page however far past it is: the
// offset it works out to is computed in int64, so a caller-supplied page number
// cannot overflow into the negative OFFSET Postgres refuses.
func TestAPageNumberPastTheEndIsAnEmptyPage(t *testing.T) {
	h := newHarness(t)
	h.recordDays(3, opening, cash)

	for _, number := range []int32{4, 33554432, 2147483647} {
		listed, err := h.client.ListTransactions(h.ctx,
			&pb.ListTransactionsRequest{PageSize: 100, PageNumber: number})
		require.NoError(t, err, "page number %d", number)
		assert.Empty(t, listed.Transactions, "page number %d holds no transaction", number)
		assert.EqualValues(t, 3, listed.TotalCount, "page number %d still counts every match", number)

		register, err := h.client.ListPostings(h.ctx,
			&pb.ListPostingsRequest{PageSize: 100, PageNumber: number})
		require.NoError(t, err, "page number %d", number)
		assert.Empty(t, register.Postings, "page number %d holds no posting", number)
		assert.EqualValues(t, 6, register.TotalCount, "page number %d still counts every match", number)
	}
}

// Transactions may share a transaction date, so both listings break the tie on
// id. Without that tie-break the rows a page holds are whatever the planner
// chose that time, and walking the pages repeats one row while skipping another.
func TestPagingThroughTiedDatesReturnsEveryRowOnce(t *testing.T) {
	h := newHarness(t)
	for number := 1; number <= 5; number++ {
		tied := transfer(fmt.Sprintf("tied-%d", number), fmt.Sprintf("note %d", number),
			opening, cash, usd(int64(number), 0))
		tied.Date = timestamppb.New(day(1))
		h.mustRecord(tied)
	}

	for _, ascending := range []bool{false, true} {
		walked := []string{}
		for number := int32(1); number <= 5; number++ {
			page := h.transactions(&pb.ListTransactionsRequest{
				PageSize: 2, PageNumber: number, OrderByAscending: ascending})
			assert.EqualValues(t, 5, page.TotalCount)
			walked = append(walked, notesOf(page.Transactions)...)
		}
		assert.ElementsMatch(t,
			[]string{"note 1", "note 2", "note 3", "note 4", "note 5"}, walked,
			"every transaction sharing the date is on exactly one page (ascending %v)", ascending)
	}

	seen := map[string]bool{}
	walked := 0
	for number := int32(1); number <= 5; number++ {
		page := h.register(&pb.ListPostingsRequest{
			Filter: &pb.PostingFilter{Account: exactly(cash)}, PageSize: 2, PageNumber: number})
		assert.EqualValues(t, 5, page.TotalCount)
		for _, posting := range page.Postings {
			seen[posting.Id] = true
			walked++
		}
	}
	assert.Equal(t, 5, walked, "the register hands out one page each and then stops")
	assert.Len(t, seen, 5, "no posting sharing the date is paged twice")
}

// The transaction listing counts transactions, not postings: the page is taken
// over transactions and joined to their postings afterwards, so a transaction
// carrying more postings than another must not weigh more in either number.
func TestTheTotalCountIsUnmovedByDifferingPostingCounts(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(onDayInto(1, "two", "note 1", opening, cash))
	h.mustRecord(onDayInto(2, "three", "note 2", opening, cash, savings))
	h.mustRecord(onDayInto(3, "four", "note 3", opening, cash, savings, rent))

	all := h.transactions(&pb.ListTransactionsRequest{})
	assert.EqualValues(t, 3, all.TotalCount, "three transactions, whatever they carry")
	assert.Equal(t, []int{4, 3, 2}, postingCountsOf(all.Transactions), "each keeps all its postings")

	// A page of two is two whole transactions, seven postings between them.
	page := h.transactions(&pb.ListTransactionsRequest{PageSize: 2})
	assert.EqualValues(t, 3, page.TotalCount)
	assert.Equal(t, []int{4, 3}, postingCountsOf(page.Transactions),
		"a long transaction does not eat into the page size")
	next := h.transactions(&pb.ListTransactionsRequest{PageSize: 2, PageNumber: 2})
	assert.EqualValues(t, 3, next.TotalCount)
	assert.Equal(t, []int{2}, postingCountsOf(next.Transactions))

	// The register counts postings, which is the other number: 2 + 3 + 4.
	assert.EqualValues(t, 9, h.register(&pb.ListPostingsRequest{}).TotalCount)
}

func TestListTransactionsFiltersByExactMetadataPairs(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(tagged(onDay(1, "key-1", "note 1", opening, cash, usd(1, 0)),
		map[string]string{"order": "42", "source": "checkout"}))
	h.mustRecord(tagged(onDay(2, "key-2", "note 2", opening, cash, usd(2, 0)),
		map[string]string{"order": "43", "source": "checkout"}))
	h.mustRecord(onDay(3, "key-3", "note 3", opening, cash, usd(3, 0)))

	byMetadata := func(pairs map[string]string) *pb.ListTransactionsResponse {
		return h.transactions(&pb.ListTransactionsRequest{Filter: &pb.TransactionFilter{Metadata: pairs}})
	}

	matched := byMetadata(map[string]string{"source": "checkout"})
	assert.EqualValues(t, 2, matched.TotalCount)
	assert.Equal(t, []string{"note 2", "note 1"}, notesOf(matched.Transactions))
	assert.Equal(t, map[string]string{"order": "43", "source": "checkout"}, matched.Transactions[0].Metadata,
		"the pairs come back out of storage as they went in")

	assert.Equal(t, []string{"note 1"}, notesOf(byMetadata(map[string]string{"order": "42"}).Transactions))
	assert.Empty(t, byMetadata(map[string]string{"order": "4"}).Transactions,
		"a value is matched whole, never as a prefix")
	assert.Empty(t, byMetadata(map[string]string{"Order": "42"}).Transactions, "a key is matched exactly")

	// Several pairs are ANDed: every one of them has to match.
	assert.Equal(t, []string{"note 1"},
		notesOf(byMetadata(map[string]string{"order": "42", "source": "checkout"}).Transactions))
	assert.Empty(t, byMetadata(map[string]string{"order": "42", "source": "refund"}).Transactions,
		"one pair matching is not enough")

	// No pairs at all is not filtered on, and the transaction recorded without
	// metadata reads back as an empty map.
	all := h.transactions(&pb.ListTransactionsRequest{Filter: &pb.TransactionFilter{}})
	assert.EqualValues(t, 3, all.TotalCount)
	assert.Empty(t, all.Transactions[0].Metadata)
}

func TestAMetadataPairWithAnEmptyKeyIsRefused(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListTransactions(h.ctx, &pb.ListTransactionsRequest{
		Filter: &pb.TransactionFilter{Metadata: map[string]string{"": "42"}},
	})
	requireCode(t, err, codes.InvalidArgument)

	_, err = h.client.ListPostings(h.ctx, &pb.ListPostingsRequest{
		Filter: &pb.PostingFilter{Metadata: map[string]string{"": "42"}},
	})
	requireCode(t, err, codes.InvalidArgument)

	// The write path is as strict as the filter path: a pair no filter could
	// ever ask for again is refused rather than stored.
	_, err = h.client.RecordTransaction(h.ctx,
		tagged(transfer("key-1", "note", opening, cash, usd(5, 0)), map[string]string{"": "x"}))
	requireCode(t, err, codes.InvalidArgument)
}

func TestListPostingsReturnsOneAccountsRegisterWithRunningBalances(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(onDay(1, "key-1", "opening deposit", opening, cash, usd(100, 0)))
	h.mustRecord(onDay(2, "key-2", "rent", cash, rent, usd(30, 0)))
	h.mustRecord(onDay(3, "key-3", "to savings", cash, savings, usd(20, 0)))

	register := h.register(&pb.ListPostingsRequest{
		Filter:           &pb.PostingFilter{Account: exactly(cash)},
		OrderByAscending: true,
	})

	assert.EqualValues(t, 3, register.TotalCount)
	require.Len(t, register.Postings, 3)
	for _, posting := range register.Postings {
		assert.Equal(t, cash.Name, posting.Account.Name, "the register holds one account only")
		assert.Equal(t, cash.User, posting.Account.User)
	}

	// Each entry carries its own amount and the balance left after it.
	assertMoney(t, usd(100, 0), register.Postings[0].Amount)
	assertMoney(t, usd(100, 0), register.Postings[0].Balance)
	assertMoney(t, usd(-30, 0), register.Postings[1].Amount)
	assertMoney(t, usd(70, 0), register.Postings[1].Balance)
	assertMoney(t, usd(-20, 0), register.Postings[2].Amount)
	assertMoney(t, usd(50, 0), register.Postings[2].Balance)

	newestFirst := h.register(&pb.ListPostingsRequest{Filter: &pb.PostingFilter{Account: exactly(cash)}})
	assertMoney(t, usd(50, 0), newestFirst.Postings[0].Balance, "newest first by default")
}

func TestListPostingsFiltersTheRegisterExactly(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(onDay(1, "key-usd", "usd deposit", opening, cash, usd(100, 0)))
	h.mustRecord(onDay(2, "key-eur", "eur deposit", opening, cash, amount("EUR", 20, 0)))
	h.mustRecord(onDay(3, "key-savings", "savings deposit", opening, savings, usd(7, 0)))

	total := func(filter *pb.PostingFilter) int64 {
		return h.register(&pb.ListPostingsRequest{Filter: filter}).TotalCount
	}

	assert.EqualValues(t, 2, total(&pb.PostingFilter{Account: exactly(cash)}), "both currencies of one account")
	assert.EqualValues(t, 1, total(&pb.PostingFilter{Account: exactly(cash), CurrencyCode: "USD"}))
	assert.EqualValues(t, 3, total(&pb.PostingFilter{
		Account: &pb.AccountFilter{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS}}), "by type alone")
	assert.EqualValues(t, 3, total(&pb.PostingFilter{
		Account: &pb.AccountFilter{User: proto.String("alice")}}), "by user alone")
	assert.EqualValues(t, 1, total(&pb.PostingFilter{
		Account: &pb.AccountFilter{Name: proto.String("Savings")}}), "by name alone")
	assert.EqualValues(t, 0, total(&pb.PostingFilter{
		Account: &pb.AccountFilter{Name: proto.String("Check")}}), "a name is never a prefix")

	// The register takes the same half-open date range as the listing.
	assert.EqualValues(t, 4, total(&pb.PostingFilter{
		StartDate: timestamppb.New(day(1)),
		EndDate:   timestamppb.New(day(3)),
	}), "two transactions of two postings each")
}

func TestListPostingsFiltersTheRegisterByItsParentTransactionsMetadata(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(tagged(onDay(1, "key-1", "rent", cash, rent, usd(30, 0)),
		map[string]string{"source": "checkout"}))
	h.mustRecord(onDay(2, "key-2", "groceries", cash, rent, usd(5, 0)))

	// A posting has no metadata of its own, so the pairs are matched against
	// the transaction it belongs to.
	register := h.register(&pb.ListPostingsRequest{Filter: &pb.PostingFilter{
		Account:  exactly(cash),
		Metadata: map[string]string{"source": "checkout"},
	}})
	assert.EqualValues(t, 1, register.TotalCount)
	require.Len(t, register.Postings, 1)
	assertMoney(t, usd(-30, 0), register.Postings[0].Amount)

	assert.EqualValues(t, 2, h.register(&pb.ListPostingsRequest{
		Filter: &pb.PostingFilter{Account: exactly(cash)}}).TotalCount, "unfiltered, both legs are there")
	assert.Zero(t, h.register(&pb.ListPostingsRequest{Filter: &pb.PostingFilter{
		Metadata: map[string]string{"source": "refund"}}}).TotalCount)
}

func TestAnEmptyUserIsFilteredForExactly(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(onDay(1, "key-1", "opening deposit", opening, cash, usd(100, 0)))

	// `opening` has no user at all. Asking for that user must not also return
	// the accounts that have one.
	balances := h.balances(&pb.ListAccountBalancesRequest{
		Account: &pb.AccountFilter{User: proto.String("")},
	})
	require.Len(t, balances, 1)
	assert.Equal(t, opening.Name, balances[0].Account.Name)

	register := h.register(&pb.ListPostingsRequest{
		Filter: &pb.PostingFilter{Account: &pb.AccountFilter{User: proto.String("")}},
	})
	require.Len(t, register.Postings, 1)
	assert.Equal(t, opening.Name, register.Postings[0].Account.Name)

	// Leaving the user out entirely still means every user.
	assert.Len(t, h.balances(&pb.ListAccountBalancesRequest{Account: &pb.AccountFilter{}}), 2)
	assert.EqualValues(t, 2, h.register(&pb.ListPostingsRequest{Filter: &pb.PostingFilter{}}).TotalCount)
}

func TestACallerWithoutATokenIsRefused(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListAccountBalances(t.Context(), &pb.ListAccountBalancesRequest{})
	requireCode(t, err, codes.Unauthenticated)
}

// day fixes a transaction date, so what the listings order by is the supplied
// date and never the clock.
func day(number int) time.Time {
	return time.Date(2026, time.March, number, 12, 0, 0, 0, time.UTC)
}

func onDay(number int, key, note string, from, to *pb.Account, amount *money.Money) *pb.RecordTransactionRequest {
	request := transfer(key, note, from, to, amount)
	request.Date = timestamppb.New(day(number))
	return request
}

// onDayInto is a transaction with more postings than the usual two: one unit
// out of `from` for each account in `to`, and one unit into each of them.
func onDayInto(number int, key, note string, from *pb.Account, to ...*pb.Account) *pb.RecordTransactionRequest {
	postings := []*pb.RecordTransactionRequest_PostingInput{posting(from, usd(int64(-len(to)), 0))}
	for _, into := range to {
		postings = append(postings, posting(into, usd(1, 0)))
	}
	return &pb.RecordTransactionRequest{
		IdempotencyKey: key,
		Note:           note,
		Date:           timestamppb.New(day(number)),
		Postings:       postings,
	}
}

// tagged attaches metadata to a transaction about to be recorded.
func tagged(request *pb.RecordTransactionRequest, metadata map[string]string) *pb.RecordTransactionRequest {
	request.Metadata = metadata
	return request
}

// recordDays records one transaction per day, "note 1" on day 1 and so on,
// moving that many units into `to`.
func (h *harness) recordDays(days int, from, to *pb.Account) {
	h.t.Helper()
	for number := 1; number <= days; number++ {
		h.mustRecord(onDay(number,
			fmt.Sprintf("key-%d", number), fmt.Sprintf("note %d", number),
			from, to, usd(int64(number), 0)))
	}
}

func notesOf(transactions []*pb.Transaction) []string {
	notes := make([]string, len(transactions))
	for i, transaction := range transactions {
		notes[i] = transaction.Note
	}
	return notes
}

func postingCountsOf(transactions []*pb.Transaction) []int {
	counts := make([]int, len(transactions))
	for i, transaction := range transactions {
		counts[i] = len(transaction.Postings)
	}
	return counts
}

func assertDate(t *testing.T, want time.Time, got *timestamppb.Timestamp, where string) {
	t.Helper()
	require.NotNil(t, got, where)
	assert.True(t, want.Equal(got.AsTime()), "%s reports %s, want %s",
		where, got.AsTime().Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
}

func assertMoney(t *testing.T, want, got *money.Money, msgAndArgs ...any) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.CurrencyCode, got.CurrencyCode, msgAndArgs...)
	assert.Equal(t, want.Units, got.Units, msgAndArgs...)
	assert.Equal(t, want.Nanos, got.Nanos, msgAndArgs...)
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

func TestConcurrentOppositeTransfersDoNotDeadlock(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(transfer("opening", "opening deposit", opening, cash, usd(1000, 0)))

	// Half the writers move money one way between the same two accounts and
	// half move it the other way, so their postings name the accounts in
	// opposite orders. Whatever order the rows are locked in has to be the same
	// for both halves or they deadlock on each other.
	const pairs = 20
	errs := make(chan error, 2*pairs)
	var wg sync.WaitGroup
	for i := range pairs {
		for _, request := range []*pb.RecordTransactionRequest{
			transfer(fmt.Sprintf("out-%d", i), "cash to savings", cash, savings, usd(1, 0)),
			transfer(fmt.Sprintf("in-%d", i), "savings to cash", savings, cash, usd(1, 0)),
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := h.record(request)
				errs <- err
			}()
		}
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err, "concurrent opposite transfers must all succeed")
	}
	assertMoney(t, usd(1000, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{Account: exactly(cash)}), cash))
}

// A write takes the balance row of every account it touches, so two writes to
// one account wait for each other while two writes to different accounts do
// not. The lock is held here from outside the service, on the same database, so
// the two halves can be told apart rather than raced.
func TestAWriteBlocksOnlyOnTheAccountsItTouches(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(transfer("opening", "opening deposit", opening, cash, usd(100, 0)))

	locker, err := h.db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	defer locker.Rollback() //nolint:errcheck // releasing the lock is the point
	_, err = locker.ExecContext(t.Context(),
		"SELECT 1 FROM account_balances WHERE account_name = $1 FOR UPDATE", cash.Name)
	require.NoError(t, err)

	// Savings is a different account, so this write never meets that lock.
	elsewhere, cancelElsewhere := context.WithTimeout(h.ctx, 30*time.Second)
	defer cancelElsewhere()
	_, err = h.client.RecordTransaction(elsewhere,
		transfer("elsewhere", "into savings", opening, savings, usd(1, 0)))
	require.NoError(t, err, "a write to a different account must not wait")

	// A write to the locked account serializes behind it, so it gets no further
	// than its deadline.
	behind, cancelBehind := context.WithTimeout(h.ctx, time.Second)
	defer cancelBehind()
	_, err = h.client.RecordTransaction(behind,
		transfer("behind", "into checking", opening, cash, usd(1, 0)))
	requireCode(t, err, codes.DeadlineExceeded)
}

func TestABackdatedTransactionIsRefused(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(onDay(2, "key-2", "a deposit", opening, cash, usd(50, 0)))

	_, err := h.record(onDay(1, "key-1", "the day before", opening, cash, usd(10, 0)))
	requireCode(t, err, codes.FailedPrecondition)
	assert.Contains(t, err.Error(), "ASSETS:alice:Checking", "the refusal names the account that blocked it")

	// The refusal leaves no trace, and the same date is fine on accounts that
	// have no later posting.
	assertMoney(t, usd(50, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
	h.mustRecord(onDay(1, "key-1", "the day before, elsewhere", savings, rent, usd(10, 0)))
}

func TestTheFirstPostingToANewAccountIsNeverBackdated(t *testing.T) {
	h := newHarness(t)

	// Both accounts are brand new and the date is years old. Nothing precedes
	// this transaction, so nothing can refuse it.
	old := transfer("old", "an old event", opening, cash, usd(10, 0))
	old.Date = timestamppb.New(time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC))

	transaction := h.mustRecord(old)
	assert.Equal(t, 2020, transaction.Date.AsTime().Year())
}

func TestASuppliedDateTooFarAheadIsRefused(t *testing.T) {
	h := newHarness(t)

	tooFar := transfer("too-far", "a broken clock", opening, cash, usd(1, 0))
	tooFar.Date = timestamppb.New(time.Now().Add(6 * time.Minute))
	_, err := h.record(tooFar)
	requireCode(t, err, codes.InvalidArgument)

	// Inside the five-minute tolerance a date ahead of now is accepted: clocks
	// between the caller and the ledger drift.
	nearby := transfer("nearby", "a drifting clock", opening, cash, usd(1, 0))
	nearby.Date = timestamppb.New(time.Now().Add(4 * time.Minute))
	h.mustRecord(nearby)
}

func TestAnOmittedDateIsStampedAndNeverRefused(t *testing.T) {
	h := newHarness(t)

	// A supplied date inside the tolerance leaves Checking with a posting ahead
	// of the clock. The next caller omits its date and is stamped past that
	// posting rather than refused for a clock it does not control.
	ahead := time.Now().Add(4 * time.Minute).UTC().Truncate(time.Millisecond)
	drifted := transfer("ahead", "a drifting clock", opening, cash, usd(10, 0))
	drifted.Date = timestamppb.New(ahead)
	h.mustRecord(drifted)

	stamped := h.mustRecord(transfer("stamped", "no date at all", opening, cash, usd(5, 0)))
	assert.True(t, stamped.Date.AsTime().After(ahead),
		"a stamped date advances past the latest posting of the accounts it touches, not onto it")

	// With no posting ahead of the clock, the stamp is simply now.
	fresh := h.mustRecord(transfer("fresh", "untouched accounts", savings, rent, usd(1, 0)))
	assert.WithinDuration(t, time.Now(), fresh.Date.AsTime(), time.Minute)
}

// Landing past `last_date` rather than onto it is what keeps a run of stamped
// transactions apart: onto it they would all carry the parked date, and the
// Register could no longer order them by date.
func TestStampedDatesBehindAParkedPostingAreDistinct(t *testing.T) {
	h := newHarness(t)

	ahead := time.Now().Add(4 * time.Minute).UTC().Truncate(time.Millisecond)
	drifted := transfer("ahead", "a drifting clock", opening, cash, usd(10, 0))
	drifted.Date = timestamppb.New(ahead)
	h.mustRecord(drifted)

	previous := ahead
	for i := range 5 {
		stamped := h.mustRecord(transfer(fmt.Sprintf("stamped-%d", i), "no date at all", opening, cash, usd(1, 0)))
		date := stamped.Date.AsTime()
		assert.True(t, date.After(previous), "stamped date %s must advance past %s", date, previous)
		previous = date
	}
}

func TestCurrencyCodeIsCaseInsensitive(t *testing.T) {
	h := newHarness(t)

	h.mustRecord(transfer("key-lower", "lower case", opening, cash, amount("usd", 100, 0)))
	transaction := h.mustRecord(transfer("key-upper", "upper case", opening, cash, amount("USD", 50, 0)))

	// The second transaction carried on from the first, so both landed in the
	// same bucket, stored under the upper case code.
	assertMoney(t, usd(150, 0), transaction.Postings[0].Balance)

	balances := h.balances(&pb.ListAccountBalancesRequest{Account: exactly(cash)})
	require.Len(t, balances, 1, "usd and USD are one balance, not two")
	assertMoney(t, usd(150, 0), balances[0].Balance)
}

func TestALowerCaseCurrencyFilterFindsTheBalance(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(transfer("key", "a deposit", opening, cash, usd(100, 0)))

	// The write path stores the code upper case, so a filter has to be
	// normalised the same way or it silently matches nothing.
	balances := h.balances(&pb.ListAccountBalancesRequest{
		Account:      exactly(cash),
		CurrencyCode: "usd",
	})
	require.Len(t, balances, 1, "a lower case currency filter finds the balance")
	assertMoney(t, usd(100, 0), balances[0].Balance)

	register := h.register(&pb.ListPostingsRequest{
		Filter: &pb.PostingFilter{Account: exactly(cash), CurrencyCode: "usd"},
	})
	require.Len(t, register.Postings, 1, "the same holds for the Register")
}

func TestVerifyNonNegativeBalancesRefusesATransactionThatWouldGoNegative(t *testing.T) {
	h := newHarness(t)
	h.mustRecord(transfer("opening", "opening deposit", opening, cash, usd(100, 0)))

	overdraw := transfer("overdraw", "more than there is", cash, rent, usd(150, 0))
	overdraw.VerifyNonNegativeBalances = []*pb.Account{cash}

	_, err := h.record(overdraw)
	requireCode(t, err, codes.FailedPrecondition)
	assert.Contains(t, err.Error(), "ASSETS:alice:Checking", "the refusal names the account")

	// The refusal leaves no trace. The balance snapshot is untouched, and the
	// idempotency key is free again, so no transaction row survived — and a
	// posting cannot outlive the transaction it belongs to.
	balances := h.balances(&pb.ListAccountBalancesRequest{})
	require.Len(t, balances, 2, "the refused transaction touched no new account")
	assertMoney(t, usd(100, 0), balanceOf(t, balances, cash))
	h.mustRecord(transfer("overdraw", "small enough", cash, rent, usd(10, 0)))
}

func TestVerifyNonNegativeBalancesLeavesUnnamedAccountsAlone(t *testing.T) {
	h := newHarness(t)

	request := transfer("spend", "spend money that is not there", cash, rent, usd(50, 0))
	request.VerifyNonNegativeBalances = []*pb.Account{rent}

	h.mustRecord(request)
	assertMoney(t, usd(-50, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
}

// An account to verify is matched exactly, so every near miss on the composite
// names an account this transaction does not touch. That is refused rather than
// passed over: a typo would otherwise turn the guard off silently and record
// the transaction anyway, which is the whole thing the caller asked to prevent.
// Nothing here is a pattern — "*", "%" and "Check%" are literal names.
func TestVerifyNonNegativeBalancesMatchesAccountsExactly(t *testing.T) {
	h := newHarness(t)

	nearMisses := map[string]*pb.Account{
		"the wrong type":      account(pb.AccountType_ACCOUNT_TYPE_LIABILITIES, "alice", "Checking"),
		"the wrong user":      account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "bob", "Checking"),
		"a truncated name":    account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Check"),
		"a typo in the name":  account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Checkin"),
		"an empty name":       account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", ""),
		"an empty user":       account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "", "Checking"),
		"only the type set":   account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "", ""),
		"a star as the name":  account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "*", "*"),
		"a per-cent sign":     account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "%"),
		"a trailing per-cent": account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Check%"),
	}

	for name, toVerify := range nearMisses {
		t.Run(name, func(t *testing.T) {
			request := transfer("spend", "spend money that is not there", cash, rent, usd(50, 0))
			request.VerifyNonNegativeBalances = []*pb.Account{toVerify}

			_, err := h.record(request)
			requireCode(t, err, codes.InvalidArgument)
			assert.Empty(t, h.balances(&pb.ListAccountBalancesRequest{}), "the refused transaction changed nothing")
		})
	}

	// The account the transaction really does drive negative is spelled exactly,
	// so it guards, and the same transaction is refused on its balance instead.
	request := transfer("spend", "spend money that is not there", cash, rent, usd(50, 0))
	request.VerifyNonNegativeBalances = []*pb.Account{cash}
	_, err := h.record(request)
	requireCode(t, err, codes.FailedPrecondition)
	assert.Contains(t, err.Error(), "ASSETS:alice:Checking")
}

func TestATransactionThatDipsNegativeAndRecoversIsAccepted(t *testing.T) {
	h := newHarness(t)

	// Checking is empty, falls to -100 on the first posting and recovers to 50
	// by the last. Only the balance left once every posting has been applied is
	// verified, so this is accepted.
	transaction := h.mustRecord(&pb.RecordTransactionRequest{
		IdempotencyKey: "dip",
		Note:           "dips and recovers within itself",
		Postings: []*pb.RecordTransactionRequest_PostingInput{
			posting(cash, usd(-100, 0)),
			posting(rent, usd(100, 0)),
			posting(opening, usd(-150, 0)),
			posting(cash, usd(150, 0)),
		},
		VerifyNonNegativeBalances: []*pb.Account{cash},
	})

	assertMoney(t, usd(-100, 0), transaction.Postings[0].Balance)
	assertMoney(t, usd(50, 0), transaction.Postings[3].Balance)
	assertMoney(t, usd(50, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
}

func TestARetryWithIdenticalContentReplaysTheOriginal(t *testing.T) {
	h := newHarness(t)

	request := tagged(onDay(1, "retry", "a deposit", opening, cash, usd(100, 0)),
		map[string]string{"order": "42"})
	request.VerifyNonNegativeBalances = []*pb.Account{cash}

	first, err := h.record(request)
	require.NoError(t, err)
	assert.False(t, first.Replayed, "a first write is not a replay")

	// The same key with the same content, sent again as a timed-out caller
	// would: the original transaction comes back and no money moves twice.
	second, err := h.record(request)
	require.NoError(t, err)
	assert.True(t, second.Replayed, "a retry with identical content is a replay")
	assert.Equal(t, first.Transaction.Id, second.Transaction.Id)
	assert.Equal(t, first.Transaction.Note, second.Transaction.Note)
	assert.Equal(t, first.Transaction.Metadata, second.Transaction.Metadata)
	require.Len(t, second.Transaction.Postings, 2)
	assertMoney(t, usd(100, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
	assert.EqualValues(t, 1, h.transactions(&pb.ListTransactionsRequest{}).TotalCount,
		"the retry recorded nothing new")
}

func TestTheSameIdempotencyKeyWithDifferentContentIsRefused(t *testing.T) {
	h := newHarness(t)
	original := h.mustRecord(transfer("collide", "a deposit", opening, cash, usd(100, 0)))

	// Every one of these differs from the original in content the fingerprint
	// covers, so none of them may be handed the original transaction.
	different := map[string]*pb.RecordTransactionRequest{
		"a different amount":  transfer("collide", "a deposit", opening, cash, usd(200, 0)),
		"a different note":    transfer("collide", "a withdrawal", opening, cash, usd(100, 0)),
		"a different account": transfer("collide", "a deposit", opening, savings, usd(100, 0)),
		"added metadata": tagged(transfer("collide", "a deposit", opening, cash, usd(100, 0)),
			map[string]string{"order": "42"}),
	}

	for name, request := range different {
		t.Run(name, func(t *testing.T) {
			_, err := h.record(request)
			requireCode(t, err, codes.AlreadyExists)
		})
	}

	assertMoney(t, usd(100, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
	assert.EqualValues(t, 1, h.transactions(&pb.ListTransactionsRequest{}).TotalCount)
	assert.Equal(t, original.Id, h.transactions(&pb.ListTransactionsRequest{}).Transactions[0].Id)
}

// A retry racing the request it retries is the case the fast path cannot catch:
// neither caller has seen the other's row yet. The unique constraint on the key
// is what decides it, and the loser replays the winner.
func TestTwoConcurrentIdenticalRequestsApplyExactlyOnce(t *testing.T) {
	h := newHarness(t)

	const callers = 8
	type outcome struct {
		response *pb.RecordTransactionResponse
		err      error
	}
	outcomes := make(chan outcome, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			response, err := h.record(transfer("racing", "a deposit", opening, cash, usd(100, 0)))
			outcomes <- outcome{response, err}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)

	written := 0
	id := ""
	for got := range outcomes {
		require.NoError(t, got.err, "every caller of an identical request is answered")
		if !got.response.Replayed {
			written++
		}
		if id == "" {
			id = got.response.Transaction.Id
		}
		assert.Equal(t, id, got.response.Transaction.Id, "every caller gets the same transaction back")
		require.Len(t, got.response.Transaction.Postings, 2)
	}

	assert.Equal(t, 1, written, "exactly one caller wrote the transaction")
	assertMoney(t, usd(100, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
	assert.EqualValues(t, 1, h.transactions(&pb.ListTransactionsRequest{}).TotalCount,
		"the money was applied exactly once")
}

// A caller who omits the date is stamped at whatever instant the ledger writes,
// which differs between a request and its retry. The stamp is the ledger's, not
// content, so it stays out of the fingerprint and the two still match.
func TestTwoRetriesThatOmitTheDateStillMatch(t *testing.T) {
	h := newHarness(t)

	request := transfer("no-date", "no date at all", opening, cash, usd(100, 0))
	first, err := h.record(request)
	require.NoError(t, err)
	require.False(t, first.Replayed)

	// Move the clock on as far as the test can: another write to the same
	// account stamps a later date, so a second stamp could not equal the first.
	h.mustRecord(transfer("in-between", "elsewhere", opening, cash, usd(1, 0)))

	second, err := h.record(request)
	require.NoError(t, err)
	assert.True(t, second.Replayed, "a stamped date does not make a retry a different request")
	assert.Equal(t, first.Transaction.Id, second.Transaction.Id)
	assertMoney(t, usd(101, 0), balanceOf(t, h.balances(&pb.ListAccountBalancesRequest{}), cash))
}

func TestReorderingThePostingsIsNotAReplay(t *testing.T) {
	h := newHarness(t)

	request := transfer("order", "a deposit", opening, cash, usd(100, 0))
	h.mustRecord(request)

	reordered := proto.Clone(request).(*pb.RecordTransactionRequest)
	reordered.Postings[0], reordered.Postings[1] = reordered.Postings[1], reordered.Postings[0]

	_, err := h.record(reordered)
	requireCode(t, err, codes.AlreadyExists)
	assert.EqualValues(t, 1, h.transactions(&pb.ListTransactionsRequest{}).TotalCount)
}

func TestChangingOnlyTheVerifiedAccountsIsStillAReplay(t *testing.T) {
	h := newHarness(t)

	request := transfer("verified", "a deposit", opening, cash, usd(100, 0))
	request.VerifyNonNegativeBalances = []*pb.Account{cash}
	first := h.mustRecord(request)

	// The accounts to verify are a precondition on the write, not content of
	// it, so naming different ones is the same transaction.
	relaxed := proto.Clone(request).(*pb.RecordTransactionRequest)
	relaxed.VerifyNonNegativeBalances = nil

	replay, err := h.record(relaxed)
	require.NoError(t, err)
	assert.True(t, replay.Replayed)
	assert.Equal(t, first.Id, replay.Transaction.Id)
	assert.EqualValues(t, 1, h.transactions(&pb.ListTransactionsRequest{}).TotalCount)
}
