package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"ScaleOfCoherence/internal/api/dto"

	"github.com/golang-migrate/migrate/v4"
	mpostgres "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/scaleco_test?sslmode=disable"
	}
	db, err := setupTestDB(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "skipping repository integration tests: %v\n", err)
		os.Exit(0)
	}
	testDB = db
	code := m.Run()
	_ = db.Close()
	os.Exit(code)
}

func setupTestDB(dsn string) (*sql.DB, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, errors.New("test database name is empty")
	}
	// Use a package-specific database so `go test ./...` can run the API and
	// repository integration suites concurrently without dropping each
	// other's database.
	dbName += "_repo"
	u.Path = "/" + dbName
	dsn = u.String()

	adminURL := *u
	adminURL.Path = "/postgres"
	admin, err := sql.Open("pgx", adminURL.String())
	if err != nil {
		return nil, err
	}
	if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + quoteIdent(dbName) + ` WITH (FORCE)`); err != nil {
		admin.Close()
		return nil, err
	}
	if _, err := admin.Exec(`CREATE DATABASE ` + quoteIdent(dbName)); err != nil {
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

	migrationsPath, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		db.Close()
		return nil, err
	}
	driver, err := mpostgres.WithInstance(db, &mpostgres.Config{})
	if err != nil {
		db.Close()
		return nil, err
	}
	m, err := migrate.NewWithDatabaseInstance("file://"+migrationsPath, "postgres", driver)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		db.Close()
		return nil, err
	}
	return db, nil
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func resetDB(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec(`
		TRUNCATE idempotency_keys, item_comments, item_stage_activity, item_changes, items, workflow_stages, workflows,
		         team_members, teams, organization_members, users, organization
		RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// ── Seed helpers ──────────────────────────────────────────────────────────────

func seedOrg(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := testDB.QueryRow(`INSERT INTO organization (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	return id
}

func seedUser(t *testing.T, orgID, username, usertype string) string {
	t.Helper()
	var id string
	if err := testDB.QueryRow(`
		INSERT INTO users (email, username, password_hash)
		VALUES ($1, $2, 'x') RETURNING id`,
		username+"@example.com", username).Scan(&id); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := testDB.Exec(`
		INSERT INTO organization_members (org_id, user_id, usertype)
		VALUES ($1, $2, $3)`, orgID, id, usertype); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return id
}

func seedTeam(t *testing.T, orgID, name string) string {
	t.Helper()
	var id string
	if err := testDB.QueryRow(`INSERT INTO teams (org_id, name) VALUES ($1, $2) RETURNING id`,
		orgID, name).Scan(&id); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	return id
}

func seedTeamMember(t *testing.T, orgID, teamID, userID, role string) {
	t.Helper()
	if _, err := testDB.Exec(`
		INSERT INTO team_members (org_id, team_id, user_id, role) VALUES ($1, $2, $3, $4)`,
		orgID, teamID, userID, role); err != nil {
		t.Fatalf("seed team member: %v", err)
	}
}

func seedWorkflow(t *testing.T, orgID, creatorID, name string) string {
	t.Helper()
	var id string
	if err := testDB.QueryRow(`
		INSERT INTO workflows (org_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`,
		orgID, name, creatorID).Scan(&id); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	return id
}

func seedStage(t *testing.T, orgID, workflowID, teamID, name string, position int, approvalRequired bool, approverID *string) string {
	t.Helper()
	var id string
	if err := testDB.QueryRow(`
		INSERT INTO workflow_stages
		    (org_id, workflow_id, assigned_team_id, name, position, approval_required, approver_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		orgID, workflowID, teamID, name, position, approvalRequired, approverID).Scan(&id); err != nil {
		t.Fatalf("seed stage: %v", err)
	}
	return id
}

// world is a minimal org with an admin and a two-stage workflow.
type world struct {
	orgID      string
	adminID    string
	otherUser  string
	teamID     string
	workflowID string
	stageOne   string
	stageTwo   string
}

func seedWorld(t *testing.T, approvalRequired bool) world {
	t.Helper()
	orgID := seedOrg(t, "org-"+randName())
	adminID := seedUser(t, orgID, "admin-"+randName(), "admin")
	otherID := seedUser(t, orgID, "member-"+randName(), "user")
	teamID := seedTeam(t, orgID, "team-"+randName())
	seedTeamMember(t, orgID, teamID, adminID, "leader")
	seedTeamMember(t, orgID, teamID, otherID, "editor")
	workflowID := seedWorkflow(t, orgID, adminID, "wf-"+randName())
	var approver *string
	if approvalRequired {
		approver = &adminID
	}
	stageOne := seedStage(t, orgID, workflowID, teamID, "stage-one", 1, approvalRequired, approver)
	stageTwo := seedStage(t, orgID, workflowID, teamID, "stage-two", 2, false, nil)
	return world{orgID, adminID, otherID, teamID, workflowID, stageOne, stageTwo}
}

func (w world) createItem(t *testing.T, repo *ItemRepo) dto.Item {
	t.Helper()
	item, err := repo.Create(context.Background(), w.orgID, w.adminID, dto.ItemRequest{
		Title:          "item",
		Description:    "",
		Priority:       "medium",
		CurrentStageID: &w.stageOne,
	})
	if err != nil {
		t.Fatalf("create item: %v", err)
	}
	return item
}

func randName() string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = byte('a' + (i*7+int(os.Getpid()))%26)
	}
	return string(b) + fmt.Sprint(os.Getpid())
}

// ── Assertion helpers ─────────────────────────────────────────────────────────

func changesVersions(t *testing.T, orgID, itemID string) []int64 {
	t.Helper()
	rows, err := testDB.Query(`SELECT version FROM item_changes WHERE org_id=$1 AND item_id=$2 ORDER BY version`, orgID, itemID)
	if err != nil {
		t.Fatalf("query changes: %v", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, v)
	}
	return out
}

func countOpenActivity(t *testing.T, orgID, itemID string) int {
	t.Helper()
	var n int
	if err := testDB.QueryRow(`
		SELECT count(*) FROM item_stage_activity
		WHERE org_id=$1 AND item_id=$2 AND released_at IS NULL AND completed_at IS NULL`,
		orgID, itemID).Scan(&n); err != nil {
		t.Fatalf("count open activity: %v", err)
	}
	return n
}

func itemVersion(t *testing.T, orgID, itemID string) int64 {
	t.Helper()
	var v int64
	if err := testDB.QueryRow(`SELECT version FROM items WHERE org_id=$1 AND id=$2`, orgID, itemID).Scan(&v); err != nil {
		t.Fatalf("item version: %v", err)
	}
	return v
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// 1. Parallel claims with the same expected version: one wins, the rest get 409.
func TestConcurrentClaimSameVersion(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, repo)

	const n = 8
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		success int
		failed  int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, item.Version, true)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
				return
			}
			if errors.Is(err, ErrStale) || errors.Is(err, ErrDuplicate) {
				failed++
				return
			}
			t.Errorf("unexpected claim error: %v", err)
		}()
	}
	wg.Wait()

	if success != 1 {
		t.Fatalf("expected exactly 1 successful claim, got %d", success)
	}
	if failed != n-1 {
		t.Fatalf("expected %d conflicts, got %d", n-1, failed)
	}
	if got := countOpenActivity(t, w.orgID, item.ID); got != 1 {
		t.Fatalf("expected 1 open activity row, got %d", got)
	}
	// created (v1) + exactly one claim (v2)
	if versions := changesVersions(t, w.orgID, item.ID); len(versions) != 2 || versions[1] != 2 {
		t.Fatalf("expected change versions [1 2], got %v", versions)
	}
	if v := itemVersion(t, w.orgID, item.ID); v != 2 {
		t.Fatalf("expected item version 2, got %d", v)
	}
}

// 2. Reassign closes the old row and opens a new one in a single version bump.
func TestReassignSingleVersion(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, repo)

	// Claim so there is a concrete assignee to reassign away from.
	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 1, true); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if _, err := repo.ReassignActivity(context.Background(), w.orgID, item.ID, w.adminID, w.otherUser, 2, true); err != nil {
		t.Fatalf("reassign: %v", err)
	}

	if v := itemVersion(t, w.orgID, item.ID); v != 3 {
		t.Fatalf("expected item version 3 after reassign, got %d", v)
	}
	if got := countOpenActivity(t, w.orgID, item.ID); got != 1 {
		t.Fatalf("expected 1 open activity row, got %d", got)
	}

	var released int
	if err := testDB.QueryRow(`
		SELECT count(*) FROM item_stage_activity
		WHERE org_id=$1 AND item_id=$2 AND release_reason='reassigned'`, w.orgID, item.ID).Scan(&released); err != nil {
		t.Fatalf("count released: %v", err)
	}
	if released != 1 {
		t.Fatalf("expected 1 reassigned row, got %d", released)
	}

	var newVersionChanges int
	if err := testDB.QueryRow(`
		SELECT count(*) FROM item_changes
		WHERE org_id=$1 AND item_id=$2 AND version=3 AND changes->>'type'='assigned'`,
		w.orgID, item.ID).Scan(&newVersionChanges); err != nil {
		t.Fatalf("count assigned changes: %v", err)
	}
	if newVersionChanges != 1 {
		t.Fatalf("expected exactly one assigned change row at v3, got %d", newVersionChanges)
	}
}

// 3. Direct SQL activity insert with a stale item_version is rejected by the trigger.
func TestStaleActivityVersionRejected(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, repo)

	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 1, true); err != nil {
		t.Fatalf("claim: %v", err)
	}

	_, err := testDB.Exec(`
		INSERT INTO item_stage_activity
		    (org_id, item_id, stage_id, team_id, assigned_by, item_version)
		VALUES ($1, $2, $3, $4, $5, 1)`,
		w.orgID, item.ID, w.stageOne, w.teamID, w.adminID)
	if err == nil {
		t.Fatal("expected stale item_version insert to fail")
	}
	if !isSerializationFailure(err) {
		t.Fatalf("expected SQLSTATE 40001, got %v", err)
	}
}

// 4. Bumping items.version without an item_changes row fails at commit.
func TestItemVersionMustBeLogged(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, repo)

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(`UPDATE items SET version = version + 1 WHERE org_id=$1 AND id=$2`, w.orgID, item.ID); err != nil {
		tx.Rollback()
		t.Fatalf("update: %v", err)
	}
	if err := tx.Commit(); err == nil {
		t.Fatal("expected commit to fail because the new version has no item_changes row")
	}
}

// 5. Complete and approve each bump once; versions stay contiguous.
func TestCompleteApproveContiguousVersions(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, true)
	item := w.createItem(t, repo)

	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 1, true); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.CompleteActivity(context.Background(), w.orgID, item.ID, w.adminID, "done", 2); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := repo.ApproveActivity(context.Background(), w.orgID, item.ID, w.adminID, true, "lgtm", 3, true); err != nil {
		t.Fatalf("approve: %v", err)
	}

	versions := changesVersions(t, w.orgID, item.ID)
	if len(versions) != 4 {
		t.Fatalf("expected 4 change rows, got %v", versions)
	}
	for i, v := range versions {
		if v != int64(i+1) {
			t.Fatalf("expected contiguous versions 1..4, got %v", versions)
		}
	}
	if v := itemVersion(t, w.orgID, item.ID); v != 4 {
		t.Fatalf("expected item version 4, got %d", v)
	}
}

// 5b. Timeline entries keep the activity state from when they were written,
// even after the same activity row is rejected, re-claimed, and re-completed.
func TestTimelineActivitySnapshots(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, true)
	item := w.createItem(t, repo)

	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 1, true); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.CompleteActivity(context.Background(), w.orgID, item.ID, w.adminID, "first pass", 2); err != nil {
		t.Fatalf("complete first pass: %v", err)
	}
	if _, err := repo.ApproveActivity(context.Background(), w.orgID, item.ID, w.adminID, false, "redo", 3, true); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 4, true); err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if _, err := repo.CompleteActivity(context.Background(), w.orgID, item.ID, w.adminID, "second pass", 5); err != nil {
		t.Fatalf("complete second pass: %v", err)
	}
	if _, err := repo.ApproveActivity(context.Background(), w.orgID, item.ID, w.adminID, true, "ok", 6, true); err != nil {
		t.Fatalf("approve: %v", err)
	}

	page, err := repo.Timeline(context.Background(), w.orgID, item.ID, nil, 50)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	byVersion := map[int64]dto.TimelineEntry{}
	for _, e := range page.Entries {
		byVersion[e.Version] = e
	}
	entry := func(v int64) dto.TimelineEntry {
		t.Helper()
		e, ok := byVersion[v]
		if !ok {
			t.Fatalf("timeline missing version %d", v)
		}
		if e.Activity == nil {
			t.Fatalf("timeline version %d missing activity", v)
		}
		return e
	}
	assertString := func(label string, got *string, want string) {
		t.Helper()
		if got == nil || *got != want {
			t.Fatalf("%s = %v, want %q", label, got, want)
		}
	}

	created := entry(1)
	if created.Activity.Type != "created" || created.Activity.StageID != w.stageOne {
		t.Fatalf("created snapshot = %+v", created.Activity)
	}
	claimed := entry(2)
	if claimed.Activity.Type != "assigned" || claimed.Activity.AssignedUserID == nil || *claimed.Activity.AssignedUserID != w.adminID {
		t.Fatalf("claim snapshot = %+v", claimed.Activity)
	}
	first := entry(3)
	assertString("first completion note", first.Activity.CompletionNote, "first pass")
	assertString("first completion status", first.Activity.ApprovalStatus, "pending")
	if first.Activity.AssignedUserID == nil || *first.Activity.AssignedUserID != w.adminID {
		t.Fatalf("first completion assignee = %v", first.Activity.AssignedUserID)
	}
	rejected := entry(4)
	assertString("rejected note", rejected.Activity.ApprovalNote, "redo")
	assertString("rejected completion note", rejected.Activity.CompletionNote, "first pass")
	assertString("rejected status", rejected.Activity.ApprovalStatus, "rejected")
	second := entry(6)
	assertString("second completion note", second.Activity.CompletionNote, "second pass")
	approved := entry(7)
	assertString("approval note", approved.Activity.ApprovalNote, "ok")
	assertString("approval completion note", approved.Activity.CompletionNote, "second pass")
	assertString("approval status", approved.Activity.ApprovalStatus, "approved")

	// The earlier completion entry must not drift to the latest row state.
	assertString("first completion note after approve", entry(3).Activity.CompletionNote, "first pass")
}

// 5c. Reassignment entries retain their target even after the activity row is
// rejected and cleared for rework.
func TestTimelineAssignmentSnapshotAfterRejection(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, true)
	item := w.createItem(t, repo)

	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 1, true); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.ReassignActivity(context.Background(), w.orgID, item.ID, w.adminID, w.otherUser, 2, true); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if _, err := repo.CompleteActivity(context.Background(), w.orgID, item.ID, w.otherUser, "member work", 3); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := repo.ApproveActivity(context.Background(), w.orgID, item.ID, w.adminID, false, "redo", 4, true); err != nil {
		t.Fatalf("reject: %v", err)
	}

	page, err := repo.Timeline(context.Background(), w.orgID, item.ID, nil, 50)
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	for _, e := range page.Entries {
		if e.Version != 3 {
			continue
		}
		if e.Activity == nil || e.Activity.AssignedUserID == nil || *e.Activity.AssignedUserID != w.otherUser {
			t.Fatalf("reassignment snapshot lost target: %+v", e.Activity)
		}
		if e.Activity.AssignedUser == nil || e.Activity.AssignedUser.Username == "" {
			t.Fatalf("reassignment snapshot lost username: %+v", e.Activity)
		}
		return
	}
	t.Fatal("timeline missing reassignment entry")
}

// 6. Timeline is newest-first and paginates without duplicates or gaps.
func TestTimelineOrderingAndPagination(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	w := seedWorld(t, false)
	item := w.createItem(t, repo)

	// created=1, claim=2, reassign=3, advance=4, close=5, reopen=6
	if _, err := repo.ClaimActivity(context.Background(), w.orgID, item.ID, w.adminID, 1, true); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := repo.ReassignActivity(context.Background(), w.orgID, item.ID, w.adminID, w.otherUser, 2, true); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	if _, err := repo.AdvanceStage(context.Background(), w.orgID, item.ID, w.adminID, 3, true); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if _, err := repo.SetClosed(context.Background(), w.orgID, item.ID, w.adminID, true, 4, true); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := repo.SetClosed(context.Background(), w.orgID, item.ID, w.adminID, false, 5, true); err != nil {
		t.Fatalf("reopen: %v", err)
	}

	const limit = 2
	var (
		got      []int64
		before   *int64
		firstItr = true
	)
	for {
		page, err := repo.Timeline(context.Background(), w.orgID, item.ID, before, limit)
		if err != nil {
			t.Fatalf("timeline: %v", err)
		}
		for _, e := range page.Entries {
			got = append(got, e.Version)
		}
		if !firstItr {
			// pages must not overlap
			if len(page.Entries) > 0 && page.Entries[0].Version >= got[0] {
				t.Fatalf("page overlaps previous page")
			}
		}
		firstItr = false
		if page.NextBeforeVersion == nil {
			break
		}
		before = page.NextBeforeVersion
	}

	want := []int64{6, 5, 4, 3, 2, 1}
	if len(got) != len(want) {
		t.Fatalf("expected versions %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected versions %v, got %v", want, got)
		}
	}
}

// 7. Cross-org isolation: no timeline entries and no cross-org references.
func TestCrossOrgIsolation(t *testing.T) {
	resetDB(t)
	repo := NewItemRepo(testDB)
	a := seedWorld(t, false)
	itemA := a.createItem(t, repo)

	orgB := seedOrg(t, "org-b")
	adminB := seedUser(t, orgB, "admin-b", "admin")

	page, err := repo.Timeline(context.Background(), orgB, itemA.ID, nil, 50)
	if err != nil {
		t.Fatalf("timeline orgB: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("orgB must not see orgA timeline, got %d entries", len(page.Entries))
	}

	// orgB cannot claim orgA's activity.
	if _, err := repo.ClaimActivity(context.Background(), orgB, itemA.ID, adminB, 1, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound cross-org claim, got %v", err)
	}

	// orgB cannot reassign orgA's item to an orgB user (item invisible).
	if _, err := repo.ReassignActivity(context.Background(), orgB, itemA.ID, adminB, adminB, 1, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound cross-org reassign, got %v", err)
	}

	// orgA timeline is unchanged.
	pageA, err := repo.Timeline(context.Background(), a.orgID, itemA.ID, nil, 50)
	if err != nil {
		t.Fatalf("timeline orgA: %v", err)
	}
	if len(pageA.Entries) != 1 {
		t.Fatalf("expected 1 orgA timeline entry, got %d", len(pageA.Entries))
	}
}

// 8. Every mutation type rejects a stale version and writes nothing.
func TestStaleVersionAllMutations(t *testing.T) {
	ctx := context.Background()

	t.Run("edit", func(t *testing.T) {
		resetDB(t)
		repo := NewItemRepo(testDB)
		w := seedWorld(t, false)
		item := w.createItem(t, repo)
		before := len(changesVersions(t, w.orgID, item.ID))

		_, err := repo.Update(ctx, w.orgID, item.ID, w.adminID, dto.ItemRequest{
			Title: "changed", Priority: "high", CurrentStageID: &w.stageOne, Version: 999,
		}, true)
		if !errors.Is(err, ErrStale) {
			t.Fatalf("expected ErrStale, got %v", err)
		}
		if got := len(changesVersions(t, w.orgID, item.ID)); got != before {
			t.Fatalf("stale edit wrote a change row (%d -> %d)", before, got)
		}
	})

	t.Run("assign", func(t *testing.T) {
		resetDB(t)
		repo := NewItemRepo(testDB)
		w := seedWorld(t, false)
		item := w.createItem(t, repo)
		before := len(changesVersions(t, w.orgID, item.ID))

		_, err := repo.ClaimActivity(ctx, w.orgID, item.ID, w.adminID, 999, true)
		if !errors.Is(err, ErrStale) {
			t.Fatalf("expected ErrStale, got %v", err)
		}
		if got := len(changesVersions(t, w.orgID, item.ID)); got != before {
			t.Fatalf("stale assign wrote a change row")
		}
	})

	t.Run("complete", func(t *testing.T) {
		resetDB(t)
		repo := NewItemRepo(testDB)
		w := seedWorld(t, false)
		item := w.createItem(t, repo)
		if _, err := repo.ClaimActivity(ctx, w.orgID, item.ID, w.adminID, 1, true); err != nil {
			t.Fatalf("claim: %v", err)
		}
		before := len(changesVersions(t, w.orgID, item.ID))

		_, err := repo.CompleteActivity(ctx, w.orgID, item.ID, w.adminID, "done", 999)
		if !errors.Is(err, ErrStale) {
			t.Fatalf("expected ErrStale, got %v", err)
		}
		if got := len(changesVersions(t, w.orgID, item.ID)); got != before {
			t.Fatalf("stale complete wrote a change row")
		}
	})

	t.Run("approve", func(t *testing.T) {
		resetDB(t)
		repo := NewItemRepo(testDB)
		w := seedWorld(t, true)
		item := w.createItem(t, repo)
		if _, err := repo.ClaimActivity(ctx, w.orgID, item.ID, w.adminID, 1, true); err != nil {
			t.Fatalf("claim: %v", err)
		}
		if _, err := repo.CompleteActivity(ctx, w.orgID, item.ID, w.adminID, "done", 2); err != nil {
			t.Fatalf("complete: %v", err)
		}
		before := len(changesVersions(t, w.orgID, item.ID))

		_, err := repo.ApproveActivity(ctx, w.orgID, item.ID, w.adminID, true, "", 999, true)
		if !errors.Is(err, ErrStale) {
			t.Fatalf("expected ErrStale, got %v", err)
		}
		if got := len(changesVersions(t, w.orgID, item.ID)); got != before {
			t.Fatalf("stale approve wrote a change row")
		}
	})

	t.Run("stage_move", func(t *testing.T) {
		resetDB(t)
		repo := NewItemRepo(testDB)
		w := seedWorld(t, false)
		item := w.createItem(t, repo)
		before := len(changesVersions(t, w.orgID, item.ID))

		_, err := repo.AdvanceStage(ctx, w.orgID, item.ID, w.adminID, 999, true)
		if !errors.Is(err, ErrStale) {
			t.Fatalf("expected ErrStale, got %v", err)
		}
		if got := len(changesVersions(t, w.orgID, item.ID)); got != before {
			t.Fatalf("stale stage move wrote a change row")
		}
	})
}
