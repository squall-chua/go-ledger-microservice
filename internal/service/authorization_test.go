package service

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

// The ledger takes only service callers, and asks each one only what it is
// allowed to do: `ledger:write` to record, `ledger:read` to query. Neither
// scope implies the other. See docs/adr/0003-trusted-service-callers-only.md.
//
// The scope check runs in the interceptor, ahead of the method body, so a
// refusal here is the same whether or not the method behind it is implemented.

// queries names the three read RPCs so each can be attempted with a given token.
var queries = map[string]func(context.Context, *harness) error{
	"ListAccountBalances": func(ctx context.Context, h *harness) error {
		_, err := h.client.ListAccountBalances(ctx, &pb.ListAccountBalancesRequest{})
		return err
	},
	"ListTransactions": func(ctx context.Context, h *harness) error {
		_, err := h.client.ListTransactions(ctx, &pb.ListTransactionsRequest{})
		return err
	},
	"ListPostings": func(ctx context.Context, h *harness) error {
		_, err := h.client.ListPostings(ctx, &pb.ListPostingsRequest{})
		return err
	},
}

func aTransfer(key string) *pb.RecordTransactionRequest {
	return transfer(key, "scope check",
		account(pb.AccountType_ACCOUNT_TYPE_ASSETS, "alice", "Checking"),
		account(pb.AccountType_ACCOUNT_TYPE_EXPENSES, "alice", "Groceries"),
		usd(10, 0))
}

func TestReadScopeMayNotRecord(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.RecordTransaction(callerContext(t, t.Context(), "ledger:read"), aTransfer("read-only"))
	requireCode(t, err, codes.PermissionDenied)
}

func TestWriteScopeMayNotQuery(t *testing.T) {
	h := newHarness(t)
	ctx := callerContext(t, t.Context(), "ledger:write")

	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			requireCode(t, query(ctx, h), codes.PermissionDenied)
		})
	}
}

func TestBothScopesMayRecordAndQuery(t *testing.T) {
	h := newHarness(t)
	ctx := callerContext(t, t.Context(), "ledger:read", "ledger:write")

	_, err := h.client.RecordTransaction(ctx, aTransfer("both-scopes"))
	require.NoError(t, err)

	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			// The listings may still be unimplemented; what matters is that
			// authorization let the call reach the method at all.
			err := query(ctx, h)
			require.NotEqual(t, codes.PermissionDenied, status.Code(err), "message was: %v", err)
			require.NotEqual(t, codes.Unauthenticated, status.Code(err), "message was: %v", err)
		})
	}
}

func TestBadTokensAreRefused(t *testing.T) {
	h := newHarness(t)

	tests := map[string]context.Context{
		"missing":        t.Context(),
		"malformed":      bearerContext(t.Context(), "not-a-jwt"),
		"expired":        bearerContext(t.Context(), scopedToken(t, testJWTSecret, time.Now().Add(-time.Hour))),
		"wrongly signed": bearerContext(t.Context(), scopedToken(t, "not-the-secret", time.Now().Add(time.Hour))),
	}

	for name, ctx := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := h.client.RecordTransaction(ctx, aTransfer(name))
			requireCode(t, err, codes.Unauthenticated)

			requireCode(t, queries["ListAccountBalances"](ctx, h), codes.Unauthenticated)
		})
	}
}

// scopedToken mints a token carrying both scopes, signed and dated as asked.
func scopedToken(t *testing.T, secret string, expiry time.Time) string {
	t.Helper()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"scope": "ledger:read ledger:write",
		"exp":   expiry.Unix(),
	}).SignedString([]byte(secret))
	require.NoError(t, err)
	return token
}
