package middleware

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type mockTokenValidator struct {
	tokenInfo *TokenInfo
	err       error
}

func (m *mockTokenValidator) ValidateToken(ctx context.Context, token string) (*TokenInfo, error) {
	return m.tokenInfo, m.err
}

func bearerContext() context.Context {
	return metadata.NewIncomingContext(context.Background(),
		metadata.Pairs("authorization", "bearer any-token"))
}

const (
	recordMethod   = "/v1.LedgerService/RecordTransaction" // declares ledger:write
	balancesMethod = "/v1.LedgerService/ListAccountBalances"
)

func TestAuthInterceptorScopes(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}

	tests := []struct {
		name   string
		method string
		scopes []string
		want   codes.Code
	}{
		{"write scope records", recordMethod, []string{"ledger:write"}, codes.OK},
		{"read scope queries", balancesMethod, []string{"ledger:read"}, codes.OK},
		{"both scopes do both", recordMethod, []string{"ledger:read", "ledger:write"}, codes.OK},
		{"read scope may not record", recordMethod, []string{"ledger:read"}, codes.PermissionDenied},
		{"write scope may not query", balancesMethod, []string{"ledger:write"}, codes.PermissionDenied},
		{"no scopes at all", recordMethod, nil, codes.PermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interceptor := AuthInterceptor(&mockTokenValidator{tokenInfo: &TokenInfo{Scopes: tt.scopes}})
			_, err := interceptor(bearerContext(), nil, &grpc.UnaryServerInfo{FullMethod: tt.method}, handler)
			if got := status.Code(err); got != tt.want {
				t.Fatalf("expected %v, got %v (%v)", tt.want, got, err)
			}
		})
	}
}

func TestAuthInterceptorUnauthenticated(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}
	info := &grpc.UnaryServerInfo{FullMethod: recordMethod}

	// No token at all.
	interceptor := AuthInterceptor(&mockTokenValidator{tokenInfo: &TokenInfo{Scopes: []string{"ledger:write"}}})
	_, err := interceptor(context.Background(), nil, info, handler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("missing token: expected Unauthenticated, got %v (%v)", got, err)
	}

	// A token the validator rejects.
	interceptor = AuthInterceptor(&mockTokenValidator{err: errors.New("bad token")})
	_, err = interceptor(bearerContext(), nil, info, handler)
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Fatalf("rejected token: expected Unauthenticated, got %v (%v)", got, err)
	}
}

// A method the registry does not know carries no rule, so it is not guarded.
func TestAuthInterceptorPassesThroughUnknownMethod(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}
	interceptor := AuthInterceptor(&mockTokenValidator{tokenInfo: &TokenInfo{}})

	_, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/UnknownService/UnknownMethod"}, handler)
	if err != nil {
		t.Fatalf("expected no error for unknown method, got %v", err)
	}
}

func TestContextWithTokenInfo(t *testing.T) {
	ctx := ContextWithTokenInfo(context.Background(), &TokenInfo{Scopes: []string{"ledger:read"}})

	retrieved, ok := TokenInfoFromContext(ctx)
	if !ok {
		t.Fatal("expected to find TokenInfo in context")
	}
	if len(retrieved.Scopes) != 1 || retrieved.Scopes[0] != "ledger:read" {
		t.Errorf("unexpected scopes: %v", retrieved.Scopes)
	}
}
