// Package demo seeds a demo workspace and walks the main flows using only the
// server's public HTTP API:
//
//   - stable demo accounts for each role, matching the frontend login page;
//   - a team-owned workflow with an approval stage;
//   - item versioning with optimistic concurrency (a stale write gets 409);
//   - role checks for viewer, commentator, editor and team leader;
//   - claim, complete, reject, approve and advance;
//   - the merged item timeline.
//
// It never touches the database. The workspace is seeded once and reused on
// later runs, so it is safe to run at startup or from the demo command.
package demo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	demoPassword = "demo-password-123"
	demoDomain   = "demo.local"
	demoOrg      = "Scale Demo"
	demoTeam     = "Demo Team"
	demoWorkflow = "Demo Workflow"
	demoStage1   = "Design"
	demoStage2   = "Build"

	demoAdmin       = "demo-admin"
	demoLeader      = "demo-leader"
	demoEditor      = "demo-editor"
	demoCommentator = "demo-commentator"
	demoViewer      = "demo-viewer"
	demoMember      = "demo-member"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

type client struct {
	ctx   context.Context
	base  string
	token string
	out   io.Writer
}

type reply struct {
	status int
	body   []byte
}

func (c *client) send(method, path string, payload any) reply {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			die("encode request: %v", err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, c.base+path, body)
	if err != nil {
		die("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		die("%s %s: %v\nis the server running at %s?", method, path, err, c.base)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		die("read %s %s: %v", method, path, err)
	}
	fmt.Fprintf(c.out, "   %-6s %-52s -> %d\n", method, path, resp.StatusCode)
	return reply{status: resp.StatusCode, body: raw}
}

// call performs a request, requires the expected status, and decodes a JSON
// object. It exits with the response body on any mismatch.
func (c *client) call(method, path string, payload any, want int, what string) map[string]any {
	r := c.send(method, path, payload)
	if r.status != want {
		die("%s: %s %s = %d, want %d\n%s", what, method, path, r.status, want, r.body)
	}
	var m map[string]any
	if len(r.body) > 0 {
		if err := json.Unmarshal(r.body, &m); err != nil {
			die("decode %s: %v (%s)", what, err, r.body)
		}
	}
	return m
}

func (c *client) callList(method, path string, want int, what string) []map[string]any {
	r := c.send(method, path, nil)
	if r.status != want {
		die("%s: %s %s = %d, want %d\n%s", what, method, path, r.status, want, r.body)
	}
	var list []map[string]any
	if err := json.Unmarshal(r.body, &list); err != nil {
		die("decode %s: %v (%s)", what, err, r.body)
	}
	return list
}

func (c *client) callPage(method, path string, want int, what string) []map[string]any {
	r := c.send(method, path, nil)
	if r.status != want {
		die("%s: %s %s = %d, want %d\n%s", what, method, path, r.status, want, r.body)
	}
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(r.body, &page); err != nil {
		die("decode %s: %v (%s)", what, err, r.body)
	}
	return page.Data
}

func (c *client) itemVersion(itemID string) int64 {
	return num(c.call("GET", "/items/"+itemID, nil, http.StatusOK, "get item"), "version")
}

// Run seeds the demo workspace and walks one item through its workflow. It is
// safe to call repeatedly; the workspace and accounts are reused.
func Run(ctx context.Context, baseURL string, out io.Writer) (err error) {
	defer func() {
		if p := recover(); p != nil {
			f, ok := p.(failure)
			if !ok {
				panic(p)
			}
			err = f.err
		}
	}()
	if out == nil {
		out = io.Discard
	}
	step := func(title string) {
		fmt.Fprintf(out, "\n== %s ==\n", title)
	}
	note := func(format string, args ...any) {
		fmt.Fprintf(out, "   "+format+"\n", args...)
	}

	admin := &client{ctx: ctx, base: baseURL, out: out}

	step("1. Seed or reuse the demo workspace")
	r := admin.send("POST", "/auth/login", map[string]any{
		"email": emailFor(demoAdmin), "password": demoPassword,
	})
	switch r.status {
	case http.StatusOK:
		admin.token = tokenFrom(r)
		note("reusing org %q", demoOrg)
	case http.StatusUnauthorized:
		admin.token = str(admin.call("POST", "/auth/signup", map[string]any{
			"org_name": demoOrg, "username": demoAdmin,
			"email": emailFor(demoAdmin), "password": demoPassword,
		}, http.StatusCreated, "signup"), "token")
		note("created org %q", demoOrg)
	default:
		die("login %s = %d\n%s", demoAdmin, r.status, r.body)
	}

	ids := make(map[string]string)
	for _, u := range admin.callList("GET", "/auth/users?page=1&limit=100", http.StatusOK, "list users") {
		ids[asString(u["username"])] = str(u, "id")
	}
	if ids[demoAdmin] == "" {
		die("%s is missing from /auth/users", demoAdmin)
	}
	ensureUser := func(username string) string {
		if id, ok := ids[username]; ok {
			return id
		}
		id := str(admin.call("POST", "/auth/users", map[string]any{
			"username": username, "email": emailFor(username),
			"password": demoPassword, "usertype": "user",
		}, http.StatusCreated, "create "+username), "id")
		ids[username] = id
		note("created user %s", username)
		return id
	}
	for _, name := range []string{demoLeader, demoEditor, demoCommentator, demoViewer, demoMember} {
		ensureUser(name)
	}

	login := func(username string) *client {
		c := &client{ctx: ctx, base: baseURL, out: out}
		c.token = str(c.call("POST", "/auth/login", map[string]any{
			"email": emailFor(username), "password": demoPassword,
		}, http.StatusOK, "login "+username), "token")
		return c
	}
	leader := login(demoLeader)
	editor := login(demoEditor)
	commentator := login(demoCommentator)
	viewer := login(demoViewer)

	teamID := ""
	for _, t := range admin.callPage("GET", "/teams?page=1&limit=100", http.StatusOK, "list teams") {
		if asString(t["name"]) == demoTeam {
			teamID = str(t, "id")
		}
	}
	if teamID == "" {
		teamID = str(admin.call("POST", "/teams", map[string]any{
			"name": demoTeam,
		}, http.StatusCreated, "create team"), "id")
		note("created team %q", demoTeam)
	} else {
		note("reusing team %q", demoTeam)
	}

	roles := make(map[string]string)
	for _, m := range admin.callPage("GET", "/teams/"+teamID+"/members?page=1&limit=100", http.StatusOK, "list members") {
		roles[str(m, "user_id")] = str(m, "role")
	}
	ensureMember := func(username, role string) {
		id := ids[username]
		if current, ok := roles[id]; ok {
			if current == role {
				return
			}
			admin.call("DELETE", "/teams/"+teamID+"/members/"+id, nil, http.StatusNoContent, "remove "+username)
		}
		admin.call("POST", "/teams/"+teamID+"/members", map[string]any{
			"user_id": id, "role": role,
		}, http.StatusCreated, "add "+username)
		roles[id] = role
		note("team role: %s = %s", username, role)
	}
	ensureMember(demoLeader, "leader")
	ensureMember(demoEditor, "editor")
	ensureMember(demoCommentator, "commentator")
	ensureMember(demoViewer, "viewer")

	wfID := ""
	for _, w := range admin.callPage("GET", "/workflows?page=1&limit=100", http.StatusOK, "list workflows") {
		if asString(w["name"]) == demoWorkflow {
			wfID = str(w, "id")
		}
	}
	if wfID == "" {
		wfID = str(admin.call("POST", "/workflows", map[string]any{
			"name": demoWorkflow, "description": "Demo flow with an approval gate",
		}, http.StatusCreated, "create workflow"), "id")
		note("created workflow %q", demoWorkflow)
	} else {
		note("reusing workflow %q", demoWorkflow)
	}

	stageIDs := make(map[string]string)
	for _, s := range admin.callList("GET", "/workflows/"+wfID+"/stages", http.StatusOK, "list stages") {
		stageIDs[asString(s["name"])] = str(s, "id")
	}
	ensureStage := func(name string, position int, approval bool) string {
		if id, ok := stageIDs[name]; ok {
			return id
		}
		payload := map[string]any{
			"name": name, "assigned_team_id": teamID, "position": position,
		}
		if approval {
			payload["approval_required"] = true
			payload["approver_user_id"] = ids[demoAdmin]
		}
		id := str(admin.call("POST", "/workflows/"+wfID+"/stages", payload, http.StatusCreated, "create stage "+name), "id")
		stageIDs[name] = id
		note("created stage %q", name)
		return id
	}
	stage1ID := ensureStage(demoStage1, 1, true)
	stage2ID := ensureStage(demoStage2, 2, false)

	step("2. Create an item as the editor")
	item := editor.call("POST", "/items", map[string]any{
		"title":       "Ship the demo " + time.Now().Format("15:04:05"),
		"description": "Versioning, approvals and the timeline.",
		"priority":    "high", "workflow_id": wfID, "current_stage_id": stage1ID,
	}, http.StatusCreated, "create item")
	itemID := str(item, "id")
	ver := num(item, "version")
	note("item %s is version %d on stage %s", itemID, ver, demoStage1)

	open := editor.call("GET", "/items/"+itemID+"/activity/open", nil, http.StatusOK, "open activity")
	if open["assigned_user_id"] != nil {
		die("new activity should be unclaimed: %v", open)
	}
	note("an unclaimed activity was opened for the stage team")

	step("3. Versioning and optimistic concurrency")
	update := func(title string, version int64, want int) (map[string]any, int64) {
		m := editor.call("PUT", "/items/"+itemID, map[string]any{
			"title": title, "description": "Versioning, approvals and the timeline.",
			"priority": "high", "workflow_id": wfID, "current_stage_id": stage1ID,
			"version": version,
		}, want, "update item")
		if want == http.StatusOK {
			return m, num(m, "version")
		}
		return m, 0
	}
	_, next := update("Ship the demo (edited)", ver, http.StatusOK)
	note("editor saved with version %d -> version %d", ver, next)
	stale, _ := update("Ship the demo (stale save)", ver, http.StatusConflict)
	note("saving again with version %d was rejected: %q, server version %v",
		ver, str(stale, "error"), stale["version"])
	ver = next
	_, next = update("Ship the demo (final)", ver, http.StatusOK)
	note("refetched version %d, retried -> version %d", ver, next)
	ver = next

	step("4. Team roles are enforced")
	if r := editor.send("POST", "/items/"+itemID+"/advance", map[string]any{"version": ver}); r.status != http.StatusForbidden {
		die("editor advancing = %d, want 403: %s", r.status, r.body)
	}
	note("editor -> advance: 403 (only a team leader or admin may advance)")
	for _, p := range []struct {
		c    *client
		name string
	}{{viewer, demoViewer}, {commentator, demoCommentator}} {
		if r := p.c.send("POST", "/items/"+itemID+"/activity/claim", map[string]any{"version": ver}); r.status != http.StatusForbidden {
			die("%s claiming = %d, want 403: %s", p.name, r.status, r.body)
		}
	}
	note("viewer and commentator -> claim: 403")
	comment := commentator.call("POST", "/items/"+itemID+"/comments", map[string]any{
		"body": "Acceptance criteria: the demo completes end to end.",
	}, http.StatusCreated, "comment")
	note("commentator -> comment allowed, anchored to version %v", comment["version"])
	viewer.call("GET", "/items/"+itemID+"/comments", nil, http.StatusOK, "read comments")
	note("viewer can read the item, its timeline and comments")

	step("5. Claim, complete, and the approval loop")
	editor.call("POST", "/items/"+itemID+"/activity/claim", map[string]any{
		"version": ver,
	}, http.StatusOK, "claim")
	ver = editor.itemVersion(itemID)
	note("editor claimed the activity (version %d)", ver)

	editor.call("POST", "/items/"+itemID+"/activity/complete", map[string]any{
		"note": "Design is ready", "version": ver,
	}, http.StatusOK, "complete")
	ver = editor.itemVersion(itemID)
	note("completed; the stage requires approval, so the activity is pending (version %d)", ver)

	attention := admin.call("GET", "/items/attention", nil, http.StatusOK, "attention")
	note("admin dashboard: %v approval(s) waiting, %d item(s) listed",
		attention["approval_total"], len(attention["items"].([]any)))

	admin.call("POST", "/items/"+itemID+"/activity/approve", map[string]any{
		"approved": false, "note": "Add acceptance criteria", "version": ver,
	}, http.StatusOK, "reject")
	ver = editor.itemVersion(itemID)
	note("admin rejected with a note; the activity reopened unclaimed (version %d)", ver)

	editor.call("POST", "/items/"+itemID+"/activity/claim", map[string]any{
		"version": ver,
	}, http.StatusOK, "re-claim")
	ver = editor.itemVersion(itemID)
	note("editor re-claimed the reworked activity (version %d)", ver)

	editor.call("POST", "/items/"+itemID+"/activity/complete", map[string]any{
		"note": "Acceptance criteria added", "version": ver,
	}, http.StatusOK, "re-complete")
	ver = editor.itemVersion(itemID)
	note("completed again (version %d); now pending approval", ver)

	approved := admin.call("POST", "/items/"+itemID+"/activity/approve", map[string]any{
		"approved": true, "note": "Approved", "version": ver,
	}, http.StatusOK, "approve")
	ver = editor.itemVersion(itemID)
	note("admin approved (status %q), activity released (version %d)", str(approved, "approval_status"), ver)

	step("6. Advance to the next stage")
	advanced := leader.call("POST", "/items/"+itemID+"/advance", map[string]any{
		"version": ver,
	}, http.StatusOK, "advance")
	ver = num(advanced, "version")
	open = editor.call("GET", "/items/"+itemID+"/activity/open", nil, http.StatusOK, "open activity")
	note("leader advanced to stage %s (version %d); new activity unclaimed: %v",
		demoStage2, ver, open["assigned_user_id"] == nil)

	editor.call("POST", "/items/"+itemID+"/activity/claim", map[string]any{
		"version": ver,
	}, http.StatusOK, "claim stage 2")
	ver = editor.itemVersion(itemID)
	note("editor claimed the %s activity (version %d)", demoStage2, ver)

	editor.call("POST", "/items/"+itemID+"/activity/complete", map[string]any{
		"note": "Built", "version": ver,
	}, http.StatusOK, "complete stage 2")
	ver = editor.itemVersion(itemID)
	note("completed on a plain stage, so the activity was released (version %d)", ver)

	step("7. Close the item")
	closed := leader.call("POST", "/items/"+itemID+"/status", map[string]any{
		"closed": true, "version": ver,
	}, http.StatusOK, "close")
	ver = num(closed, "version")
	note("closed (version %d)", ver)

	step("8. The merged timeline")
	tl := editor.call("GET", "/items/"+itemID+"/timeline?limit=50", nil, http.StatusOK, "timeline")
	entries := tl["entries"].([]any)
	stages := map[string]string{stage1ID: demoStage1, stage2ID: demoStage2}
	names := map[string]string{
		ids[demoAdmin]: demoAdmin, ids[demoLeader]: demoLeader,
		ids[demoEditor]: demoEditor, ids[demoCommentator]: demoCommentator,
	}
	for i := len(entries) - 1; i >= 0; i-- {
		e, _ := entries[i].(map[string]any)
		actor := ""
		if a, ok := e["actor"].(map[string]any); ok {
			actor, _ = a["username"].(string)
		}
		fmt.Fprintf(out, "   v%-2d %-11v %-28s %s\n",
			int64(e["version"].(float64)), e["type"], actor, detail(e, stages, names))
	}

	step("Demo ready")
	fmt.Fprintf(out, "   Org:      %s\n", demoOrg)
	fmt.Fprintf(out, "   Item:     %s (closed, final version %d)\n", itemID, ver)
	fmt.Fprintf(out, "   Login at: %s (the login page lists these accounts)\n", baseURL)
	fmt.Fprintf(out, "   Password: %s\n\n", demoPassword)
	fmt.Fprintf(out, "   %-17s %-28s %s\n", "ACCOUNT", "EMAIL", "ROLE")
	for _, a := range []struct {
		name, role string
	}{
		{demoAdmin, "org admin"},
		{demoLeader, "user · team leader"},
		{demoEditor, "user · editor"},
		{demoCommentator, "user · commentator"},
		{demoViewer, "user · viewer"},
		{demoMember, "user · no team role"},
	} {
		fmt.Fprintf(out, "   %-17s %-28s %s\n", a.name, emailFor(a.name), a.role)
	}
	return nil
}

// failure carries a demo error out of the panic-based helpers below so Run can
// return it instead of exiting the host process.
type failure struct{ err error }

// detail renders the extra information carried by a timeline entry. Notes are
// not shown because the timeline references the activity's current state, not
// a per-version snapshot.
func detail(e map[string]any, stages, users map[string]string) string {
	changes, _ := e["changes"].(map[string]any)
	switch e["type"] {
	case "created":
		return "item created"
	case "updated":
		keys := make([]string, 0, len(changes))
		for k := range changes {
			if k != "type" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		return "changed: " + strings.Join(keys, ", ")
	case "assigned":
		if name := users[asString(changes["to"])]; name != "" {
			return "to " + name
		}
		return "unassigned"
	case "stage_moved":
		return stages[asString(changes["from"])] + " -> " + stages[asString(changes["to"])]
	case "closed":
		if closed, _ := changes["closed"].(bool); closed {
			return "item closed"
		}
		return "item reopened"
	}
	return ""
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func emailFor(username string) string {
	return username + "@" + demoDomain
}

func tokenFrom(r reply) string {
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		die("decode login response: %v (%s)", err, r.body)
	}
	return str(m, "token")
}

func str(m map[string]any, key string) string {
	v, ok := m[key].(string)
	if !ok || v == "" {
		die("response is missing %q: %v", key, m)
	}
	return v
}

func num(m map[string]any, key string) int64 {
	v, ok := m[key].(float64)
	if !ok {
		die("response is missing %q: %v", key, m)
	}
	return int64(v)
}

func die(format string, args ...any) {
	panic(failure{err: fmt.Errorf(format, args...)})
}
