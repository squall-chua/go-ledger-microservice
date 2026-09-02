package accountfmt

import (
	"strings"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

// AccountTypeToString converts the protobuf AccountType enum to the string kept
// in the account_type column. An unspecified type has no stored form: it is
// refused on write and means "not filtered" on read.
func AccountTypeToString(t pb.AccountType) string {
	if t == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
		return ""
	}
	// Trim the "ACCOUNT_TYPE_" prefix
	return strings.TrimPrefix(t.String(), "ACCOUNT_TYPE_")
}

// StringToAccountType converts a stored string (like "ASSETS") to the enum.
func StringToAccountType(s string) pb.AccountType {
	val, ok := pb.AccountType_value["ACCOUNT_TYPE_"+strings.ToUpper(s)]
	if !ok {
		return pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED
	}
	return pb.AccountType(val)
}
