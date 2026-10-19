package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Keep middleware logging out of the test output.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestRequestIDEchoesValidIncoming(t *testing.T) {
	var seen string
	h := withRequestMeta(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = requestIDFrom(r.Context())
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-Request-ID", "req-abc_123.4")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != "req-abc_123.4" {
		t.Errorf("context ID = %q, want echoed value", seen)
	}
	if got := rec.Header().Get("X-Request-ID"); got != "req-abc_123.4" {
		t.Errorf("response header = %q", got)
	}
}

func TestRequestIDGeneratesWhenMissingOrInvalid(t *testing.T) {
	for _, incoming := range []string{"", "bad id with spaces", strings.Repeat("a", 65), "sneaky\nnewline"} {
		var seen string
		h := withRequestMeta(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = requestIDFrom(r.Context())
		}))
		req := httptest.NewRequest("GET", "/x", nil)
		if incoming != "" {
			req.Header.Set("X-Request-ID", incoming)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if seen == "" || seen == incoming {
			t.Errorf("incoming %q → %q, want freshly generated ID", incoming, seen)
		}
		if got := rec.Header().Get("X-Request-ID"); got != seen {
			t.Errorf("header %q != context %q", got, seen)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/items", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "DENY",
		"Cache-Control":          "no-store",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP = %q, want default-src 'self'", csp)
	}
	if hsts := rec.Header().Get("Strict-Transport-Security"); hsts != "" {
		t.Errorf("HSTS = %q on plain HTTP, want empty", hsts)
	}
}

func TestHSTSOnlyBehindHTTPSProxy(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if hsts := rec.Header().Get("Strict-Transport-Security"); !strings.Contains(hsts, "max-age=") {
		t.Fatalf("HSTS = %q, want max-age", hsts)
	}
}

func TestBodyLimit(t *testing.T) {
	h := limitBody(16)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var maxErr *http.MaxBytesError
		if !errors.As(err, &maxErr) {
			t.Errorf("read err = %v, want MaxBytesError", err)
		}
	}))

	req := httptest.NewRequest("POST", "/items", strings.NewReader(strings.Repeat("x", 64)))
	h.ServeHTTP(httptest.NewRecorder(), req)
}

func TestPanicRecovery(t *testing.T) {
	h := withRequestMeta(logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})))

	req := httptest.NewRequest("GET", "/items", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	if body["error"] != "internal error" || body["request_id"] == "" {
		t.Errorf("body = %v", body)
	}
}

func TestReadyAndHealthHandlers(t *testing.T) {
	health := httptest.NewRecorder()
	handleHealth(health, httptest.NewRequest("GET", "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", health.Code)
	}

	up := httptest.NewRecorder()
	handleReady(fakePinger{})(up, httptest.NewRequest("GET", "/readyz", nil))
	if up.Code != http.StatusOK {
		t.Errorf("/readyz up = %d, want 200", up.Code)
	}

	down := httptest.NewRecorder()
	handleReady(fakePinger{err: errors.New("db down")})(down, httptest.NewRequest("GET", "/readyz", nil))
	if down.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz down = %d, want 503", down.Code)
	}
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(ctx context.Context) error { return f.err }
