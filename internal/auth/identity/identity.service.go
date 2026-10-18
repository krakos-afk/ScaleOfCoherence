package identity

import (
	"context"
	"errors"
)

type Identity struct {
	UserID   string
	OrgID    string
	Usertype string
}

type ctxKey struct{}

var ErrNoIdentity = errors.New("no identity in context")

func (i Identity) IsAdmin() bool {
	return i.Usertype == "admin"
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
