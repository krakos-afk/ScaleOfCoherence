package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app-abc123.js"), []byte("js"), 0o644); err != nil {
		t.Fatal(err)
	}

	apiStub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := withSPA(apiStub, dir)

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCache  string
		wantBody   string
	}{
		{"root", "GET", "/", http.StatusOK, "no-cache", "index"},
		{"hashed asset", "GET", "/assets/app-abc123.js", http.StatusOK, "public, max-age=31536000, immutable", "js"},
		{"client route falls back", "GET", "/some/client/route", http.StatusOK, "no-cache", "index"},
		{"api path delegated", "GET", "/items", http.StatusTeapot, "", ""},
		{"health path delegated", "GET", "/healthz", http.StatusTeapot, "", ""},
		{"non-GET rejected", "POST", "/", http.StatusMethodNotAllowed, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantCache != "" {
				if got := rec.Header().Get("Cache-Control"); got != tc.wantCache {
					t.Errorf("Cache-Control = %q, want %q", got, tc.wantCache)
				}
			}
			if tc.wantBody != "" && !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Errorf("body = %q, want to contain %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestWithSPADisabledWhenDirEmpty(t *testing.T) {
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	h := withSPA(stub, "")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want passthrough to API handler", rec.Code)
	}
}
