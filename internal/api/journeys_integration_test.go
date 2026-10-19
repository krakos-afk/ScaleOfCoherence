package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Journey tests exercise the common user workflows end to end over the HTTP
// stack (router → middleware → controllers → services → repositories). They
// reuse the scratch Postgres and the helpers from integration_test.go; each
// journey signs up its own organization so tests stay independent even though
// they share one server and database.

type world struct {
	ts *httptest.Server

	admin, leader, member, member2 apiClient
	adminID, leaderID, memberID,
	member2ID string
	adminName, leaderName, memberName,
	member2Name string

	teamID, workflowID, stage1ID, stage2ID string
}

// setupWorld builds one organization with an admin, a leader and two editors
// on one team, and a two-stage workflow assigned to that team.
func setupWorld(t *testing.T) world {
	t.Helper()
	ts := startAPI(t)
	suffix := unique()
	org := "org-journey-" + suffix
	// Unique client "IP" per journey so the per-IP auth rate limiters do not
	// see unrelated journeys as a single noisy client.
	xff := "test-journey-" + suffix

	w := world{ts: ts}
	w.adminName = "admin-" + suffix
	w.admin = apiClient{t: t, base: ts.URL, token: signupJourney(t, ts, xff, org, w.adminName), xff: xff}
	w.adminID = userID(t, w.admin, w.adminName)

	w.leaderName = "leader-" + suffix
	w.leader, w.leaderID = createUser(t, ts, w.admin, xff, w.leaderName)
	w.memberName = "member-" + suffix
	w.member, w.memberID = createUser(t, ts, w.admin, xff, w.memberName)
	w.member2Name = "member2-" + suffix
	w.member2, w.member2ID = createUser(t, ts, w.admin, xff, w.member2Name)

	// The journey tests are the only tests that care about these endpoints'
	// success paths; the authorization matrix covers their failures.
	w.teamID = id(t, w.admin.json("POST", "/teams", map[string]any{"name": "team-" + suffix}, http.StatusCreated), "id")
	for uid, role := range map[string]string{w.leaderID: "leader", w.memberID: "editor", w.member2ID: "editor"} {
		w.admin.mustDo("POST", "/teams/"+w.teamID+"/members", map[string]any{"user_id": uid, "role": role}, http.StatusCreated)
	}

	w.workflowID = id(t, w.admin.json("POST", "/workflows", map[string]any{"name": "flow-" + suffix, "description": "journey fixture"}, http.StatusCreated), "id")
	for i, name := range []string{"stage-1", "stage-2"} {
		sid := id(t, w.admin.json("POST", "/workflows/"+w.workflowID+"/stages", map[string]any{
			"name": name, "assigned_team_id": w.teamID, "position": i + 1,
		}, http.StatusCreated), "id")
		if i == 0 {
			w.stage1ID = sid
		} else {
			w.stage2ID = sid
		}
	}
	return w
}

func signupJourney(t *testing.T, ts *httptest.Server, xff, org, username string) string {
	t.Helper()
	client := apiClient{t: t, base: ts.URL, xff: xff}
	body := client.json("POST", "/auth/signup", map[string]any{
		"org_name": org, "username": username,
		"email": username + "@example.com", "password": "integration-password-1",
	}, http.StatusCreated)
	return body["token"].(string)
}

// createUser adds a plain member through the admin API and logs in as them.
func createUser(t *testing.T, ts *httptest.Server, admin apiClient, xff, username string) (apiClient, string) {
	t.Helper()
	body := admin.json("POST", "/auth/users", map[string]any{
		"username": username, "email": username + "@example.com",
		"password": "integration-password-1", "usertype": "user",
	}, http.StatusCreated)

	client := apiClient{t: t, base: ts.URL, xff: xff}
	token := client.json("POST", "/auth/login", map[string]any{
		"email": username + "@example.com", "password": "integration-password-1",
	}, http.StatusOK)["token"].(string)
	client.token = token
	return client, id(t, body, "id")
}

// userID resolves a username to its user id through the org name directory.
func userID(t *testing.T, c apiClient, username string) string {
	t.Helper()
	out := c.mustDo("GET", "/auth/user-names", nil, http.StatusOK)
	var names []map[string]any
	if err := json.Unmarshal(out, &names); err != nil {
		t.Fatalf("decode /auth/user-names: %v (%s)", err, out)
	}
	for _, n := range names {
		if n["username"] == username {
			return n["id"].(string)
		}
	}
	t.Fatalf("user %q not in name directory: %s", username, out)
	return ""
}

