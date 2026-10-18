package auth

import (
	"context"
	"errors"
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
)

const tokenTTL = 24 * time.Hour

type Service struct {
	repo   *repository.AuthRepo
	secret []byte
}

func NewService(repo *repository.AuthRepo, secret string) *Service {
	return &Service{repo: repo, secret: []byte(secret)}
}

func (s *Service) Signup(ctx context.Context, orgName, email, username, password string) (string, error) {
	hash, err := hashPassword(password)
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
	u, err := s.repo.GetByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
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
	hash, err := hashPassword(password)
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

func (s *Service) Authenticate(token string) (identity.Identity, error) {
	c := &dto.Claims{}
	t, err := jwt.ParseWithClaims(token, c, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !t.Valid || c.Subject == "" {
		return identity.Identity{}, ErrInvalidToken
	}
	return identity.Identity{UserID: c.Subject, OrgID: c.OrgID, Usertype: c.Usertype}, nil
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
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
}

func hashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}
