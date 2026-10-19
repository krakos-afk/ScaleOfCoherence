package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	scaleco "ScaleOfCoherence"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/comments"
	"ScaleOfCoherence/internal/database/repository"
	"ScaleOfCoherence/internal/items"
	"ScaleOfCoherence/internal/teams"
	"ScaleOfCoherence/internal/workflows"

	"github.com/golang-migrate/migrate/v4"
	mpostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// These tests run the full HTTP stack (router → middleware → controllers →
// services → repositories) against a scratch Postgres so that organization
// isolation and the authorization matrix are verified end to end. They skip
// when Postgres is unreachable, like the repository integration tests.

var (
	apiServerOnce sync.Once
	apiServer     *httptest.Server
	apiSkip       string
)

func startAPI(t *testing.T) *httptest.Server {
	t.Helper()
	apiServerOnce.Do(func() {
		dsn := os.Getenv("TEST_DATABASE_URL")
		if dsn == "" {
			dsn = "postgres://postgres:postgres@localhost:5432/scaleco_api_test?sslmode=disable"
		}
		db, err := setupAPIDB(dsn)
		if err != nil {
			apiSkip = "skipping api integration tests: " + err.Error()
			return
		}
		secret := strings.Repeat("s", 32)
		router := NewRouter(
			auth.NewService(repository.NewAuthRepo(db), secret, time.Hour),
			teams.NewService(repository.NewTeamRepo(db)),
			items.NewService(repository.NewItemRepo(db)),
			workflows.NewService(repository.NewWorkflowRepo(db)),
			comments.NewService(repository.NewCommentRepo(db)),
			repository.NewIdempotencyRepo(db),
			dbPinger{db},
			"",
		)
		apiServer = httptest.NewServer(router)
	})
	if apiSkip != "" {
		t.Skip(apiSkip)
	}
	return apiServer
}

type dbPinger struct{ db *sql.DB }

func (p dbPinger) Ping(ctx context.Context) error { return p.db.PingContext(ctx) }

func setupAPIDB(dsn string) (*sql.DB, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, errors.New("test database name is empty")
	}

	adminURL := *u
	adminURL.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		return nil, err
	}
	quoted := `"` + strings.ReplaceAll(dbName, `"`, `""`) + `"`
	if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + quoted + ` WITH (FORCE)`); err != nil {
		admin.Close()
		return nil, err
	}
	if _, err := admin.Exec(`CREATE DATABASE ` + quoted); err != nil {
		admin.Close()
		return nil, err
	}
	admin.Close()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	src, err := iofs.New(scaleco.Migrations, "migrations")
	if err != nil {
		db.Close()
		return nil, err
	}
	driver, err := mpostgres.WithInstance(db, &mpostgres.Config{})
	if err != nil {
		db.Close()
		return nil, err
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		db.Close()
		return nil, err
	}
	return db, nil
}

// ── HTTP helpers ──────────────────────────────────────────────────────────────

type apiClient struct {
	t     *testing.T
	base  string
	token string
}

func (c apiClient) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	status, _, out := c.doFull(method, path, body, "")
	return status, out
}

func (c apiClient) doHeader(method, path string, body any, idemKey string) (int, []byte) {
	c.t.Helper()
	status, _, out := c.doFull(method, path, body, idemKey)
	return status, out
}

func (c apiClient) doFull(method, path string, body any, idemKey string) (int, http.Header, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, resp.Header, out
}

func decodeMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode: %v (%s)", err, raw)
	}
	return m
}

func (c apiClient) mustDo(method, path string, body any, want int) []byte {
	c.t.Helper()
	status, out := c.do(method, path, body)
	if status != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, out)
	}
	return out
}

func (c apiClient) json(method, path string, body any, want int) map[string]any {
	c.t.Helper()
	out := c.mustDo(method, path, body, want)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		c.t.Fatalf("decode %s %s: %v (%s)", method, path, err, out)
	}
	return m
}

// id reads a string field (uuid) from a JSON response.
func id(t *testing.T, m map[string]any, field string) string {
	t.Helper()
	v, ok := m[field].(string)
	if !ok || v == "" {
		t.Fatalf("field %q missing in %v", field, m)
	}
	return v
}

