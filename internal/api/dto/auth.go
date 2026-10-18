package dto

import "github.com/golang-jwt/jwt/v5"

type SignupRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	OrgName  string `json:"org_name"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type CreateUserRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
	Usertype string `json:"usertype"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

type UserResponse struct {
	ID       string `json:"id"`
	OrgID    string `json:"org_id"`
	Usertype string `json:"usertype"`
}

type OrgUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Usertype string `json:"usertype"`
}

// UserName is the minimal org-scoped directory entry any member may read to
// render usernames instead of raw user ids.
type UserName struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type AuthUser struct {
	ID           string
	Username     string
	PasswordHash string
	OrgID        string
	Usertype     string
}

type Claims struct {
	OrgID    string `json:"org"`
	Usertype string `json:"typ"`
	Username string `json:"unm"`
	jwt.RegisteredClaims
}
