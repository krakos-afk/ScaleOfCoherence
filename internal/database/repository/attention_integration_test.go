package repository

import (
	"context"
	"testing"

	"ScaleOfCoherence/internal/api/dto"
)

// The dashboard attention summary merges assigned work with pending approvals:
// approvals come first, counts include both, closed items are excluded, and an
// item that is both assigned and awaiting the caller's approval is listed once.
func TestAttentionAssignedAndApprovals(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, true) // stage-one requires approval, approver is the admin
	ctx := context.Background()

	mk := func(title, priority, stage string) dto.Item {
		t.Helper()
		it, err := repo.Create(ctx, w.orgID, w.adminID, dto.ItemRequest{
			Title: title, Priority: priority, CurrentStageID: &stage,
		})
		if err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		return it
	}

	// A: assigned to other, completed, now awaiting the admin's approval.
	approval := mk("Approval urgent", "urgent", w.stageOne)
	if _, err := repo.ClaimActivity(ctx, w.orgID, approval.ID, w.otherUser, approval.Version, false); err != nil {
		t.Fatalf("claim approval item: %v", err)
	}
	if _, err := repo.CompleteActivity(ctx, w.orgID, approval.ID, w.otherUser, "done", 2); err != nil {
		t.Fatalf("complete approval item: %v", err)
	}

	// B: plain assigned work for the admin, no approval needed.
	assigned := mk("My assigned work", "low", w.stageTwo)
	if _, err := repo.ClaimActivity(ctx, w.orgID, assigned.ID, w.adminID, assigned.Version, true); err != nil {
		t.Fatalf("claim assigned item: %v", err)
	}

	// C: admin both holds the open activity and is the stage approver.
	both := mk("Both roles", "medium", w.stageOne)
	if _, err := repo.ClaimActivity(ctx, w.orgID, both.ID, w.adminID, both.Version, true); err != nil {
		t.Fatalf("claim both item: %v", err)
	}
	if _, err := repo.CompleteActivity(ctx, w.orgID, both.ID, w.adminID, "done", 2); err != nil {
		t.Fatalf("complete both item: %v", err)
	}

	items := func(a dto.Attention) []string {
		out := make([]string, len(a.Items))
		for i, it := range a.Items {
			out[i] = it.Title + ":" + it.Reason
		}
		return out
	}

	admin, err := repo.Attention(ctx, w.orgID, w.adminID, 5)
	if err != nil {
		t.Fatalf("attention admin: %v", err)
	}
	if admin.ApprovalTotal != 2 || admin.AssignedTotal != 2 {
		t.Fatalf("admin totals: approval=%d assigned=%d, want 2/2", admin.ApprovalTotal, admin.AssignedTotal)
	}
	eqStrings(t, "admin items", items(admin),
		[]string{"Approval urgent:approval", "Both roles:approval", "My assigned work:assigned"})
	if admin.Items[0].StageName != "stage-one" {
		t.Fatalf("stage name: got %q, want stage-one", admin.Items[0].StageName)
	}

	other, err := repo.Attention(ctx, w.orgID, w.otherUser, 5)
	if err != nil {
		t.Fatalf("attention other: %v", err)
	}
	if other.ApprovalTotal != 0 || other.AssignedTotal != 1 {
		t.Fatalf("other totals: approval=%d assigned=%d, want 0/1", other.ApprovalTotal, other.AssignedTotal)
	}
	eqStrings(t, "other items", items(other), []string{"Approval urgent:assigned"})

	// limit caps the merged list while counts stay complete.
	capped, err := repo.Attention(ctx, w.orgID, w.adminID, 1)
	if err != nil {
		t.Fatalf("attention limit: %v", err)
	}
	eqStrings(t, "capped items", items(capped), []string{"Approval urgent:approval"})

	// Closed items drop out of attention entirely.
	if _, err := repo.SetClosed(ctx, w.orgID, assigned.ID, w.adminID, true, 2, true); err != nil {
		t.Fatalf("close assigned item: %v", err)
	}
	admin, err = repo.Attention(ctx, w.orgID, w.adminID, 5)
	if err != nil {
		t.Fatalf("attention after close: %v", err)
	}
	if admin.AssignedTotal != 1 {
		t.Fatalf("assigned total after close: got %d, want 1", admin.AssignedTotal)
	}
	eqStrings(t, "admin items after close", items(admin),
		[]string{"Approval urgent:approval", "Both roles:approval"})
}