func signup(t *testing.T, ts *httptest.Server, org, username string) string {
	t.Helper()
	client := apiClient{t: t, base: ts.URL}
	body := client.json("POST", "/auth/signup", map[string]any{
		"org_name": org, "username": username,
		"email":    username + "@example.com",
		"password": "integration-password-1",
	}, http.StatusCreated)
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("signup returned no token: %v", body)
	}
	return token
}

// ── Organization isolation ────────────────────────────────────────────────────

func TestOrgIsolationAcrossEndpoints(t *testing.T) {
	ts := startAPI(t)
	a := apiClient{t: t, base: ts.URL, token: signup(t, ts, "org-a", "alice-"+unique())}
	b := apiClient{t: t, base: ts.URL, token: signup(t, ts, "org-b", "bob-"+unique())}

	// A's own user ID: needed to attempt adding it to a foreign team.
	usersOut := a.mustDo("GET", "/auth/users", nil, http.StatusOK)
	var users []map[string]any
	if err := json.Unmarshal(usersOut, &users); err != nil {
		t.Fatalf("decode /auth/users: %v (%s)", err, usersOut)
	}
	aUserID := ""
	if len(users) > 0 {
		aUserID, _ = users[0]["id"].(string)
	}
	if aUserID == "" {
		t.Fatal("could not resolve admin A user id")
	}

	// B owns one resource of every kind; names carry markers so leaks are
	// detectable in any response body.
	teamID := id(t, b.json("POST", "/teams", map[string]any{"name": marker("team")}, http.StatusCreated), "id")
	wfID := id(t, b.json("POST", "/workflows", map[string]any{"name": marker("workflow"), "description": ""}, http.StatusCreated), "id")
	stageID := id(t, b.json("POST", "/workflows/"+wfID+"/stages", map[string]any{
		"name": marker("stage"), "assigned_team_id": teamID, "position": 1,
	}, http.StatusCreated), "id")
	itemID := id(t, b.json("POST", "/items", map[string]any{
		"title": marker("item"), "workflow_id": wfID, "current_stage_id": stageID,
	}, http.StatusCreated), "id")
	comment := b.json("POST", "/items/"+itemID+"/comments", map[string]any{"body": marker("comment")}, http.StatusCreated)
	commentID := fmt.Sprint(comment["id"])

	probes := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
	}{
		{"read item", "GET", "/items/" + itemID, nil, http.StatusNotFound},
		{"update item", "PUT", "/items/" + itemID, map[string]any{"title": "hijack", "version": 1}, http.StatusNotFound},
		{"delete item", "DELETE", "/items/" + itemID, nil, http.StatusNotFound},
		{"close item", "POST", "/items/" + itemID + "/status", map[string]any{"closed": true, "version": 1}, http.StatusNotFound},
		{"claim foreign activity", "POST", "/items/" + itemID + "/activity/claim", map[string]any{"version": 1}, http.StatusNotFound},
		{"timeline", "GET", "/items/" + itemID + "/timeline", nil, http.StatusOK},
		{"list comments", "GET", "/items/" + itemID + "/comments", nil, http.StatusOK},
		{"comment", "POST", "/items/" + itemID + "/comments", map[string]any{"body": "intrusion"}, http.StatusNotFound},
		{"delete comment", "DELETE", "/items/" + itemID + "/comments/" + commentID, nil, http.StatusNotFound},
		{"read team", "GET", "/teams/" + teamID, nil, http.StatusNotFound},
		{"update team", "PUT", "/teams/" + teamID, map[string]any{"name": "hijack"}, http.StatusNotFound},
		{"delete team", "DELETE", "/teams/" + teamID, nil, http.StatusNotFound},
		{"list team members", "GET", "/teams/" + teamID + "/members", nil, http.StatusNotFound},
		{"add team member", "POST", "/teams/" + teamID + "/members", map[string]any{"user_id": aUserID, "role": "leader"}, http.StatusNotFound},
		{"read workflow", "GET", "/workflows/" + wfID, nil, http.StatusNotFound},
		{"update workflow", "PUT", "/workflows/" + wfID, map[string]any{"name": "hijack"}, http.StatusNotFound},
		{"delete workflow", "DELETE", "/workflows/" + wfID, nil, http.StatusNotFound},
		{"list stages", "GET", "/workflows/" + wfID + "/stages", nil, http.StatusNotFound},
		{"create stage", "POST", "/workflows/" + wfID + "/stages", map[string]any{"name": "s", "assigned_team_id": teamID, "position": 9}, http.StatusNotFound},
		{"update stage", "PUT", "/workflows/" + wfID + "/stages/" + stageID, map[string]any{"name": "hijack", "assigned_team_id": teamID, "position": 1}, http.StatusNotFound},
		{"delete stage", "DELETE", "/workflows/" + wfID + "/stages/" + stageID, nil, http.StatusNotFound},
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			status, out := a.do(p.method, p.path, p.body)
			if status != p.want {
				t.Fatalf("%s %s = %d, want %d: %s", p.method, p.path, status, p.want, out)
			}
			if leaked := leakedMarker(out); leaked != "" {
				t.Fatalf("response leaked org B data %q: %s", leaked, out)
			}
		})
	}

	// Search/list endpoints must not surface foreign rows either.
	for _, p := range []string{"/items?q=" + marker("item"), "/teams?q=" + marker("team"), "/workflows?q=" + marker("workflow")} {
		status, out := a.do("GET", p, nil)
		if status != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", p, status, out)
		}
		if leaked := leakedMarker(out); leaked != "" {
			t.Fatalf("search %s leaked %q: %s", p, leaked, out)
		}
	}

	// And B's own resources still work for B (guards against over-restricting).
	b.mustDo("GET", "/items/"+itemID, nil, http.StatusOK)
	b.mustDo("GET", "/teams/"+teamID, nil, http.StatusOK)
}

