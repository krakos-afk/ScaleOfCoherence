package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"ScaleOfCoherence/internal/httpx"
)

type ctxKey struct{}

// requestMeta holds per-request values that outer middleware can read after
// the inner handler returns. The identity middleware writes user/org IDs into
// the shared struct so the logger sees them even though the request context
// was replaced downstream.
type requestMeta struct {
	ID     string
	UserID string
	OrgID  string
}

func metaFrom(ctx context.Context) *requestMeta {
	if m, ok := ctx.Value(ctxKey{}).(*requestMeta); ok {
		return m
	}
	return &requestMeta{}
}

func requestIDFrom(ctx context.Context) string {
	return metaFrom(ctx).ID
}

// withRequestMeta resolves the request ID (incoming X-Request-ID or a fresh
// one), echoes it on the response and exposes it to logs and error bodies.
func withRequestMeta(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = newRequestID()
		}
		meta := &requestMeta{ID: id}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, meta)))
	})
}

func sanitizeRequestID(id string) string {
	if id == "" || len(id) > 64 {
		return ""
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
		default:
			return ""
		}
	}
	return id
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-unknown"
	}
	return hex.EncodeToString(b[:])
}

// logRequests emits one structured line per request and recovers from panics
// so a single bad handler cannot take the process down.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		meta := metaFrom(r.Context())

		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"request_id", meta.ID,
					"method", r.Method,
					"path", r.URL.Path,
					"err", rec,
					"stack", string(debug.Stack()),
				)
				sw.status = http.StatusInternalServerError
				if !sw.wrote {
					sw.Header().Set("Content-Type", "application/json")
					sw.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(sw).Encode(map[string]any{
						"error": "internal error", "request_id": meta.ID,
					})
				}
			}

			attrs := []any{
				"request_id", meta.ID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_ip", httpx.ClientIP(r),
			}
			if meta.UserID != "" {
				attrs = append(attrs, "user_id", meta.UserID, "org_id", meta.OrgID)
			}
			slog.Info("request", attrs...)
		}()

		next.ServeHTTP(sw, r)
	})
}

// securityHeaders applies the browser hardening headers and marks API
// responses as uncacheable. Static assets take their cache headers from the
// SPA handler.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; connect-src 'self'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if isAPIPath(r.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies before any handler reads them.
func limitBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.status = code
	w.wrote = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}
