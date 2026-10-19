package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

const (
	idempotencyKeyMaxLen = 200
	maxReplayBodyBytes   = 1 << 20
	// finalizeTimeout bounds the bookkeeping write so a slow database cannot
	// hold the response open after the handler already finished.
	finalizeTimeout = 5 * time.Second
)

// IdempotencyStore is implemented by *repository.IdempotencyRepo; tests use a
// fake.
type IdempotencyStore interface {
	Claim(ctx context.Context, orgID, userID, key, requestHash string) (*repository.StoredResponse, error)
	Complete(ctx context.Context, orgID, userID, key string, status int, body []byte, contentType string) error
	Delete(ctx context.Context, orgID, userID, key string) error
}

// withIdempotency makes item mutations replayable. A client that retries a
// request with the same Idempotency-Key and body receives the original
// response instead of a second write. Scope: authenticated /items routes
// (items, comments and activity transitions).
//
// Reactions to an existing key:
//   - completed, same body hash: the stored response is replayed with
//     Idempotency-Replayed: true;
//   - any state, different body hash: 422;
//   - in progress: 409.
//
// Failed server-side requests (5xx) release the key so a retry can proceed;
// 4xx responses are stored because they are deterministic.
func withIdempotency(store IdempotencyStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" || !isMutation(r.Method) || !isItemPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		id, err := identity.GetIdentity(r.Context())
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if !validIdempotencyKey(key) {
			writeAPIError(w, http.StatusBadRequest, "Idempotency-Key must be 1-200 characters of [A-Za-z0-9._:-]")
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeAPIError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		hash := requestHash(r, body)
		stored, err := store.Claim(r.Context(), id.OrgID, id.UserID, key, hash)
		switch {
		case errors.Is(err, repository.ErrIdempotencyMismatch):
			writeAPIError(w, http.StatusUnprocessableEntity, repository.ErrIdempotencyMismatch.Error())
			return
		case errors.Is(err, repository.ErrIdempotencyInProgress):
			writeAPIError(w, http.StatusConflict, repository.ErrIdempotencyInProgress.Error())
			return
		case err != nil:
			slog.Error("idempotency claim failed", "request_id", requestIDFrom(r.Context()), "err", err)
			writeAPIError(w, http.StatusInternalServerError, "internal error")
			return
		case stored != nil:
			if stored.ContentType != "" {
				w.Header().Set("Content-Type", stored.ContentType)
			}
			w.Header().Set("Idempotency-Replayed", "true")
			w.WriteHeader(stored.Status)
			_, _ = w.Write(stored.Body)
			return
		}

		cw := &captureWriter{ResponseWriter: w}
		defer func() {
			// Finalize even when the handler panicked or the client
			// disconnected, so the key never stays claimed forever.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), finalizeTimeout)
			defer cancel()
			var saveErr error
			if cw.status >= http.StatusInternalServerError || !cw.wrote {
				saveErr = store.Delete(ctx, id.OrgID, id.UserID, key)
			} else {
				body := cw.body.Bytes()
				if cw.overflow {
					body = nil
				}
				saveErr = store.Complete(ctx, id.OrgID, id.UserID, key, cw.status, body, cw.Header().Get("Content-Type"))
			}
			if saveErr != nil {
				slog.Error("idempotency finalize failed",
					"request_id", requestIDFrom(r.Context()), "key", key, "err", saveErr)
			}
		}()
		next.ServeHTTP(cw, r)
	})
}

func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func isItemPath(path string) bool {
	return path == "/items" || strings.HasPrefix(path, "/items/")
}

func validIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > idempotencyKeyMaxLen {
		return false
	}
	for _, c := range key {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == ':':
		default:
			return false
		}
	}
	return true
}

// requestHash fingerprints the request so a key cannot be silently reused for
// a different operation.
func requestHash(r *http.Request, body []byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%d\n", r.Method, r.URL.RequestURI(), len(body))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// captureWriter records the response for idempotent replay. Bodies larger than
// maxReplayBodyBytes are not kept: the response still goes out, but a retry
// simply re-executes.
type captureWriter struct {
	http.ResponseWriter
	status   int
	wrote    bool
	body     bytes.Buffer
	overflow bool
}

func (c *captureWriter) WriteHeader(code int) {
	if c.wrote {
		return
	}
	c.status = code
	c.wrote = true
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(p []byte) (int, error) {
	if !c.wrote {
		c.status = http.StatusOK
		c.wrote = true
	}
	if !c.overflow {
		if c.body.Len()+len(p) > maxReplayBodyBytes {
			c.overflow = true
			c.body.Reset()
		} else {
			c.body.Write(p)
		}
	}
	return c.ResponseWriter.Write(p)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	body := map[string]any{"error": msg}
	if id := w.Header().Get("X-Request-ID"); id != "" {
		body["request_id"] = id
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
