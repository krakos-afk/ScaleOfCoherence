package repository

import (
	"context"
	"testing"

	"ScaleOfCoherence/internal/api/dto"
)

func teamNames(p dto.Page[dto.Team]) []string {
	out := make([]string, len(p.Data))
	for i, t := range p.Data {
		out[i] = t.Name
	}
	return out
}

func memberNames(p dto.Page[dto.TeamMember]) []string {
	out := make([]string, len(p.Data))
	for i, m := range p.Data {
		out[i] = m.Username
	}
	return out
}

func userNames(u []dto.OrgUser) []string {
	out := make([]string, len(u))
	for i := range u {
		out[i] = u[i].Username
	}
	return out
}

func eqStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want %v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
}

// Teams list: substring search (case-insensitive, wildcards literal), name and
// created_at sorts, and filtered totals.
func TestTeamListSearchAndSort(t *testing.T) {
	resetDB(t)
	repo := NewTeamRepo(testDB)
	ctx := context.Background()
	orgID := seedOrg(t, "org")

	alpha := seedTeam(t, orgID, "Alpha Squad")
	beta := seedTeam(t, orgID, "beta crew")
	gamma := seedTeam(t, orgID, "Gamma 100%")
	for id, age := range map[string]string{alpha: "3 hours", beta: "2 hours", gamma: "1 hour"} {
		if _, err := testDB.Exec(
			`UPDATE teams SET created_at = now() - $2::interval WHERE id = $1`, id, age); err != nil {
			t.Fatalf("age team: %v", err)
		}
	}

	list := func(p dto.ListParams) dto.Page[dto.Team] {
		t.Helper()
		page, err := repo.List(ctx, orgID, 50, 0, p)
		if err != nil {
			t.Fatalf("list %+v: %v", p, err)
		}
		return page
	}

	eqStrings(t, "default", teamNames(list(dto.ListParams{})), []string{"Alpha Squad", "beta crew", "Gamma 100%"})
	if got := teamNames(list(dto.ListParams{Search: "CREW"})); len(got) != 1 || got[0] != "beta crew" {
		t.Fatalf("search CREW: %v", got)
	}
	if got := teamNames(list(dto.ListParams{Search: "%"})); len(got) != 1 || got[0] != "Gamma 100%" {
		t.Fatalf("literal %% search: %v", got)
	}
	eqStrings(t, "name desc", teamNames(list(dto.ListParams{Sort: "name", Order: "desc"})),
		[]string{"Gamma 100%", "beta crew", "Alpha Squad"})
	eqStrings(t, "created_at default dir", teamNames(list(dto.ListParams{Sort: "created_at"})),
		[]string{"Gamma 100%", "beta crew", "Alpha Squad"})
	eqStrings(t, "unknown sort falls back to name", teamNames(list(dto.ListParams{Sort: "bogus"})),
		[]string{"Alpha Squad", "beta crew", "Gamma 100%"})

	page1, err := repo.List(ctx, orgID, 2, 0, dto.ListParams{Search: "a"})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	page2, err := repo.List(ctx, orgID, 2, 2, dto.ListParams{Search: "a"})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if page1.Total != 3 || page2.Total != 3 {
		t.Fatalf("expected filtered total 3, got %d and %d", page1.Total, page2.Total)
	}
	eqStrings(t, "page 1", teamNames(page1), []string{"Alpha Squad", "beta crew"})
	eqStrings(t, "page 2", teamNames(page2), []string{"Gamma 100%"})
}