// ── Authorization matrix ──────────────────────────────────────────────────────

func TestAuthorizationMatrix(t *testing.T) {
	ts := startAPI(t)
	admin := apiClient{t: t, base: ts.URL, token: signup(t, ts, "org-authz", "admin-"+unique())}
	uname := "plain-" + unique()
	admin.json("POST", "/auth/users", map[string]any{
		"username": uname, "email": uname + "@example.com",
		"password": "integration-password-1", "usertype": "user",
	}, http.StatusCreated)

	login := apiClient{t: t, base: ts.URL}
	token := login.json("POST", "/auth/login", map[string]any{
		"email": uname + "@example.com", "password": "integration-password-1",
	}, http.StatusOK)["token"].(string)
	member := apiClient{t: t, base: ts.URL, token: token}

	teamID := id(t, admin.json("POST", "/teams", map[string]any{"name": "team-" + unique()}, http.StatusCreated), "id")
	wfID := id(t, admin.json("POST", "/workflows", map[string]any{"name": "wf-" + unique()}, http.StatusCreated), "id")
	stageID := id(t, admin.json("POST", "/workflows/"+wfID+"/stages", map[string]any{
		"name": "stage", "assigned_team_id": teamID, "position": 1,
	}, http.StatusCreated), "id")
	itemID := id(t, admin.json("POST", "/items", map[string]any{
		"title": "authz-item", "workflow_id": wfID, "current_stage_id": stageID,
	}, http.StatusCreated), "id")

	// Regular org members may create items and edit them...
	member.mustDo("POST", "/items", map[string]any{"title": "member-item"}, http.StatusCreated)

	// ...but every admin-only endpoint rejects them.
	for _, p := range []struct {
		name, method, path string
		body               any
	}{
		{"list users", "GET", "/auth/users", nil},
		{"create user", "POST", "/auth/users", map[string]any{"username": "x", "email": "x@example.com", "password": "integration-password-1", "usertype": "user"}},
		{"create team", "POST", "/teams", map[string]any{"name": "x"}},
		{"delete item", "DELETE", "/items/" + itemID, nil},
		{"create workflow", "POST", "/workflows", map[string]any{"name": "x"}},
	} {
		t.Run(p.name, func(t *testing.T) {
			status, out := member.do(p.method, p.path, p.body)
			if status != http.StatusForbidden {
				t.Fatalf("%s %s = %d, want 403: %s", p.method, p.path, status, out)
			}
		})
	}

	// A team viewer can read the item but cannot claim its activity or comment.
	viewerName := "viewer-" + unique()
	viewerBody := admin.json("POST", "/auth/users", map[string]any{
		"username": viewerName, "email": viewerName + "@example.com",
		"password": "integration-password-1", "usertype": "user",
	}, http.StatusCreated)
	viewerID := id(t, viewerBody, "id")
	admin.mustDo("POST", "/teams/"+teamID+"/members", map[string]any{"user_id": viewerID, "role": "viewer"}, http.StatusCreated)

	viewerLogin := apiClient{t: t, base: ts.URL}
	viewerToken := viewerLogin.json("POST", "/auth/login", map[string]any{
		"email": viewerName + "@example.com", "password": "integration-password-1",
	}, http.StatusOK)["token"].(string)
	viewer := apiClient{t: t, base: ts.URL, token: viewerToken}

	viewer.mustDo("GET", "/items/"+itemID, nil, http.StatusOK)
	if status, out := viewer.do("POST", "/items/"+itemID+"/activity/claim", map[string]any{"version": 1}); status != http.StatusForbidden {
		t.Fatalf("viewer claim = %d, want 403: %s", status, out)
	}
	if status, out := viewer.do("POST", "/items/"+itemID+"/comments", map[string]any{"body": "viewer note"}); status != http.StatusForbidden {
		t.Fatalf("viewer comment = %d, want 403: %s", status, out)
	}
}

