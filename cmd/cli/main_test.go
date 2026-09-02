package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

func TestParsePosting(t *testing.T) {
	t.Run("four parts, type matched case-insensitively", func(t *testing.T) {
		posting, err := parsePosting("assets:alice:Checking:10.50+usd")
		require.NoError(t, err)

		assert.Equal(t, pb.AccountType_ACCOUNT_TYPE_ASSETS, posting.Account.Type)
		assert.Equal(t, "alice", posting.Account.User)
		assert.Equal(t, "Checking", posting.Account.Name)
		assert.Equal(t, "usd", posting.Amount.CurrencyCode)
		assert.EqualValues(t, 10, posting.Amount.Units)
		assert.EqualValues(t, 500000000, posting.Amount.Nanos)
	})

	t.Run("every category is accepted", func(t *testing.T) {
		for name, want := range map[string]pb.AccountType{
			"Assets":      pb.AccountType_ACCOUNT_TYPE_ASSETS,
			"LIABILITIES": pb.AccountType_ACCOUNT_TYPE_LIABILITIES,
			"equities":    pb.AccountType_ACCOUNT_TYPE_EQUITIES,
			"Incomes":     pb.AccountType_ACCOUNT_TYPE_INCOMES,
			"eXpEnSeS":    pb.AccountType_ACCOUNT_TYPE_EXPENSES,
		} {
			posting, err := parsePosting(name + "::Cash:1+USD")
			require.NoError(t, err, name)
			assert.Equal(t, want, posting.Account.Type)
		}
	})

	t.Run("an empty user is a user, not a wildcard", func(t *testing.T) {
		posting, err := parsePosting("EXPENSES::Food:-10+USD")
		require.NoError(t, err)

		assert.Equal(t, "", posting.Account.User)
		assert.Equal(t, "Food", posting.Account.Name)
		assert.EqualValues(t, -10, posting.Amount.Units)
	})

	// Four exact parts: a name carrying a colon is not a path to split further,
	// it is an argument the CLI cannot read.
	t.Run("malformed arguments are refused, naming the argument", func(t *testing.T) {
		for _, arg := range []string{
			"",
			"ASSETS:alice:Checking",
			"ASSETS:alice:Sub:Checking:10+USD",
			"Wallet:alice:Checking:10+USD",
			"ASSETS:alice:Checking:10",
			"ASSETS:alice:Checking:10+",
			"ASSETS:alice:Checking:ten+USD",
		} {
			_, err := parsePosting(arg)
			require.Error(t, err, arg)
			assert.Contains(t, err.Error(), arg)
		}
	})
}

// Wiring a whole server into a CLI test to read two lines of output is out of
// proportion, so the decision `post` makes about a response is a pure function
// and it is that which is pinned here.
func TestPostReport(t *testing.T) {
	response := &pb.RecordTransactionResponse{
		Transaction: &pb.Transaction{
			Id:   "018f-0000",
			Date: timestamppb.New(time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)),
			Note: "a deposit",
			Postings: []*pb.Posting{{
				Account: &pb.Account{Type: pb.AccountType_ACCOUNT_TYPE_ASSETS, User: "alice", Name: "Checking"},
				Amount:  &money.Money{CurrencyCode: "USD", Units: 10},
				Balance: &money.Money{CurrencyCode: "USD", Units: 10},
			}},
		},
	}

	fresh := postReport("key-1", response)
	assert.Contains(t, fresh, "recorded 018f-0000")
	assert.Contains(t, fresh, "ASSETS:alice:Checking")
	assert.Contains(t, fresh, "balance 10 USD")
	assert.NotContains(t, fresh, "replay")

	// A replay prints the original Transaction, postings and running balances
	// alike, and says it was a replay rather than a new record.
	response.Replayed = true
	replayed := postReport("key-1", response)
	assert.Contains(t, replayed, "replayed 018f-0000")
	assert.NotContains(t, replayed, "recorded 018f-0000")
	assert.Contains(t, replayed, "ASSETS:alice:Checking")
	assert.Contains(t, replayed, "balance 10 USD")
	assert.Contains(t, replayed, `(idempotency key "key-1" was already recorded, so nothing new was recorded)`)
}

func TestParseAccount(t *testing.T) {
	account, err := parseAccount("liabilities:bob:Card")
	require.NoError(t, err)
	assert.Equal(t, pb.AccountType_ACCOUNT_TYPE_LIABILITIES, account.Type)
	assert.Equal(t, "bob", account.User)
	assert.Equal(t, "Card", account.Name)

	_, err = parseAccount("ASSETS:alice:Checking:10+USD")
	assert.ErrorContains(t, err, "ASSETS:alice:Checking:10+USD")
}

// An amount is refused at the boundary rather than quietly changed: the integer
// part has to fit money's int64 units. 1e20 used to wrap to 7766279631452241919
// and be recorded.
func TestParsePostingRefusesAnAmountItCannotRecord(t *testing.T) {
	for _, arg := range []string{
		"ASSETS:alice:Checking:99999999999999999999+USD",
		"ASSETS:alice:Checking:-99999999999999999999+USD",
	} {
		_, err := parsePosting(arg)
		require.Error(t, err, arg)
		assert.Contains(t, err.Error(), arg)
	}
}
