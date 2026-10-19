package repository

import (
	"context"
	"errors"
	"testing"
)

func TestGetMembershipReReadsFromDatabase(t *testing.T) {
	resetDB(t)
	orgID := seedOrg(t, "org-"+randName())
	userID := seedUser(t, orgID, "member-"+randName(), "admin")
	repo := NewAuthRepo(testDB)

	usertype, err := repo.GetMembership(context.Background(), orgID, userID)
	if err != nil || usertype != "admin" {
		t.Fatalf("GetMembership = %q, %v; want admin, nil", usertype, err)
	}

	// A demotion is visible on the very next read, no token refresh needed.
	if _, err := testDB.Exec(
		`UPDATE organization_members SET usertype = 'user' WHERE org_id = $1 AND user_id = $2`,
		orgID, userID); err != nil {
		t.Fatalf("demote: %v", err)
	}
	usertype, err = repo.GetMembership(context.Background(), orgID, userID)
	if err != nil || usertype != "user" {
		t.Fatalf("GetMembership after demote = %q, %v; want user, nil", usertype, err)
	}

	// Removal from the org looks like "no membership" so authentication fails.
	if _, err := testDB.Exec(
		`DELETE FROM organization_members WHERE org_id = $1 AND user_id = $2`,
		orgID, userID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	if _, err := repo.GetMembership(context.Background(), orgID, userID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetMembership after delete = %v, want ErrNotFound", err)
	}
}

func TestGetByEmailIsCaseInsensitive(t *testing.T) {
	resetDB(t)
	orgID := seedOrg(t, "org-"+randName())

	var userID string
	if err := testDB.QueryRow(
		`INSERT INTO users (email, username, password_hash) VALUES ($1, $2, 'x') RETURNING id`,
		"MiXeD@Example.COM", "mixed-"+randName()).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := testDB.Exec(
		`INSERT INTO organization_members (org_id, user_id, usertype) VALUES ($1, $2, 'user')`,
		orgID, userID); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	repo := NewAuthRepo(testDB)
	u, err := repo.GetByEmail(context.Background(), "mixed@example.com")
	if err != nil || u.ID != userID {
		t.Fatalf("GetByEmail = %+v, %v; want id %s", u, err, userID)
	}
}
