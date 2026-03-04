package middleware

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// JwtTokenValidator validates JWT tokens using a symmetric HMAC secret.
type JwtTokenValidator struct {
	secret []byte
}

func NewJwtTokenValidator(secret string) *JwtTokenValidator {
	return &JwtTokenValidator{secret: []byte(secret)}
}

func (v *JwtTokenValidator) ValidateToken(ctx context.Context, tokenStr string) (*TokenInfo, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		// Validate the alg is what we expect
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return v.secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		info := &TokenInfo{}

		if scopeClaim, ok := claims["scope"].(string); ok {
			info.Scopes = strings.Split(scopeClaim, " ")
		}

		if subClaim, ok := claims["sub"].(string); ok {
			info.UserID = subClaim
		}

		// Assuming roles are provided in a "roles" claim as an array of strings
		if rolesClaim, ok := claims["roles"].([]interface{}); ok {
			for _, role := range rolesClaim {
				if roleStr, ok := role.(string); ok {
					info.Roles = append(info.Roles, roleStr)
				}
			}
		}

		return info, nil
	}

	return nil, errors.New("invalid jwt claims")
}
