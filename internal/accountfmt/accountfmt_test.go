package accountfmt

import (
	"testing"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestAccountTypeToString(t *testing.T) {
	tests := []struct {
		name     string
		accType  pb.AccountType
		expected string
	}{
		{
			name:     "Unspecified has no stored form",
			accType:  pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
			expected: "",
		},
		{
			name:     "Assets",
			accType:  pb.AccountType_ACCOUNT_TYPE_ASSETS,
			expected: "ASSETS",
		},
		{
			name:     "Liabilities",
			accType:  pb.AccountType_ACCOUNT_TYPE_LIABILITIES,
			expected: "LIABILITIES",
		},
		{
			name:     "Equities",
			accType:  pb.AccountType_ACCOUNT_TYPE_EQUITIES,
			expected: "EQUITIES",
		},
		{
			name:     "Incomes",
			accType:  pb.AccountType_ACCOUNT_TYPE_INCOMES,
			expected: "INCOMES",
		},
		{
			name:     "Expenses",
			accType:  pb.AccountType_ACCOUNT_TYPE_EXPENSES,
			expected: "EXPENSES",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AccountTypeToString(tt.accType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestStringToAccountType(t *testing.T) {
	tests := []struct {
		name     string
		strType  string
		expected pb.AccountType
	}{
		{
			name:     "Assets",
			strType:  "ASSETS",
			expected: pb.AccountType_ACCOUNT_TYPE_ASSETS,
		},
		{
			name:     "Liabilities lowercase",
			strType:  "liabilities",
			expected: pb.AccountType_ACCOUNT_TYPE_LIABILITIES,
		},
		{
			name:     "Equities mixed case",
			strType:  "EqUiTiEs",
			expected: pb.AccountType_ACCOUNT_TYPE_EQUITIES,
		},
		{
			name:     "Incomes",
			strType:  "INCOMES",
			expected: pb.AccountType_ACCOUNT_TYPE_INCOMES,
		},
		{
			name:     "Expenses",
			strType:  "EXpenses",
			expected: pb.AccountType_ACCOUNT_TYPE_EXPENSES,
		},
		{
			name:     "Unknown",
			strType:  "UNKNOWN",
			expected: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
		},
		{
			name:     "Empty",
			strType:  "",
			expected: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StringToAccountType(tt.strType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
