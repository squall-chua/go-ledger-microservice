package accountfmt

import (
	"fmt"
	"strings"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

// AccountTypeToString converts the protobuf AccountType enum to a string.
func AccountTypeToString(t pb.AccountType) string {
	if t == pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED {
		return "*"
	}
	// Trim the "ACCOUNT_TYPE_" prefix
	return strings.TrimPrefix(t.String(), "ACCOUNT_TYPE_")
}

// StringToAccountType converts a string (like "ASSETS") to the enum.
func StringToAccountType(s string) pb.AccountType {
	val, ok := pb.AccountType_value["ACCOUNT_TYPE_"+strings.ToUpper(s)]
	if !ok {
		return pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED
	}
	return pb.AccountType(val)
}

// BuildString converts a protobuf AccountName message into the flat ":"-delimited string format
// expected by the database engines, preserving wildcards for empty fields.
// Format: Type[:User]:Name
func BuildString(a *pb.AccountName) string {
	if a == nil {
		return "*"
	}

	t := AccountTypeToString(a.Type)
	u := a.User
	if u == "" {
		u = "*"
	}
	n := a.Name
	if n == "" {
		n = "*"
	}

	// Always emit Type:User:Name for consistency now that user is cleanly split
	// Unless both user and name are *, and type is also *, then just "*"
	if t == "*" && u == "*" && n == "*" {
		return "*"
	}

	return fmt.Sprintf("%s:%s:%s", t, u, n)
}

// ParseString converts a flat ":"-delimited database string back into a protobuf AccountName message.
func ParseString(s string) *pb.AccountName {
	if s == "" || s == "*" {
		return &pb.AccountName{
			Type: pb.AccountType_ACCOUNT_TYPE_UNSPECIFIED,
			User: "*",
			Name: "*",
		}
	}

	parts := strings.SplitN(s, ":", 3)
	if len(parts) == 1 {
		return &pb.AccountName{
			Type: StringToAccountType(parts[0]),
			User: "*",
			Name: "*",
		}
	} else if len(parts) == 2 {
		// Assume [Type]:[Name] if only 2 parts, implicitly User=*
		return &pb.AccountName{
			Type: StringToAccountType(parts[0]),
			User: "*",
			Name: parts[1],
		}
	}

	return &pb.AccountName{
		Type: StringToAccountType(parts[0]),
		User: parts[1],
		Name: parts[2],
	}
}