// ── Idempotency end to end ────────────────────────────────────────────────────

func TestIdempotencyEndToEnd(t *testing.T) {
	ts := startAPI(t)
	admin := apiClient{t: t, base: ts.URL, token: signup(t, ts, "org-idem", "idem-"+unique())}

	key := "e2e-key-" + unique()
	title := "idem-item-" + unique()
	body := map[string]any{"title": title}

	status1, header1, out1 := admin.doFull("POST", "/items", body, key)
	if status1 != http.StatusCreated {
		t.Fatalf("first create = %d: %s", status1, out1)
	}
	if header1.Get("Idempotency-Replayed") != "" {
		t.Fatal("first request must not be marked as a replay")
	}
	itemID1 := id(t, decodeMap(t, out1), "id")

	status2, header2, out2 := admin.doFull("POST", "/items", body, key)
	if status2 != http.StatusCreated {
		t.Fatalf("replay = %d: %s", status2, out2)
	}
	if header2.Get("Idempotency-Replayed") != "true" {
		t.Fatal("replay missing Idempotency-Replayed header")
	}
	if string(out1) != string(out2) {
		t.Fatalf("replay body differs:\n%s\n%s", out1, out2)
	}

	// Exactly one item was created.
	page := admin.json("GET", "/items?q="+title, nil, http.StatusOK)
	if total, _ := page["total"].(float64); total != 1 {
		t.Fatalf("expected exactly one item, total = %v", page["total"])
	}
	admin.mustDo("GET", "/items/"+itemID1, nil, http.StatusOK)

	// The same key with a different body is a client bug.
	if status, out := admin.doHeader("POST", "/items", map[string]any{"title": "different"}, key); status != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch = %d, want 422: %s", status, out)
	}
}

// ── Markers ───────────────────────────────────────────────────────────────────

func unique() string {
	return fmt.Sprint(os.Getpid(), "-", time.Now().UnixNano()%1_000_000)
}

// marker returns a distinctive string that a foreign org's responses must
// never contain.
func marker(kind string) string {
	return "zz-secret-" + kind + "-" + unique()
}

// leakedMarker scans a response body for any of the current test run's markers.
func leakedMarker(body []byte) string {
	s := string(body)
	for _, kind := range []string{"item", "team", "workflow", "stage", "comment"} {
		prefix := "zz-secret-" + kind + "-"
		if i := strings.Index(s, prefix); i >= 0 {
			end := i + len(prefix)
			for end < len(s) && (s[end] >= '0' && s[end] <= '9' || s[end] == '-') {
				end++
			}
			return s[i:end]
		}
	}
	return ""
}
