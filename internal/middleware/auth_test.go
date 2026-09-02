package middleware

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/emptypb"
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

// A method the interceptor cannot read scopes from is a method it cannot
// authorize, so it is refused rather than let through. Called with no token at
// all: the refusal is the annotation's absence, not the caller's.
func TestAuthInterceptorDeniesMethodsWithoutRule(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}
	interceptor := AuthInterceptor(&mockTokenValidator{tokenInfo: &TokenInfo{}})

	methods := map[string]string{
		"declares no required_scopes": probeMethod,
		"absent from the registry":    "/v1.LedgerService/NoSuchMethod",
		"not a method name":           "not-a-method",
		"not a method descriptor":     "/v1/RecordTransactionRequest",
	}

	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			_, err := interceptor(context.Background(), nil,
				&grpc.UnaryServerInfo{FullMethod: method}, handler)
			if got := status.Code(err); got != codes.PermissionDenied {
				t.Fatalf("expected PermissionDenied, got %v (%v)", got, err)
			}
		})
	}
}

// The health check is served alongside the ledger, carries no scopes and stays
// reachable without a token.
func TestAuthInterceptorAllowsHealthCheck(t *testing.T) {
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "success", nil
	}
	interceptor := AuthInterceptor(&mockTokenValidator{tokenInfo: &TokenInfo{}})

	_, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: grpc_health_v1.Health_Check_FullMethodName}, handler)
	if err != nil {
		t.Fatalf("expected the health check to pass, got %v", err)
	}
}

// The interceptor is unary only, so a streaming RPC would reach its handler
// unauthorized however it were annotated. LedgerService declares none; this
// fails the day one is added, which is the day a stream interceptor is needed.
func TestLedgerServiceDeclaresNoStreamingRPCs(t *testing.T) {
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName("v1.LedgerService")
	if err != nil {
		t.Fatalf("LedgerService not in the registry: %v", err)
	}

	methods := desc.(protoreflect.ServiceDescriptor).Methods()
	for i := range methods.Len() {
		if method := methods.Get(i); method.IsStreamingClient() || method.IsStreamingServer() {
			t.Errorf("%s streams; AuthInterceptor guards unary calls only", method.FullName())
		}
	}
}

// probeMethod is an RPC registered without an `auth.v1.rule`, standing in for
// one someone adds and forgets to annotate. It is registered here rather than
// declared in ledger.proto so the ledger's own API carries no unguarded RPC.
const probeMethod = "/middleware.probe.v1.ProbeService/Unannotated"

func init() {
	file, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
		Name:       proto.String("middleware/probe/v1/probe.proto"),
		Package:    proto.String("middleware.probe.v1"),
		Syntax:     proto.String("proto3"),
		Dependency: []string{"google/protobuf/empty.proto"},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("ProbeService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Unannotated"),
				InputType:  proto.String(".google.protobuf.Empty"),
				OutputType: proto.String(".google.protobuf.Empty"),
			}},
		}},
	}, protoregistry.GlobalFiles)
	if err != nil {
		panic(err)
	}
	if err := protoregistry.GlobalFiles.RegisterFile(file); err != nil {
		panic(err)
	}
}
