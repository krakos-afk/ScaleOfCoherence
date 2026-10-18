package repository

import (
	"context"
	"errors"
	"testing"

	"ScaleOfCoherence/internal/api/dto"
)

// Comments may be added by org admins and by editors, commentators, and
// leaders of the team responsible for the item's current stage. The recorded
// version is the item's current version and commenting never bumps it.
func TestCommentCreatePermissionsAndVersion(t *testing.T) {
	resetDB(t)
	itemRepo := NewItemRepo(testDB)
	repo := NewCommentRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, itemRepo)
	ctx := context.Background()

	commentator := seedUser(t, w.orgID, "commentator-"+randName(), "user")
	seedTeamMember(t, w.orgID, w.teamID, commentator, "commentator")
	viewer := seedUser(t, w.orgID, "viewer-"+randName(), "user")
	seedTeamMember(t, w.orgID, w.teamID, viewer, "viewer")
	outsider := seedUser(t, w.orgID, "outsider-"+randName(), "user")

	for _, tc := range []struct {
		who string
		id  string
	}{
		{"editor", w.otherUser},
		{"commentator", commentator},
		{"leader", w.adminID},
	} {
		c, err := repo.Create(ctx, w.orgID, item.ID, tc.id, "hello from "+tc.who, false)
		if err != nil {
			t.Fatalf("%s comment: %v", tc.who, err)
		}
		if c.Version != item.Version {
			t.Fatalf("%s: expected version %d, got %d", tc.who, item.Version, c.Version)
		}
		if c.Author == "" {
			t.Fatalf("%s: expected author username", tc.who)
		}
	}

	// Admins bypass the team-role check.
	if _, err := repo.Create(ctx, w.orgID, item.ID, outsider, "admin note", true); err != nil {
		t.Fatalf("admin comment: %v", err)
	}

	// A viewer in the team and a non-member are both forbidden.
	if _, err := repo.Create(ctx, w.orgID, item.ID, viewer, "nope", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer: expected forbidden, got %v", err)
	}
	if _, err := repo.Create(ctx, w.orgID, item.ID, outsider, "nope", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("outsider: expected forbidden, got %v", err)
	}

	// Comments never bump the item version or write changes.
	if v := itemVersion(t, w.orgID, item.ID); v != 1 {
		t.Fatalf("expected item version 1 after comments, got %d", v)
	}
	if versions := changesVersions(t, w.orgID, item.ID); len(versions) != 1 {
		t.Fatalf("expected only the created change, got %v", versions)
	}

	page, err := repo.List(ctx, w.orgID, item.ID, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 4 || len(page.Data) != 4 {
		t.Fatalf("expected 4 comments, got total=%d len=%d", page.Total, len(page.Data))
	}
	if page.Data[0].Body != "admin note" || page.Data[3].Body != "hello from editor" {
		t.Fatalf("comments not newest-first: %+v", page.Data)
	}

	if _, err := repo.Create(ctx, w.orgID, "00000000-0000-0000-0000-000000000000", w.adminID, "x", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item: expected not found, got %v", err)
	}
}

// Each comment records the version current at creation; a later item bump is
// not retroactively applied.
func TestCommentVersionAnchoring(t *testing.T) {
	resetDB(t)
	itemRepo := NewItemRepo(testDB)
	repo := NewCommentRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, itemRepo)
	ctx := context.Background()

	first, err := repo.Create(ctx, w.orgID, item.ID, w.otherUser, "on v1", false)
	if err != nil {
		t.Fatalf("comment 1: %v", err)
	}

	// Claiming bumps the item to v2.
	if _, err := itemRepo.ClaimActivity(ctx, w.orgID, item.ID, w.otherUser, item.Version, false); err != nil {
		t.Fatalf("claim: %v", err)
	}

	second, err := repo.Create(ctx, w.orgID, item.ID, w.otherUser, "on v2", false)
	if err != nil {
		t.Fatalf("comment 2: %v", err)
	}
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("expected versions 1 and 2, got %d and %d", first.Version, second.Version)
	}
	if v := itemVersion(t, w.orgID, item.ID); v != 2 {
		t.Fatalf("expected item version 2, got %d", v)
	}

	page, err := repo.List(ctx, w.orgID, item.ID, 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Data) != 2 || page.Data[0].ID != second.ID || page.Data[1].ID != first.ID {
		t.Fatalf("expected newest-first [%d %d], got %+v", second.ID, first.ID, page.Data)
	}
}

// Deletion is allowed for the author, org admins, and leaders of the item's
// current stage team. Other team editors cannot delete.
func TestCommentDeletePermissions(t *testing.T) {
	resetDB(t)
	itemRepo := NewItemRepo(testDB)
	repo := NewCommentRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, itemRepo)
	ctx := context.Background()

	commentator := seedUser(t, w.orgID, "commentator-"+randName(), "user")
	seedTeamMember(t, w.orgID, w.teamID, commentator, "commentator")

	mk := func(body string) dto.Comment {
		t.Helper()
		c, err := repo.Create(ctx, w.orgID, item.ID, commentator, body, false)
		if err != nil {
			t.Fatalf("create comment: %v", err)
		}
		return c
	}

	// A fellow editor is neither the author, a leader, nor an admin.
	c1 := mk("one")
	if err := repo.Delete(ctx, w.orgID, item.ID, c1.ID, w.otherUser, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor delete: expected forbidden, got %v", err)
	}

	// The team leader may delete any comment on the item.
	if err := repo.Delete(ctx, w.orgID, item.ID, c1.ID, w.adminID, false); err != nil {
		t.Fatalf("leader delete: %v", err)
	}

	// Org admins may delete any comment even without a team role.
	c2 := mk("two")
	if err := repo.Delete(ctx, w.orgID, item.ID, c2.ID, w.otherUser, true); err != nil {
		t.Fatalf("admin delete: %v", err)
	}

	// Authors may delete their own comments.
	c3 := mk("three")
	if err := repo.Delete(ctx, w.orgID, item.ID, c3.ID, commentator, false); err != nil {
		t.Fatalf("self delete: %v", err)
	}

	// A foreign comment id is not found.
	c4 := mk("four")
	if err := repo.Delete(ctx, w.orgID, item.ID, c4.ID+1000, w.adminID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing comment: expected not found, got %v", err)
	}
	if page, err := repo.List(ctx, w.orgID, item.ID, 10, 0); err != nil || page.Total != 1 {
		t.Fatalf("expected 1 remaining comment, got total=%v err=%v", page.Total, err)
	}
}
