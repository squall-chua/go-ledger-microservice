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
			name:     "Unspecified",
			accType:  pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
			expected: "*",
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
		{
			name:     "Wildcard",
			strType:  "*",
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

func TestBuildString(t *testing.T) {
	tests := []struct {
		name     string
		accName  *pb.AccountName
		expected string
	}{
		{
			name:     "Nil account name",
			accName:  nil,
			expected: "*",
		},
		{
			name: "All wildcard",
			accName: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
				User: "",
				Name: "",
			},
			expected: "*",
		},
		{
			name: "Type provided, user and name empty",
			accName: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "",
				Name: "",
			},
			expected: "ASSETS:*:*",
		},
		{
			name: "Type and name provided, user empty",
			accName: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "",
				Name: "Cash",
			},
			expected: "ASSETS:*:Cash",
		},
		{
			name: "Full details provided",
			accName: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_LIABILITIES,
				User: "user-123",
				Name: "Credit",
			},
			expected: "LIABILITIES:user-123:Credit",
		},
		{
			name: "Wildcards provided explicitly",
			accName: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
				User: "*",
				Name: "*",
			},
			expected: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildString(tt.accName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseString(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		expected *pb.AccountName
	}{
		{
			name: "Empty string",
			str:  "",
			expected: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
				User: "*",
				Name: "*",
			},
		},
		{
			name: "Wildcard string",
			str:  "*",
			expected: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
				User: "*",
				Name: "*",
			},
		},
		{
			name: "Only Type",
			str:  "ASSETS",
			expected: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "*",
				Name: "*",
			},
		},
		{
			name: "Type and Name (2 parts)",
			str:  "ASSETS:Cash",
			expected: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "*",
				Name: "Cash",
			},
		},
		{
			name: "Full Type User Name (3 parts)",
			str:  "LIABILITIES:user-123:Credit",
			expected: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_LIABILITIES,
				User: "user-123",
				Name: "Credit",
			},
		},
		{
			name: "Too many colons",
			str:  "ASSETS:user-123:Credit:Extra",
			expected: &pb.AccountName{
				Type: pb.AccountType_ACCOUNT_TYPE_ASSETS,
				User: "user-123",
				Name: "Credit:Extra", // SplitN with 3 limits output length
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseString(tt.str)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.User, result.User)
			assert.Equal(t, tt.expected.Name, result.Name)
		})
	}
}
