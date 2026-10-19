package auth

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"ScaleOfCoherence/internal/api/dto"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/database/repository"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrForbidden          = errors.New("forbidden")
	ErrWeakPassword       = errors.New("password must be 12-72 bytes")
)

const (
	MinPasswordBytes = 12
	MaxPasswordBytes = 72 // bcrypt only considers the first 72 bytes
)

type Service struct {
	repo    *repository.AuthRepo
	secret  []byte
	ttl     time.Duration
	hashSem chan struct{}
}

func NewService(repo *repository.AuthRepo, secret string, ttl time.Duration) *Service {
	return &Service{
		repo:    repo,
		secret:  []byte(secret),
		ttl:     ttl,
		hashSem: make(chan struct{}, runtime.NumCPU()),
	}
}

// NormalizeEmail trims and lowercases an address so it matches the
// lower(email) unique index and the login lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidatePassword enforces the length policy before bcrypt sees the input.
func ValidatePassword(password string) error {
	n := len([]byte(password))
	if n < MinPasswordBytes || n > MaxPasswordBytes {
		return fmt.Errorf("%w (got %d bytes)", ErrWeakPassword, n)
	}
	return nil
}

func (s *Service) Signup(ctx context.Context, orgName, email, username, password string) (string, error) {
	email = NormalizeEmail(email)
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return "", err
	}
	u, err := s.repo.CreateOrgWithAdmin(ctx, orgName, email, username, hash)
	if err != nil {
		return "", err
	}
	return s.issue(u)
}

func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email = NormalizeEmail(email)
	u, err := s.repo.GetByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if err := s.comparePassword(ctx, u.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}
	return s.issue(u)
}

// CreateUser lets an admin add a member to their own org.
func (s *Service) CreateUser(ctx context.Context, email, username, password, usertype string) (dto.AuthUser, error) {
	caller, err := s.requireAdmin(ctx)
	if err != nil {
		return dto.AuthUser{}, err
	}
	email = NormalizeEmail(email)
	if err := ValidatePassword(password); err != nil {
		return dto.AuthUser{}, err
	}
	hash, err := s.hashPassword(ctx, password)
	if err != nil {
		return dto.AuthUser{}, err
	}
	return s.repo.CreateMember(ctx, caller.OrgID, email, username, hash, usertype)
}

func (s *Service) ListUsers(ctx context.Context, p dto.ListParams) ([]dto.OrgUser, error) {
	caller, err := s.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListUsers(ctx, caller.OrgID, p)
}

// ListUserNames is the org-scoped name directory. Any authenticated member may
// call it; it never leaves the caller's org and exposes only id and username.
func (s *Service) ListUserNames(ctx context.Context) ([]dto.UserName, error) {
	caller, err := identity.GetIdentity(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListUserNames(ctx, caller.OrgID)
}

// DeleteUser lets a user delete themselves, or an admin delete anyone in their org.
func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	caller, err := identity.GetIdentity(ctx)
	if err != nil {
		return err
	}
	if caller.UserID != userID && !caller.IsAdmin() {
		return ErrForbidden
	}
	return s.repo.DeleteUser(ctx, caller.OrgID, userID)
}

// Authenticate validates the token and then re-reads org membership from the
// database, so deleted or demoted users lose (or gain) access immediately
// instead of when their token expires. Token claims are UI gating only.
func (s *Service) Authenticate(ctx context.Context, token string) (identity.Identity, error) {
	c := &dto.Claims{}
	t, err := jwt.ParseWithClaims(token, c, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid || c.Subject == "" || c.OrgID == "" {
		return identity.Identity{}, ErrInvalidToken
	}

	usertype, err := s.repo.GetMembership(ctx, c.OrgID, c.Subject)
	if errors.Is(err, repository.ErrNotFound) {
		return identity.Identity{}, ErrInvalidToken
	}
	if err != nil {
		return identity.Identity{}, err
	}
	return identity.Identity{UserID: c.Subject, OrgID: c.OrgID, Usertype: usertype}, nil
}

func (s *Service) requireAdmin(ctx context.Context) (identity.Identity, error) {
	id, err := identity.GetIdentity(ctx)
	if err != nil {
		return id, err
	}
	if !id.IsAdmin() {
		return id, ErrForbidden
	}
	return id, nil
}

func (s *Service) issue(u dto.AuthUser) (string, error) {
	c := dto.Claims{
		OrgID:    u.OrgID,
		Usertype: u.Usertype,
		Username: u.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
}

// hashPassword and comparePassword share a semaphore so a burst of logins can
// only occupy as many CPUs as the machine has, keeping other requests alive.
func (s *Service) hashPassword(ctx context.Context, p string) (string, error) {
	if err := s.acquire(ctx); err != nil {
		return "", err
	}
	defer s.release()
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

func (s *Service) comparePassword(ctx context.Context, hash, password string) error {
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.hashSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() {
	<-s.hashSem
}
