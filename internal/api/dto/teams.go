package dto

import "time"

type TeamRequest struct {
	Name string `json:"name"`
}

// AddMemberRequest body for POST /teams/{id}/members.
// Role must be one of: "viewer", "editor", "leader".
type AddMemberRequest struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type Team struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type TeamMember struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}
