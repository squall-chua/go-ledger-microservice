package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJwtTokenValidator_ValidateToken(t *testing.T) {
	secret := "test-secret"
	validator := NewJwtTokenValidator(secret)

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate an RSA key: %v", err)
	}

	tests := []struct {
		name        string
		claims      jwt.MapClaims
		signKey     any
		method      jwt.SigningMethod
		wantErr     bool
		wantErrText string
		wantInfo    *TokenInfo
	}{
		{
			name: "Scope claim is split on spaces",
			claims: jwt.MapClaims{
				"scope": "ledger:read ledger:write",
				"exp":   time.Now().Add(time.Hour).Unix(),
			},
			signKey: []byte(secret),
			method:  jwt.SigningMethodHS256,
			wantErr: false,
			wantInfo: &TokenInfo{
				Scopes: []string{"ledger:read", "ledger:write"},
			},
		},
		{
			name: "Roles and subject claims are ignored",
			claims: jwt.MapClaims{
				"scope": "ledger:read",
				"roles": []interface{}{"admin", "user"},
				"sub":   "someone",
				"exp":   time.Now().Add(time.Hour).Unix(),
			},
			signKey: []byte(secret),
			method:  jwt.SigningMethodHS256,
			wantErr: false,
			wantInfo: &TokenInfo{
				Scopes: []string{"ledger:read"},
			},
		},
		{
			name: "Valid Token without scopes",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(time.Hour).Unix(),
			},
			signKey: []byte(secret),
			method:  jwt.SigningMethodHS256,
			wantErr: false,
			wantInfo: &TokenInfo{
				Scopes: nil,
			},
		},
		{
			name: "Expired Token",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(-1 * time.Hour).Unix(),
			},
			signKey: []byte(secret),
			method:  jwt.SigningMethodHS256,
			wantErr: true,
		},
		{
			name: "Invalid Signature",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(time.Hour).Unix(),
			},
			signKey: []byte("wrong-secret"),
			method:  jwt.SigningMethodHS256,
			wantErr: true,
		},
		{
			// Properly signed by its own key and unexpired: refused on the
			// algorithm alone.
			name: "RS256 token is refused",
			claims: jwt.MapClaims{
				"scope": "ledger:read",
				"exp":   time.Now().Add(time.Hour).Unix(),
			},
			signKey:     rsaKey,
			method:      jwt.SigningMethodRS256,
			wantErr:     true,
			wantErrText: "unexpected signing method",
		},
		{
			// An unsigned token, otherwise valid and unexpired.
			name: "alg none token is refused",
			claims: jwt.MapClaims{
				"scope": "ledger:read",
				"exp":   time.Now().Add(time.Hour).Unix(),
			},
			signKey:     jwt.UnsafeAllowNoneSignatureType,
			method:      jwt.SigningMethodNone,
			wantErr:     true,
			wantErrText: "unexpected signing method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenStr, err := jwt.NewWithClaims(tt.method, tt.claims).SignedString(tt.signKey)
			if err != nil {
				t.Fatalf("failed to sign token: %v", err)
			}

			info, err := validator.ValidateToken(context.Background(), tokenStr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}

			// wantErrText names the check the token must be refused on, so a
			// refusal for some other reason does not stand in for it.
			if tt.wantErrText != "" && !strings.Contains(err.Error(), tt.wantErrText) {
				t.Errorf("expected an error mentioning %q, got: %v", tt.wantErrText, err)
			}

			if !tt.wantErr {
				if !reflect.DeepEqual(info, tt.wantInfo) {
					t.Errorf("expected info: %+v, got: %+v", tt.wantInfo, info)
				}
			}
		})
	}
}