// createItem creates an item; with empty workflow/stage ids it is standalone.
func createItem(t *testing.T, c apiClient, title, workflowID, stageID string) string {
	t.Helper()
	body := map[string]any{"title": title}
	if workflowID != "" {
		body["workflow_id"] = workflowID
		body["current_stage_id"] = stageID
	}
	return id(t, c.json("POST", "/items", body, http.StatusCreated), "id")
}

func itemVersion(t *testing.T, c apiClient, itemID string) int64 {
	t.Helper()
	item := c.json("GET", "/items/"+itemID, nil, http.StatusOK)
	v, ok := item["version"].(float64)
	if !ok {
		t.Fatalf("item %s has no version: %v", itemID, item)
	}
	return int64(v)
}

func pageTotal(t *testing.T, page map[string]any) int {
	t.Helper()
	v, ok := page["total"].(float64)
	if !ok {
		t.Fatalf("page has no total: %v", page)
	}
	return int(v)
}

func listItemTotal(t *testing.T, c apiClient, query string) int {
	t.Helper()
	return pageTotal(t, c.json("GET", "/items?"+query, nil, http.StatusOK))
}

// ── 1. Organization setup ─────────────────────────────────────────────────────

// TestJourneyOrgSetup walks admin onboarding and the read endpoints the other
// journeys rely on: user directory, team members and workflow stages.
func TestJourneyOrgSetup(t *testing.T) {
	w := setupWorld(t)

	usersOut := w.admin.mustDo("GET", "/auth/users", nil, http.StatusOK)
	var users []map[string]any
	if err := json.Unmarshal(usersOut, &users); err != nil {
		t.Fatalf("decode /auth/users: %v (%s)", err, usersOut)
	}
	usernames := map[string]bool{}
	for _, u := range users {
		usernames[u["username"].(string)] = true
	}
	for _, name := range []string{w.adminName, w.leaderName, w.memberName, w.member2Name} {
		if !usernames[name] {
			t.Fatalf("user %q missing from /auth/users: %s", name, usersOut)
		}
	}

	// The name directory is readable by any member and must not leak emails.
	namesOut := w.member.mustDo("GET", "/auth/user-names", nil, http.StatusOK)
	if len(namesOut) == 0 {
		t.Fatal("empty name directory")
	}
	var names []map[string]any
	if err := json.Unmarshal(namesOut, &names); err != nil {
		t.Fatalf("decode /auth/user-names: %v (%s)", err, namesOut)
	}
	if len(names) != 4 {
		t.Fatalf("name directory has %d entries, want 4: %s", len(names), namesOut)
	}
	for _, n := range names {
		if _, ok := n["email"]; ok {
			t.Fatalf("name directory leaked email: %s", namesOut)
		}
	}

	members := w.admin.json("GET", "/teams/"+w.teamID+"/members", nil, http.StatusOK)
	if got := pageTotal(t, members); got != 3 {
		t.Fatalf("team has %d members, want 3: %v", got, members)
	}
	roles := map[string]string{}
	for _, raw := range members["data"].([]any) {
		e := raw.(map[string]any)
		roles[e["user_id"].(string)] = e["role"].(string)
	}
	if roles[w.leaderID] != "leader" || roles[w.memberID] != "editor" || roles[w.member2ID] != "editor" {
		t.Fatalf("unexpected team roles: %v", roles)
	}

	stagesOut := w.admin.mustDo("GET", "/workflows/"+w.workflowID+"/stages", nil, http.StatusOK)
	var stages []map[string]any
	if err := json.Unmarshal(stagesOut, &stages); err != nil {
		t.Fatalf("decode stages: %v (%s)", err, stagesOut)
	}
	if len(stages) != 2 || stages[0]["position"] != float64(1) || stages[1]["position"] != float64(2) {
		t.Fatalf("unexpected stages: %s", stagesOut)
	}
	stage := w.admin.json("GET", "/workflows/"+w.workflowID+"/stages/"+w.stage1ID, nil, http.StatusOK)
	if stage["name"] != "stage-1" {
		t.Fatalf("stage-1 read back as %v", stage)
	}
}

// ── 2. Item lifecycle without approval ────────────────────────────────────────

