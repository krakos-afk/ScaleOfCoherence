package identity

import (
	"context"
	"errors"
)

// Org-level usertype constants stored in organization_members.usertype and the
// JWT "typ" claim. Fine-grained team roles (viewer/editor/leader) live in
// team_members and are enforced at the repository layer where it matters
// (item updates, claim/approve/reassign, team membership management).
const (
	TypeAdmin = "admin"
	TypeUser  = "user"
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

// CanWrite reports whether the caller may create or modify resources at the
// org level. Both admin and regular user members may; finer-grained viewer /
// editor restrictions are enforced per-team at the repository layer.
func (i Identity) CanWrite() bool {
	switch i.Usertype {
	case TypeAdmin, TypeUser:
		return true
	}
	return false
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
