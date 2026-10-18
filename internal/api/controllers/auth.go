package controllers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"
)

type AuthController struct {
	svc *auth.Service
}

func NewAuthController(svc *auth.Service) *AuthController {
	return &AuthController{svc: svc}
}

func (c *AuthController) Signup(w http.ResponseWriter, r *http.Request) {
	var req dto.SignupRequest
	if !decode(w, r, &req) {
		return
	}
	if req.OrgName == "" || req.Email == "" || req.Username == "" || len(req.Password) < 8 {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	token, err := c.svc.Signup(r.Context(), req.OrgName, req.Email, req.Username, req.Password)
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
	token, err := c.svc.Login(r.Context(), req.Email, req.Password)
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
	if req.Email == "" || req.Username == "" || len(req.Password) < 8 || !validUsertype(req.Usertype) {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	u, err := c.svc.CreateUser(r.Context(), req.Email, req.Username, req.Password, req.Usertype)
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
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
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

	switch {
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidToken):
		http.Error(w, err.Error(), http.StatusUnauthorized)
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, repository.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, repository.ErrStale):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, repository.ErrDuplicate), errors.Is(err, repository.ErrInUse), errors.Is(err, repository.ErrNoNextStage):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, repository.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		log.Printf("internal error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func writeConflict(w http.ResponseWriter, msg string, currentVersion int64) {
	body := map[string]any{"error": msg}
	if currentVersion > 0 {
		body["version"] = currentVersion
	}
	writeJSON(w, http.StatusConflict, body)
}
