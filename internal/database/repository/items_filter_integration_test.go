package repository

import (
	"context"
	"testing"

	"ScaleOfCoherence/internal/api/dto"
)

// Filters (search, priority, status, workflow/stage, assignee) and sorts
// (priority order, title alphabetical) combine correctly and keep the total
// consistent with the filtered set.
func TestItemListFiltersAndSearch(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, false)
	ctx := context.Background()

	mk := func(title, desc, priority, stage string) dto.Item {
		t.Helper()
		it, err := repo.Create(ctx, w.orgID, w.adminID, dto.ItemRequest{
			Title: title, Description: desc, Priority: priority, CurrentStageID: &stage,
		})
		if err != nil {
			t.Fatalf("create %q: %v", title, err)
		}
		return it
	}

	alpha := mk("Alpha bug", "port 8080 fails", "urgent", w.stageOne)
	beta := mk("beta feature", "search page", "low", w.stageOne)
	_ = mk("Gamma chore", "cleanup 100% done", "medium", w.stageTwo)

	// beta is assigned to the admin; alpha is closed.
	if _, err := repo.ClaimActivity(ctx, w.orgID, beta.ID, w.adminID, beta.Version, true); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.SetClosed(ctx, w.orgID, alpha.ID, w.adminID, true, alpha.Version, true); err != nil {
		t.Fatalf("close: %v", err)
	}

	list := func(f dto.ItemFilter) dto.Page[dto.Item] {
		t.Helper()
		p, err := repo.List(ctx, w.orgID, 50, 0, f)
		if err != nil {
			t.Fatalf("list %+v: %v", f, err)
		}
		return p
	}
	eqTitles := func(f dto.ItemFilter, want ...string) {
		t.Helper()
		got := titlesOf(list(f).Data)
		if len(got) != len(want) {
			t.Fatalf("filter %+v: got %v, want %v", f, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("filter %+v: got %v, want %v", f, got, want)
			}
		}
	}

	// Default: all items, newest first.
	if p := list(dto.ItemFilter{}); p.Total != 3 {
		t.Fatalf("expected 3 items, got %d", p.Total)
	}
	eqTitles(dto.ItemFilter{}, "Gamma chore", "beta feature", "Alpha bug")

	// Trigram search hits title (case-insensitive) and description.
	eqTitles(dto.ItemFilter{Search: "ALPHA"}, "Alpha bug")
	eqTitles(dto.ItemFilter{Search: "search page"}, "beta feature")

	// LIKE wildcards in user input must match literally: "0%" hits "100% done"
	// but not the "8080" description; "_" matches nothing.
	eqTitles(dto.ItemFilter{Search: "0%"}, "Gamma chore")
	if p := list(dto.ItemFilter{Search: "_"}); p.Total != 0 {
		t.Fatalf("expected 0 items for literal underscore, got %d", p.Total)
	}

	// Priority filter and priority order (urgent -> high -> medium -> low).
	eqTitles(dto.ItemFilter{Priority: "low"}, "beta feature")
	eqTitles(dto.ItemFilter{Sort: "priority"}, "Alpha bug", "Gamma chore", "beta feature")

	// Status filter.
	if p := list(dto.ItemFilter{Status: "open"}); p.Total != 2 {
		t.Fatalf("expected 2 open items, got %d", p.Total)
	}
	eqTitles(dto.ItemFilter{Status: "closed"}, "Alpha bug")

	// Workflow and current-stage filters.
	if p := list(dto.ItemFilter{WorkflowID: w.workflowID}); p.Total != 3 {
		t.Fatalf("expected 3 items in workflow, got %d", p.Total)
	}
	eqTitles(dto.ItemFilter{StageID: w.stageTwo}, "Gamma chore")

	// Assignee filter.
	eqTitles(dto.ItemFilter{AssignedTo: w.adminID}, "beta feature")
	if p := list(dto.ItemFilter{AssignedTo: "unassigned"}); p.Total != 2 {
		t.Fatalf("expected 2 unassigned items, got %d", p.Total)
	}

	// Alphabetical title sort (case-insensitive) both ways.
	eqTitles(dto.ItemFilter{Sort: "title", Order: "asc"}, "Alpha bug", "beta feature", "Gamma chore")
	eqTitles(dto.ItemFilter{Sort: "title", Order: "desc"}, "Gamma chore", "beta feature", "Alpha bug")

	// Filters combine and pagination stays within the filtered set.
	page1, err := repo.List(ctx, w.orgID, 1, 0, dto.ItemFilter{Status: "open", Sort: "title", Order: "asc"})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	page2, err := repo.List(ctx, w.orgID, 1, 1, dto.ItemFilter{Status: "open", Sort: "title", Order: "asc"})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if page1.Total != 2 || page2.Total != 2 {
		t.Fatalf("expected filtered total 2, got %d and %d", page1.Total, page2.Total)
	}
	if len(page1.Data) != 1 || len(page2.Data) != 1 ||
		page1.Data[0].Title != "beta feature" || page2.Data[0].Title != "Gamma chore" {
		t.Fatalf("unexpected pagination: %v / %v", titlesOf(page1.Data), titlesOf(page2.Data))
	}
}

func titlesOf(items []dto.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}
