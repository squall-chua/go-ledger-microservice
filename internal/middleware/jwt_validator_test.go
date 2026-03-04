package middleware

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestJwtTokenValidator_ValidateToken(t *testing.T) {
	secret := "test-secret"
	validator := NewJwtTokenValidator(secret)

	tests := []struct {
		name       string
		claims     jwt.MapClaims
		signSecret []byte
		method     jwt.SigningMethod
		wantErr    bool
		wantInfo   *TokenInfo
	}{
		{
			name: "Valid Token with Scopes and Roles",
			claims: jwt.MapClaims{
				"scope": "read:items write:items",
				"roles": []interface{}{"admin", "user"},
				"exp":   time.Now().Add(time.Hour).Unix(),
			},
			signSecret: []byte(secret),
			method:     jwt.SigningMethodHS256,
			wantErr:    false,
			wantInfo: &TokenInfo{
				Scopes: []string{"read:items", "write:items"},
				Roles:  []string{"admin", "user"},
			},
		},
		{
			name: "Valid Token without scopes or roles",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(time.Hour).Unix(),
			},
			signSecret: []byte(secret),
			method:     jwt.SigningMethodHS256,
			wantErr:    false,
			wantInfo: &TokenInfo{
				Scopes: nil,
				Roles:  nil,
			},
		},
		{
			name: "Expired Token",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(-1 * time.Hour).Unix(),
			},
			signSecret: []byte(secret),
			method:     jwt.SigningMethodHS256,
			wantErr:    true,
		},
		{
			name: "Invalid Signature",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(time.Hour).Unix(),
			},
			signSecret: []byte("wrong-secret"),
			method:     jwt.SigningMethodHS256,
			wantErr:    true,
		},
		{
			name: "Wrong Signing Method",
			claims: jwt.MapClaims{
				"exp": time.Now().Add(time.Hour).Unix(),
			},
			signSecret: []byte(secret),
			method:     jwt.SigningMethodRS256, // Incompatible with symmetric secret
			wantErr:    true,                   // Expect error for invalid signature method
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(tt.method, tt.claims)
			tokenStr, err := token.SignedString(tt.signSecret)
			if err != nil {
				// skip if we can't sign (e.g. RS256 without private key)
				if tt.name == "Wrong Signing Method" {
					// Hack to create an invalid alg for HMAC explicitly
					tokenStr = "eyJhbGciOiJub25lIn0.eyJleHAiOjE3MTExMTExMTF9."
				} else {
					t.Fatalf("failed to sign token: %v", err)
				}
			}

			info, err := validator.ValidateToken(context.Background(), tokenStr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !tt.wantErr {
				if !reflect.DeepEqual(info, tt.wantInfo) {
					t.Errorf("expected info: %+v, got: %+v", tt.wantInfo, info)
				}
			}
		})
	}
}
