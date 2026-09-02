package middleware

import (
	"context"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

type contextKey string

const TokenInfoKey contextKey = "token_info"

// ContextWithTokenInfo stores the TokenInfo in the context.
func ContextWithTokenInfo(ctx context.Context, info *TokenInfo) context.Context {
	return context.WithValue(ctx, TokenInfoKey, info)
}

// TokenInfoFromContext retrieves the TokenInfo from the context.
func TokenInfoFromContext(ctx context.Context) (*TokenInfo, bool) {
	info, ok := ctx.Value(TokenInfoKey).(*TokenInfo)
	return info, ok
}

func AuthInterceptor(validator TokenValidator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Extract method descriptor
		methodName := strings.TrimPrefix(info.FullMethod, "/")
		parts := strings.Split(methodName, "/")
		if len(parts) != 2 {
			return handler(ctx, req)
		}

		fullName := protoreflect.FullName(parts[0] + "." + parts[1])
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(fullName)
		if err != nil {
			return handler(ctx, req)
		}

		methodDesc, ok := desc.(protoreflect.MethodDescriptor)
		if !ok {
			return handler(ctx, req)
		}

		ext := proto.GetExtension(methodDesc.Options(), pb.E_Rule)
		rule, ok := ext.(*pb.AuthRule)

		if !ok || rule == nil || len(rule.RequiredScopes) == 0 {
			return handler(ctx, req)
		}

		tokenStr, err := grpcauth.AuthFromMD(ctx, "bearer")
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "invalid auth token: %v", err)
		}

		tokenInfo, err := validator.ValidateToken(ctx, tokenStr)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "token validation failed")
		}

		// The token must carry one of the scopes the method declares. Scopes do
		// not imply one another: a caller needing both is issued both.
		allowed := false
		for _, required := range rule.RequiredScopes {
			if slices.Contains(tokenInfo.Scopes, required) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, status.Errorf(codes.PermissionDenied, "missing required scope %v", rule.RequiredScopes)
		}

		ctx = ContextWithTokenInfo(ctx, tokenInfo)
		return handler(ctx, req)
	}
}