// Workflows list: search covers name and description; name sort both ways.
func TestWorkflowListSearchAndSort(t *testing.T) {
	resetDB(t)
	repo := NewWorkflowRepo(testDB)
	ctx := context.Background()
	orgID := seedOrg(t, "org")
	adminID := seedUser(t, orgID, "admin", "admin")

	release := seedWorkflow(t, orgID, adminID, "Release flow")
	support := seedWorkflow(t, orgID, adminID, "Support")
	seedWorkflow(t, orgID, adminID, "100% QA")
	for id, desc := range map[string]string{release: "major releases", support: "ticket triage"} {
		if _, err := testDB.Exec(`UPDATE workflows SET description = $2 WHERE id = $1`, id, desc); err != nil {
			t.Fatalf("set description: %v", err)
		}
	}

	list := func(p dto.ListParams) dto.Page[dto.Workflow] {
		t.Helper()
		page, err := repo.List(ctx, orgID, 50, 0, p)
		if err != nil {
			t.Fatalf("list %+v: %v", p, err)
		}
		return page
	}
	names := func(p dto.ListParams) []string {
		page := list(p)
		out := make([]string, len(page.Data))
		for i, w := range page.Data {
			out[i] = w.Name
		}
		return out
	}

	eqStrings(t, "default", names(dto.ListParams{}), []string{"100% QA", "Release flow", "Support"})
	eqStrings(t, "name desc", names(dto.ListParams{Sort: "name", Order: "desc"}), []string{"Support", "Release flow", "100% QA"})
	eqStrings(t, "search by name", names(dto.ListParams{Search: "release"}), []string{"Release flow"})
	eqStrings(t, "search by description", names(dto.ListParams{Search: "triage"}), []string{"Support"})
	if p := list(dto.ListParams{Search: "_"}); p.Total != 0 {
		t.Fatalf("expected 0 workflows for literal underscore, got %d", p.Total)
	}
}

// Team members: username search plus role-rank sort (leader, editor,
// commentator, viewer).
func TestTeamMembersSearchAndSort(t *testing.T) {
	resetDB(t)
	repo := NewTeamRepo(testDB)
	w := seedWorld(t, false) // admin is leader, other user is editor
	ctx := context.Background()

	aaron := seedUser(t, w.orgID, "aaron", "user")
	zoe := seedUser(t, w.orgID, "zoe", "user")
	seedTeamMember(t, w.orgID, w.teamID, aaron, "viewer")
	seedTeamMember(t, w.orgID, w.teamID, zoe, "commentator")

	list := func(p dto.ListParams) dto.Page[dto.TeamMember] {
		t.Helper()
		page, err := repo.ListMembers(ctx, w.orgID, w.teamID, 50, 0, p)
		if err != nil {
			t.Fatalf("list members %+v: %v", p, err)
		}
		return page
	}

	if got := memberNames(list(dto.ListParams{})); len(got) != 4 {
		t.Fatalf("expected 4 members, got %v", got)
	}
	if got := memberNames(list(dto.ListParams{Search: "ZOE"})); len(got) != 1 || got[0] != "zoe" {
		t.Fatalf("search ZOE: %v", got)
	}
	if got := memberNames(list(dto.ListParams{Search: "member"})); len(got) != 1 || got[0][:6] != "member" {
		t.Fatalf("search member: %v", got)
	}
	byRole := memberNames(list(dto.ListParams{Sort: "role"}))
	if len(byRole) != 4 || byRole[0][:5] != "admin" || byRole[1][:6] != "member" || byRole[2] != "zoe" || byRole[3] != "aaron" {
		t.Fatalf("role asc order: %v", byRole)
	}
	eqStrings(t, "role desc", memberNames(list(dto.ListParams{Sort: "role", Order: "desc"})),
		[]string{"aaron", "zoe", byRole[1], byRole[0]})
}

// Org user directory: username search and usertype sort with deterministic
// order inside a role.
func TestOrgUserListSearchAndSort(t *testing.T) {
	resetDB(t)
	repo := NewAuthRepo(testDB)
	ctx := context.Background()
	orgID := seedOrg(t, "org")

	seedUser(t, orgID, "zara", "user")
	seedUser(t, orgID, "admin", "admin")
	seedUser(t, orgID, "mike", "user")

	list := func(p dto.ListParams) []dto.OrgUser {
		t.Helper()
		users, err := repo.ListUsers(ctx, orgID, p)
		if err != nil {
			t.Fatalf("list users %+v: %v", p, err)
		}
		return users
	}

	eqStrings(t, "default", userNames(list(dto.ListParams{})), []string{"admin", "mike", "zara"})
	eqStrings(t, "search", userNames(list(dto.ListParams{Search: "A"})), []string{"admin", "zara"})
	eqStrings(t, "usertype desc", userNames(list(dto.ListParams{Sort: "usertype", Order: "desc"})),
		[]string{"mike", "zara", "admin"})
}