// TestJourneyItemLifecycle covers create → claim → complete → advance,
// including the dashboard reads the UI depends on.
func TestJourneyItemLifecycle(t *testing.T) {
	w := setupWorld(t)
	title := "lifecycle-" + unique()
	itemID := createItem(t, w.admin, title, w.workflowID, w.stage1ID)

	if v := itemVersion(t, w.admin, itemID); v != 1 {
		t.Fatalf("new item version = %d, want 1", v)
	}
	open := w.admin.json("GET", "/items/"+itemID+"/activity/open", nil, http.StatusOK)
	if open["assigned_user_id"] != nil || open["stage_id"] != w.stage1ID {
		t.Fatalf("new activity = %v", open)
	}

	if n := listItemTotal(t, w.admin, "q="+title+"&assigned_to=unassigned"); n != 1 {
		t.Fatalf("unassigned list total = %d, want 1", n)
	}
	if n := listItemTotal(t, w.member, "q="+title+"&assigned_to=me"); n != 0 {
		t.Fatalf("member 'assigned to me' before claim = %d, want 0", n)
	}

	claimed := w.member.json("POST", "/items/"+itemID+"/activity/claim", map[string]any{"version": 1}, http.StatusOK)
	if claimed["assigned_user_id"] != w.memberID {
		t.Fatalf("claim = %v", claimed)
	}
	if v := itemVersion(t, w.member, itemID); v != 2 {
		t.Fatalf("version after claim = %d, want 2", v)
	}
	if n := listItemTotal(t, w.member, "q="+title+"&assigned_to=me"); n != 1 {
		t.Fatalf("member 'assigned to me' after claim = %d, want 1", n)
	}
	if got := pageTotal(t, w.member.json("GET", "/items/my", nil, http.StatusOK)); got != 1 {
		t.Fatalf("/items/my after claim = %d, want 1", got)
	}
	if n := listItemTotal(t, w.admin, "q="+title+"&assigned_to=unassigned"); n != 0 {
		t.Fatalf("unassigned list after claim = %d, want 0", n)
	}

	attention := w.member.json("GET", "/items/attention", nil, http.StatusOK)
	if attention["assigned_total"] != float64(1) {
		t.Fatalf("attention = %v", attention)
	}
	found := false
	for _, raw := range attention["items"].([]any) {
		e := raw.(map[string]any)
		if e["id"] == itemID && e["reason"] == "assigned" {
			found = true
		}
	}
	if !found {
		t.Fatalf("attention widget missing claimed item: %v", attention)
	}

	done := w.member.json("POST", "/items/"+itemID+"/activity/complete", map[string]any{"note": "done", "version": 2}, http.StatusOK)
	if done["release_reason"] != "completed" || done["released_at"] == nil {
		t.Fatalf("complete = %v", done)
	}
	if n := listItemTotal(t, w.member, "q="+title+"&assigned_to=me"); n != 0 {
		t.Fatalf("member 'assigned to me' after complete = %d, want 0", n)
	}
	if got := pageTotal(t, w.member.json("GET", "/items/my", nil, http.StatusOK)); got != 0 {
		t.Fatalf("/items/my after complete = %d, want 0", got)
	}

	advanced := w.leader.json("POST", "/items/"+itemID+"/advance", map[string]any{"version": 3}, http.StatusOK)
	if advanced["current_stage_id"] != w.stage2ID {
		t.Fatalf("advance = %v", advanced)
	}
	if v := itemVersion(t, w.leader, itemID); v != 4 {
		t.Fatalf("version after advance = %d, want 4", v)
	}
	open = w.leader.json("GET", "/items/"+itemID+"/activity/open", nil, http.StatusOK)
	if open["stage_id"] != w.stage2ID || open["assigned_user_id"] != nil {
		t.Fatalf("stage-2 activity = %v", open)
	}

	tl := w.admin.json("GET", "/items/"+itemID+"/timeline", nil, http.StatusOK)
	entries := tl["entries"].([]any)
	want := []string{"stage_moved", "completed", "assigned", "created"} // newest first
	if len(entries) != len(want) {
		t.Fatalf("timeline has %d entries, want %d: %v", len(entries), len(want), tl)
	}
	for i, raw := range entries {
		if typ := raw.(map[string]any)["type"]; typ != want[i] {
			t.Fatalf("timeline[%d].type = %v, want %s", i, typ, want[i])
		}
	}
}

// ── 3. Approval loop ──────────────────────────────────────────────────────────

