package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type claimCall struct {
	orgID, userID, key, hash string
}

type completeCall struct {
	orgID, userID, key string
	status             int
	body               string
	contentType        string
}

type deleteCall struct {
	orgID, userID, key string
}

type fakeStore struct {
	claimResult *repository.StoredResponse
	claimErr    error

	claims    []claimCall
	completes []completeCall
	deletes   []deleteCall
}

func (f *fakeStore) Claim(_ context.Context, orgID, userID, key, hash string) (*repository.StoredResponse, error) {
	f.claims = append(f.claims, claimCall{orgID, userID, key, hash})
	return f.claimResult, f.claimErr
}

func (f *fakeStore) Complete(_ context.Context, orgID, userID, key string, status int, body []byte, contentType string) error {
	f.completes = append(f.completes, completeCall{orgID, userID, key, status, string(body), contentType})
	return nil
}

func (f *fakeStore) Delete(_ context.Context, orgID, userID, key string) error {
	f.deletes = append(f.deletes, deleteCall{orgID, userID, key})
	return nil
}

// idempotentHandler builds the middleware around next with an authenticated
// identity, mirroring the router's position inside authenticate().
func idempotentHandler(store IdempotencyStore, next http.Handler) http.Handler {
	return withIdempotency(store, next)
}

func authedRequest(method, target, key, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	ctx := identity.WithIdentity(r.Context(), identity.Identity{OrgID: "org-1", UserID: "user-1", Usertype: identity.TypeAdmin})
	return r.WithContext(ctx)
}

func TestIdempotencyPassesThrough(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		key    string
	}{
		{"no key", "POST", "/items", ""},
		{"safe method", "GET", "/items", "key-123"},
		{"non-item path", "POST", "/teams", "key-123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			called := false
			h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authedRequest(tc.method, tc.path, tc.key, ""))

			if !called {
				t.Fatal("handler was not called")
			}
			if len(store.claims)+len(store.completes)+len(store.deletes) != 0 {
				t.Fatalf("store was touched: %+v", store)
			}
		})
	}
}

func TestIdempotencyRequiresIdentity(t *testing.T) {
	store := &fakeStore{}
	called := false
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	// No identity in context: public paths pass straight through.
	req := httptest.NewRequest("POST", "/items", strings.NewReader(`{}`))
	req.Header.Set("Idempotency-Key", "key-123")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !called || len(store.claims) != 0 {
		t.Fatalf("called=%v claims=%v", called, store.claims)
	}
}

func TestIdempotencyRejectsInvalidKey(t *testing.T) {
	store := &fakeStore{}
	called := false
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest("POST", "/items", "bad key!", "{}"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if called || len(store.claims) != 0 {
		t.Fatal("handler or store touched for invalid key")
	}
}

func TestIdempotencyFreshClaimStoresResponse(t *testing.T) {
	store := &fakeStore{}
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"item-1"}`))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest("POST", "/items", "key-fresh", `{"title":"x"}`))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.claims) != 1 || store.claims[0].orgID != "org-1" || store.claims[0].userID != "user-1" || store.claims[0].key != "key-fresh" {
		t.Fatalf("claims = %+v", store.claims)
	}
	if store.claims[0].hash == "" {
		t.Fatal("empty request hash")
	}
	if len(store.completes) != 1 {
		t.Fatalf("completes = %+v", store.completes)
	}
	got := store.completes[0]
	if got.status != http.StatusCreated || got.body != `{"id":"item-1"}` || got.contentType != "application/json" {
		t.Fatalf("completes = %+v", got)
	}
	if len(store.deletes) != 0 {
		t.Fatalf("deletes = %+v", store.deletes)
	}
}

func TestIdempotencyHashDistinguishesRequests(t *testing.T) {
	store := &fakeStore{}
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-a", `{"title":"one"}`))
	h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-b", `{"title":"one"}`))
	h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-c", `{"title":"two"}`))

	if len(store.claims) != 3 {
		t.Fatalf("claims = %+v", store.claims)
	}
	if store.claims[0].hash == "" {
		t.Fatal("empty hash")
	}
	if store.claims[0].hash != store.claims[1].hash {
		t.Error("same method/path/body must hash the same")
	}
	if store.claims[0].hash == store.claims[2].hash {
		t.Error("different body must hash differently")
	}
}

func TestIdempotencyReplaysStoredResponse(t *testing.T) {
	store := &fakeStore{claimResult: &repository.StoredResponse{
		Status:      http.StatusCreated,
		Body:        []byte(`{"id":"item-1"}`),
		ContentType: "application/json",
	}}
	called := false
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest("POST", "/items", "key-replay", `{"title":"x"}`))

	if called {
		t.Fatal("handler ran for a replayed request")
	}
	if rec.Code != http.StatusCreated || rec.Body.String() != `{"id":"item-1"}` {
		t.Fatalf("replay = %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("missing Idempotency-Replayed header")
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if len(store.completes)+len(store.deletes) != 0 {
		t.Fatal("replay must not rewrite the key")
	}
}

func TestIdempotencyConflicts(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"mismatch", repository.ErrIdempotencyMismatch, http.StatusUnprocessableEntity},
		{"in progress", repository.ErrIdempotencyInProgress, http.StatusConflict},
		{"store failure", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{claimErr: tc.err}
			called := false
			h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, authedRequest("POST", "/items", "key-conflict", `{}`))

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if called {
				t.Fatal("handler ran despite claim failure")
			}
		})
	}
}

func TestIdempotencyReleasesKeyOnServerErrors(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			store := &fakeStore{}
			h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-5xx", "{}"))

			if len(store.deletes) != 1 || len(store.completes) != 0 {
				t.Fatalf("deletes = %+v completes = %+v", store.deletes, store.completes)
			}
		})
	}
}

func TestIdempotencyReleasesKeyOnPanic(t *testing.T) {
	store := &fakeStore{}
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("handler exploded")
	}))
	func() {
		defer func() { _ = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-panic", "{}"))
	}()

	if len(store.deletes) != 1 || len(store.completes) != 0 {
		t.Fatalf("deletes = %+v completes = %+v", store.deletes, store.completes)
	}
}

func TestIdempotencyKeepsBodyForHandler(t *testing.T) {
	store := &fakeStore{}
	var seen string
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
	}))
	h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-body", `{"title":"kept"}`))

	if seen != `{"title":"kept"}` {
		t.Fatalf("handler saw %q", seen)
	}
}

func TestIdempotencyOversizedRequest(t *testing.T) {
	store := &fakeStore{}
	h := limitBody(64)(idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, authedRequest("POST", "/items", "key-big", strings.Repeat("x", 200)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.claims) != 0 {
		t.Fatal("claim made for oversized body")
	}
}

func TestIdempotencyOversizedResponseIsNotStored(t *testing.T) {
	store := &fakeStore{}
	h := idempotentHandler(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", maxReplayBodyBytes+1)))
	}))
	h.ServeHTTP(httptest.NewRecorder(), authedRequest("POST", "/items", "key-big-resp", "{}"))

	if len(store.completes) != 1 {
		t.Fatalf("completes = %+v", store.completes)
	}
	if store.completes[0].body != "" {
		t.Fatalf("stored oversize body: %d bytes", len(store.completes[0].body))
	}
}
