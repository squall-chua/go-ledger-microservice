package middleware

import (
	"context"
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

type TokenInfo struct {
	UserID string
	Scopes []string
	Roles  []string
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

		if !ok || rule == nil || (len(rule.RequiredScopes) == 0 && len(rule.RequiredRoles) == 0) {
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

		// Validate Scopes
		if len(rule.RequiredScopes) > 0 {
			hasScope := false
			for _, required := range rule.RequiredScopes {
				for _, provided := range tokenInfo.Scopes {
					if provided == required {
						hasScope = true
						break
					}
				}
				if hasScope {
					break
				}
			}
			if !hasScope {
				return nil, status.Errorf(codes.PermissionDenied, "missing required scope")
			}
		}

		// Validate Roles
		if len(rule.RequiredRoles) > 0 {
			hasRole := false
			for _, required := range rule.RequiredRoles {
				for _, provided := range tokenInfo.Roles {
					if provided == required {
						hasRole = true
						break
					}
				}
				if hasRole {
					break
				}
			}
			if !hasRole {
				return nil, status.Errorf(codes.PermissionDenied, "missing required role")
			}
		}

		ctx = ContextWithTokenInfo(ctx, tokenInfo)
		return handler(ctx, req)
	}
}
