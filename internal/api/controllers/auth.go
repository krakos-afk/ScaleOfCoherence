package controllers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
	"ScaleOfCoherence/internal/httpx"
	"ScaleOfCoherence/internal/ratelimit"

	"github.com/jackc/pgx/v5/pgconn"
)

const maxBodyBytes = 1 << 20 // 1 MB

// SQLSTATEs that mean "the database was too busy for this request", not a bug.
const (
	sqlstateLockNotAvailable = "55P03"
	sqlstateQueryCanceled    = "57014"
)

// AuthController rate-limits login and signup attempts per client IP and per
// email. Limits are per replica; see ENGINEERING_DECISIONS.md.
type AuthController struct {
	svc *auth.Service

	loginIP     *ratelimit.Limiter
	loginEmail  *ratelimit.Limiter
	signupIP    *ratelimit.Limiter
	signupEmail *ratelimit.Limiter
}

func NewAuthController(svc *auth.Service) *AuthController {
	return &AuthController{
		svc:         svc,
		loginIP:     ratelimit.New(20, 10),
		loginEmail:  ratelimit.New(10, 5),
		signupIP:    ratelimit.New(10, 5),
		signupEmail: ratelimit.New(10, 5),
	}
}

func (c *AuthController) Signup(w http.ResponseWriter, r *http.Request) {
	var req dto.SignupRequest
	if !decode(w, r, &req) {
		return
	}
	if req.OrgName == "" || req.Email == "" || req.Username == "" {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, err)
		return
	}
	email := auth.NormalizeEmail(req.Email)
	if ok, retry := c.signupIP.Allow(httpx.ClientIP(r)); !ok {
		rateLimited(w, retry)
		return
	}
	if ok, retry := c.signupEmail.Allow(email); !ok {
		rateLimited(w, retry)
		return
	}
	token, err := c.svc.Signup(r.Context(), req.OrgName, email, req.Username, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.TokenResponse{Token: token})
}

func (c *AuthController) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	email := auth.NormalizeEmail(req.Email)
	if ok, retry := c.loginIP.Allow(httpx.ClientIP(r)); !ok {
		rateLimited(w, retry)
		return
	}
	if email != "" {
		if ok, retry := c.loginEmail.Allow(email); !ok {
			rateLimited(w, retry)
			return
		}
	}
	token, err := c.svc.Login(r.Context(), email, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.TokenResponse{Token: token})
}

func (c *AuthController) ListUsers(w http.ResponseWriter, r *http.Request) {
	p, err := listParams(r, "username", "usertype")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	users, err := c.svc.ListUsers(r.Context(), p)
	respond(w, http.StatusOK, users, err)
}

// ListUserNames returns the org's id/username directory for any member.
// GET /auth/user-names
func (c *AuthController) ListUserNames(w http.ResponseWriter, r *http.Request) {
	users, err := c.svc.ListUserNames(r.Context())
	respond(w, http.StatusOK, users, err)
}

func (c *AuthController) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateUserRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Usertype == "" {
		req.Usertype = identity.TypeUser
	}
	if req.Email == "" || req.Username == "" || !validUsertype(req.Usertype) {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, err)
		return
	}
	u, err := c.svc.CreateUser(r.Context(), auth.NormalizeEmail(req.Email), req.Username, req.Password, req.Usertype)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.UserResponse{ID: u.ID, OrgID: u.OrgID, Usertype: u.Usertype})
}

func (c *AuthController) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if err := c.svc.DeleteUser(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validUsertype reports whether t is an accepted organisation-level usertype
// that an admin can assign when creating a user. Team roles (viewer/editor/
// leader) are scoped to individual teams, not set here.
func validUsertype(t string) bool {
	switch t {
	case identity.TypeAdmin, identity.TypeUser:
		return true
	}
	return false
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorBody(w, "request body too large"))
			return false
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError maps domain errors to HTTP responses for callers outside this
// package (the auth middleware).
func WriteError(w http.ResponseWriter, err error) {
	writeError(w, err)
}

func writeError(w http.ResponseWriter, err error) {
	// Version-checked mutations return 409 with the item's current version so
	// the client can resync without guessing.
	var stale repository.StaleError
	if errors.As(err, &stale) {
		writeConflict(w, err.Error(), stale.CurrentVersion)
		return
	}
	var conflict repository.ConflictError
	if errors.As(err, &conflict) {
		writeConflict(w, err.Error(), conflict.CurrentVersion)
		return
	}

	// Server-side timeouts mean the database was busy, not that the request
	// was wrong: surface them as retryable.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == sqlstateLockNotAvailable || pgErr.Code == sqlstateQueryCanceled) {
		writeJSON(w, http.StatusServiceUnavailable, errorBody(w, "database busy, try again"))
		return
	}

	switch {
	case errors.Is(err, auth.ErrWeakPassword):
		writeJSON(w, http.StatusBadRequest, errorBody(w, err.Error()))
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, errorBody(w, "invalid credentials"))
	case errors.Is(err, auth.ErrInvalidToken):
		writeJSON(w, http.StatusUnauthorized, errorBody(w, "unauthorized"))
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, repository.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody(w, err.Error()))
	case errors.Is(err, repository.ErrStale):
		writeJSON(w, http.StatusConflict, errorBody(w, err.Error()))
	case errors.Is(err, repository.ErrDuplicate), errors.Is(err, repository.ErrInUse), errors.Is(err, repository.ErrNoNextStage):
		writeJSON(w, http.StatusConflict, errorBody(w, err.Error()))
	case errors.Is(err, repository.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody(w, err.Error()))
	default:
		slog.Error("internal error", "request_id", w.Header().Get("X-Request-ID"), "err", err)
		writeJSON(w, http.StatusInternalServerError, errorBody(w, "internal error"))
	}
}

func writeConflict(w http.ResponseWriter, msg string, currentVersion int64) {
	body := errorBody(w, msg)
	if currentVersion > 0 {
		body["version"] = currentVersion
	}
	writeJSON(w, http.StatusConflict, body)
}

// errorBody keeps request IDs on every JSON error so logs and clients can be
// correlated; the ID is attached to the response header by middleware.
func errorBody(w http.ResponseWriter, msg string) map[string]any {
	body := map[string]any{"error": msg}
	if id := w.Header().Get("X-Request-ID"); id != "" {
		body["request_id"] = id
	}
	return body
}

func rateLimited(w http.ResponseWriter, retry time.Duration) {
	seconds := int(math.Ceil(retry.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, errorBody(w, "too many requests"))
}
