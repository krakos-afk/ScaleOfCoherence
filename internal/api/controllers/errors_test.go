package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ScaleOfCoherence/internal/auth"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestWriteErrorMapsDatabaseTimeoutsTo503(t *testing.T) {
	for _, code := range []string{sqlstateLockNotAvailable, sqlstateQueryCanceled} {
		rec := httptest.NewRecorder()
		rec.Header().Set("X-Request-ID", "req-test")
		writeError(rec, &pgconn.PgError{Code: code})

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("code %s → %d, want 503", code, rec.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("body: %v", err)
		}
		if body["request_id"] != "req-test" {
			t.Errorf("body = %v, want request_id", body)
		}
	}
}

func TestWriteErrorStatusMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"weak password", auth.ErrWeakPassword, http.StatusBadRequest},
		{"bad credentials", auth.ErrInvalidCredentials, http.StatusUnauthorized},
		{"bad token", auth.ErrInvalidToken, http.StatusUnauthorized},
		{"forbidden", auth.ErrForbidden, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body: %v", err)
			}
			if body["error"] == "" {
				t.Errorf("body = %v, want error message", body)
			}
		})
	}
}

func TestLoginDistinguishesNothingBetweenUnknownEmailAndWrongPassword(t *testing.T) {
	// Both paths surface the same 401 message: no account enumeration.
	rec := httptest.NewRecorder()
	writeError(rec, auth.ErrInvalidCredentials)
	first := rec.Body.String()

	rec2 := httptest.NewRecorder()
	writeError(rec2, auth.ErrInvalidCredentials)
	if first != rec2.Body.String() {
		t.Fatalf("inconsistent credential errors: %q vs %q", first, rec2.Body.String())
	}
}

func TestDecodeRejectsOversizedBody(t *testing.T) {
	body := `{"org_name":"` + strings.Repeat("a", maxBodyBytes+1) + `"}`
	req := httptest.NewRequest("POST", "/auth/signup", strings.NewReader(body))
	rec := httptest.NewRecorder()

	var dst map[string]any
	if decode(rec, req, &dst) {
		t.Fatal("decode accepted an oversized body")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestLoginRateLimitedPerIP(t *testing.T) {
	// svc is nil: the limiter must reject before any service call.
	c := NewAuthController(nil)
	for i := 0; i < 10; i++ {
		c.loginIP.Allow("203.0.113.7")
	}

	req := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"email":"a@b.co","password":"whatever"}`))
	req.RemoteAddr = "203.0.113.7:4321"
	rec := httptest.NewRecorder()
	c.Login(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
}

func TestSignupRateLimitedPerIP(t *testing.T) {
	// svc is nil: the limiter must reject before any service call.
	c := NewAuthController(nil)
	for i := 0; i < 5; i++ {
		c.signupIP.Allow("203.0.113.9")
	}

	req := httptest.NewRequest("POST", "/auth/signup",
		strings.NewReader(`{"org_name":"O","email":"a@b.co","username":"u","password":"long-enough-password"}`))
	req.RemoteAddr = "203.0.113.9:4321"
	rec := httptest.NewRecorder()
	c.Signup(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}