// TestJourneyApprovalLoop covers complete → pending → reject → re-claim →
// re-complete → approve on a stage that requires approval.
func TestJourneyApprovalLoop(t *testing.T) {
	w := setupWorld(t)
	suffix := unique()
	wfID := id(t, w.admin.json("POST", "/workflows", map[string]any{"name": "approval-" + suffix, "description": ""}, http.StatusCreated), "id")
	stageID := id(t, w.admin.json("POST", "/workflows/"+wfID+"/stages", map[string]any{
		"name": "approval-stage", "assigned_team_id": w.teamID, "position": 1,
		"approval_required": true, "approver_user_id": w.adminID,
	}, http.StatusCreated), "id")
	itemID := createItem(t, w.admin, "approval-"+suffix, wfID, stageID)

	w.member.json("POST", "/items/"+itemID+"/activity/claim", map[string]any{"version": 1}, http.StatusOK)
	pending := w.member.json("POST", "/items/"+itemID+"/activity/complete", map[string]any{"note": "ready", "version": 2}, http.StatusOK)
	if pending["approval_status"] != "pending" || pending["released_at"] != nil {
		t.Fatalf("completion on approval stage = %v", pending)
	}

	if status, out := w.member.do("POST", "/items/"+itemID+"/activity/approve", map[string]any{"approved": true, "note": "self", "version": 3}); status != http.StatusForbidden {
		t.Fatalf("member approve = %d, want 403: %s", status, out)
	}

	rejected := w.admin.json("POST", "/items/"+itemID+"/activity/approve", map[string]any{"approved": false, "note": "redo", "version": 3}, http.StatusOK)
	if rejected["approval_status"] != "rejected" || rejected["assigned_user_id"] != nil || rejected["completed_at"] != nil {
		t.Fatalf("rejection = %v", rejected)
	}

	reclaimed := w.member.json("POST", "/items/"+itemID+"/activity/claim", map[string]any{"version": 4}, http.StatusOK)
	if reclaimed["assigned_user_id"] != w.memberID {
		t.Fatalf("re-claim after rejection = %v", reclaimed)
	}
	w.member.json("POST", "/items/"+itemID+"/activity/complete", map[string]any{"note": "second try", "version": 5}, http.StatusOK)

	approved := w.admin.json("POST", "/items/"+itemID+"/activity/approve", map[string]any{"approved": true, "note": "ok", "version": 6}, http.StatusOK)
	if approved["approval_status"] != "approved" || approved["released_at"] == nil || approved["completed_at"] == nil {
		t.Fatalf("approval = %v", approved)
	}
	if v := itemVersion(t, w.admin, itemID); v != 7 {
		t.Fatalf("version after approval = %d, want 7", v)
	}
}

// ── 4. Reassignment ───────────────────────────────────────────────────────────

// TestJourneyReassign covers a leader handing an in-progress activity to
// another team member, and the old assignee losing the ability to complete it.
func TestJourneyReassign(t *testing.T) {
	w := setupWorld(t)
	itemID := createItem(t, w.admin, "reassign-"+unique(), w.workflowID, w.stage1ID)
	w.member.json("POST", "/items/"+itemID+"/activity/claim", map[string]any{"version": 1}, http.StatusOK)

	if status, out := w.member.do("POST", "/items/"+itemID+"/activity/reassign", map[string]any{"user_id": w.member2ID, "version": 2}); status != http.StatusForbidden {
		t.Fatalf("member reassign = %d, want 403: %s", status, out)
	}

	moved := w.leader.json("POST", "/items/"+itemID+"/activity/reassign", map[string]any{"user_id": w.member2ID, "version": 2}, http.StatusOK)
	if moved["assigned_user_id"] != w.member2ID {
		t.Fatalf("reassign = %v", moved)
	}
	open := w.leader.json("GET", "/items/"+itemID+"/activity/open", nil, http.StatusOK)
	if open["id"] != moved["id"] || open["assigned_user_id"] != w.member2ID {
		t.Fatalf("open activity after reassign = %v (new activity %v)", open, moved)
	}

	if status, out := w.member.do("POST", "/items/"+itemID+"/activity/complete", map[string]any{"note": "old assignee", "version": 3}); status != http.StatusForbidden {
		t.Fatalf("old assignee complete = %d, want 403: %s", status, out)
	}
	done := w.member2.json("POST", "/items/"+itemID+"/activity/complete", map[string]any{"note": "new assignee", "version": 3}, http.StatusOK)
	if done["release_reason"] != "completed" {
		t.Fatalf("new assignee complete = %v", done)
	}
}

// ── 5. Optimistic concurrency ─────────────────────────────────────────────────

