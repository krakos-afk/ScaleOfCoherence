package identity

import (
	"context"
	"errors"
)

// Usertype constants for the organisation-level role stored in the JWT.
const (
	TypeAdmin  = "admin"
	TypeLeader = "leader"
	TypeEditor = "editor"
	TypeViewer = "viewer"
)

type Identity struct {
	UserID   string
	OrgID    string
	Usertype string
}

type ctxKey struct{}

var ErrNoIdentity = errors.New("no identity in context")

func (i Identity) IsAdmin() bool {
	return i.Usertype == TypeAdmin
}

// IsLeader reports whether the caller holds the org-level "leader" usertype.
// Fine-grained team-leader checks still happen at the repo level via team_members.
func (i Identity) IsLeader() bool {
	return i.Usertype == TypeLeader
}

// IsEditor reports whether the caller holds the org-level "editor" usertype.
func (i Identity) IsEditor() bool {
	return i.Usertype == TypeEditor
}

// CanWrite reports whether the caller may create or modify resources
// (admin, leader, or editor). Viewers may only read.
func (i Identity) CanWrite() bool {
	switch i.Usertype {
	case TypeAdmin, TypeLeader, TypeEditor:
		return true
	}
	return false
}

// CanManageTeam reports whether the caller may manage team structure
// (create/rename/delete teams, add/remove members). Only admin and leader.
func (i Identity) CanManageTeam() bool {
	return i.IsAdmin() || i.IsLeader()
}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func GetIdentity(ctx context.Context) (Identity, error) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	if !ok {
		return Identity{}, ErrNoIdentity
	}
	return id, nil
}
