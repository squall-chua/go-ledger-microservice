package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestParseAccount(t *testing.T) {
	account, err := parseAccount("liabilities:bob:Card")
	require.NoError(t, err)
	assert.Equal(t, pb.AccountType_ACCOUNT_TYPE_LIABILITIES, account.Type)
	assert.Equal(t, "bob", account.User)
	assert.Equal(t, "Card", account.Name)

	_, err = parseAccount("ASSETS:alice:Checking:10+USD")
	assert.ErrorContains(t, err, "ASSETS:alice:Checking:10+USD")
}
