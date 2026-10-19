package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"ScaleOfCoherence/internal/api/dto"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestNormalizeEmail(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"  User@Example.COM ", "user@example.com"},
		{"user@example.com", "user@example.com"},
		{"", ""},
	} {
		if got := NormalizeEmail(tc.in); got != tc.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name     string
		password string
		ok       bool
	}{
		{"empty", "", false},
		{"too short", strings.Repeat("a", MinPasswordBytes-1), false},
		{"minimum", strings.Repeat("a", MinPasswordBytes), true},
		{"maximum", strings.Repeat("a", MaxPasswordBytes), true},
		{"too long", strings.Repeat("a", MaxPasswordBytes+1), false},
		{"multibyte within bytes", strings.Repeat("é", MaxPasswordBytes/2), true},
		{"multibyte over bytes", strings.Repeat("é", MaxPasswordBytes/2+1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.password)
			if tc.ok && err != nil {
				t.Fatalf("ValidatePassword(%d bytes) = %v, want nil", len(tc.password), err)
			}
			if !tc.ok && !errors.Is(err, ErrWeakPassword) {
				t.Fatalf("ValidatePassword(%d bytes) = %v, want ErrWeakPassword", len(tc.password), err)
			}
		})
	}
}

func sign(t *testing.T, method jwt.SigningMethod, secret string, claims dto.Claims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return token
}

func TestAuthenticateRejectsBadTokens(t *testing.T) {
	// The repo is nil: every case below must fail before any database access.
	svc := NewService(nil, testSecret, time.Hour)
	ctx := context.Background()

	base := func() dto.Claims {
		return dto.Claims{
			OrgID: "org",
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user",
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		}
	}

	expired := base()
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))

	wrongMethod := base()

	cases := []struct {
		name  string
		token string
	}{
		{"garbage", "not-a-token"},
		{"expired", sign(t, jwt.SigningMethodHS256, testSecret, expired)},
		{"wrong signing method", sign(t, jwt.SigningMethodHS384, testSecret, wrongMethod)},
		{"wrong secret", sign(t, jwt.SigningMethodHS256, "another-secret-another-secret!", base())},
		{"missing subject", sign(t, jwt.SigningMethodHS256, testSecret, dto.Claims{OrgID: "org"})},
		{"missing org", sign(t, jwt.SigningMethodHS256, testSecret, dto.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.Authenticate(ctx, tc.token); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Authenticate = %v, want ErrInvalidToken", err)
			}
		})
	}
}
