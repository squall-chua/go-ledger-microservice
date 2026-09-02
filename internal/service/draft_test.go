package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

// draftFromRequest at its own interface. Every refusal here is a property of
// the request alone — no account balance, no lock and no clock is consulted —
// so these tests call the function directly rather than through a gRPC server,
// a signed token and a database. That a refused transaction changes nothing is
// proved once in ledger_service_test.go, where a database can show it.

func TestDraftRefusesAMalformedTransaction(t *testing.T) {
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
		"a metadata pair with an empty key": tagged(
			transfer("key", "note", opening, cash, usd(5, 0)), map[string]string{"": "x"}),
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := draftFromRequest(request)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestDraftRefusesAnUnspecifiedAccountType(t *testing.T) {
	_, err := draftFromRequest(transfer("key", "note",
		account(pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED, "alice", "Checking"), opening, usd(100, 0)))

	requireCode(t, err, codes.InvalidArgument)
}

// A proto3 enum is open, so a caller built against a newer schema can put a
// number on the wire this build has no name for. Storing it would write money
// under an account every read reports as unspecified.
func TestDraftRefusesAnUnknownAccountType(t *testing.T) {
	_, err := draftFromRequest(transfer("key", "note",
		account(pb.AccountType(99), "alice", "Checking"), opening, usd(100, 0)))

	requireCode(t, err, codes.InvalidArgument)
}

// An account to verify is matched exactly, so each one has to be a complete
// account: nothing here is a pattern. An account no posting touches is refused
// rather than passed over — a typo in the name would otherwise turn the
// overdraft guard off silently and record the transaction anyway.
func TestDraftRefusesAnAccountToVerifyThatNoPostingTouches(t *testing.T) {
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

			_, err := draftFromRequest(request)
			requireCode(t, err, codes.InvalidArgument)
		})
	}

	// Spelled exactly, the same account passes here and guards the write.
	request := transfer("spend", "spend money that is not there", cash, rent, usd(50, 0))
	request.VerifyNonNegativeBalances = []*pb.Account{cash}

	draft, err := draftFromRequest(request)
	require.NoError(t, err)
	assert.Equal(t, "ASSETS", draft.VerifyNonNegative[0].Type)
	assert.Equal(t, "Checking", draft.VerifyNonNegative[0].Name)
}
