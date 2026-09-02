package middleware

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	grpcauth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	pb "github.com/squall-chua/go-ledger-microservice/api/v1"
)

type TokenValidator interface {
	ValidateToken(ctx context.Context, token string) (*TokenInfo, error)
}

// TokenInfo is everything the ledger takes from a caller's token. The caller is
// a service, not an end user, so the only question asked of the token is what
// the caller is allowed to do.
// See docs/adr/0003-trusted-service-callers-only.md.
type TokenInfo struct {
	Scopes []string
}

// unguardedMethods are the infrastructure RPCs served alongside the ledger that
// carry no scopes and are reachable without a token. Every other method must
// declare its own `required_scopes`; anything not named here and not annotated
// is refused.
var unguardedMethods = map[string]bool{
	grpc_health_v1.Health_Check_FullMethodName: true,
}

func AuthInterceptor(validator TokenValidator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if unguardedMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		required, err := requiredScopes(info.FullMethod)
		if err != nil {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}

		tokenStr, err := grpcauth.AuthFromMD(ctx, "bearer")
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid auth token: %v", err)
		}

		// A validator that hands back neither a token nor an error has told the
		// ledger nothing about the caller, which is refused rather than read
		// through as an empty scope set.
		tokenInfo, err := validator.ValidateToken(ctx, tokenStr)
		if err != nil || tokenInfo == nil {
			return nil, status.Errorf(codes.Unauthenticated, "token validation failed")
		}

		// The token must carry one of the scopes the method declares. Scopes do
		// not imply one another: a caller needing both is issued both.
		allowed := false
		for _, scope := range required {
			if slices.Contains(tokenInfo.Scopes, scope) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, status.Errorf(codes.PermissionDenied, "missing required scope %v", required)
		}

		return handler(ctx, req)
	}
}

// requiredScopes reads the scopes a method declares in its `auth.v1.rule`
// annotation. A method the ledger cannot read scopes from is one it cannot
// authorize, so every failure here is an error the caller is refused on rather
// than a way past the check.
func requiredScopes(fullMethod string) ([]string, error) {
	parts := strings.Split(strings.TrimPrefix(fullMethod, "/"), "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("unreadable method name %q", fullMethod)
	}

	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(parts[0] + "." + parts[1]))
	if err != nil {
		return nil, fmt.Errorf("unknown method %q", fullMethod)
	}

	methodDesc, ok := desc.(protoreflect.MethodDescriptor)
	if !ok {
		return nil, fmt.Errorf("%q is not a method", fullMethod)
	}

	rule, ok := proto.GetExtension(methodDesc.Options(), pb.E_Rule).(*pb.AuthRule)
	if !ok || rule == nil || len(rule.RequiredScopes) == 0 {
		return nil, fmt.Errorf("method %q declares no required_scopes", fullMethod)
	}

	return rule.RequiredScopes, nil
}