// TestJourneyOptimisticConcurrency covers the stale-write conflict a UI must
// resync from: 409, refetch, retry with a fresh version.
func TestJourneyOptimisticConcurrency(t *testing.T) {
	w := setupWorld(t)
	itemID := createItem(t, w.admin, "concurrency-"+unique(), "", "")

	first := w.member.json("PUT", "/items/"+itemID, map[string]any{"title": "edit-one", "version": 1}, http.StatusOK)
	if first["version"] != float64(2) {
		t.Fatalf("first update = %v", first)
	}

	if status, out := w.member.do("PUT", "/items/"+itemID, map[string]any{"title": "stale-edit", "version": 1}); status != http.StatusConflict {
		t.Fatalf("stale update = %d, want 409: %s", status, out)
	}
	if v := itemVersion(t, w.member, itemID); v != 2 {
		t.Fatalf("version after stale write = %d, want 2", v)
	}

	second := w.member.json("PUT", "/items/"+itemID, map[string]any{"title": "edit-two", "version": 2}, http.StatusOK)
	if second["version"] != float64(3) {
		t.Fatalf("retried update = %v", second)
	}

	if status, out := w.member.do("PUT", "/items/"+itemID, map[string]any{"title": "versionless"}); status != http.StatusBadRequest {
		t.Fatalf("update without version = %d, want 400: %s", status, out)
	}
}

// ── 6. Close and reopen ───────────────────────────────────────────────────────

// TestJourneyCloseReopen covers the closed/open lifecycle and the status
// filter through the list endpoint.
func TestJourneyCloseReopen(t *testing.T) {
	w := setupWorld(t)
	title := "close-" + unique()
	itemID := createItem(t, w.admin, title, w.workflowID, w.stage1ID)

	closed := w.leader.json("POST", "/items/"+itemID+"/status", map[string]any{"closed": true, "version": 1}, http.StatusOK)
	if closed["closed_at"] == nil {
		t.Fatalf("close = %v", closed)
	}
	if n := listItemTotal(t, w.leader, "q="+title+"&status=closed"); n != 1 {
		t.Fatalf("closed list total = %d, want 1", n)
	}
	if n := listItemTotal(t, w.leader, "q="+title+"&status=open"); n != 0 {
		t.Fatalf("open list total after close = %d, want 0", n)
	}

	if status, out := w.member.do("POST", "/items/"+itemID+"/status", map[string]any{"closed": true, "version": 2}); status != http.StatusForbidden {
		t.Fatalf("editor close = %d, want 403: %s", status, out)
	}

	reopened := w.leader.json("POST", "/items/"+itemID+"/status", map[string]any{"closed": false, "version": 2}, http.StatusOK)
	if reopened["closed_at"] != nil {
		t.Fatalf("reopen = %v", reopened)
	}
	if n := listItemTotal(t, w.leader, "q="+title+"&status=open"); n != 1 {
		t.Fatalf("open list total after reopen = %d, want 1", n)
	}
}

// ── 7. Comments ───────────────────────────────────────────────────────────────

// TestJourneyComments covers commenting on an item and the delete rules:
// author or org admin only.
func TestJourneyComments(t *testing.T) {
	w := setupWorld(t)
	itemID := createItem(t, w.admin, "comments-"+unique(), w.workflowID, w.stage1ID)

	comment := w.member.json("POST", "/items/"+itemID+"/comments", map[string]any{"body": "first comment " + unique()}, http.StatusCreated)
	if comment["author"] != w.memberName {
		t.Fatalf("comment = %v", comment)
	}
	commentID := fmt.Sprint(comment["id"])

	page := w.member.json("GET", "/items/"+itemID+"/comments", nil, http.StatusOK)
	if got := pageTotal(t, page); got != 1 {
		t.Fatalf("comment list total = %d, want 1", got)
	}

	if status, out := w.member2.do("DELETE", "/items/"+itemID+"/comments/"+commentID, nil); status != http.StatusForbidden {
		t.Fatalf("bystander delete = %d, want 403: %s", status, out)
	}
	w.member.mustDo("DELETE", "/items/"+itemID+"/comments/"+commentID, nil, http.StatusNoContent)

	second := w.member.json("POST", "/items/"+itemID+"/comments", map[string]any{"body": "second " + unique()}, http.StatusCreated)
	w.admin.mustDo("DELETE", "/items/"+itemID+"/comments/"+fmt.Sprint(second["id"]), nil, http.StatusNoContent)
}
