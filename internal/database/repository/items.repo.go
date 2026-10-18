package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ScaleOfCoherence/internal/api/dto"
)

type ItemRepo struct {
	db *sql.DB
}

func NewItemRepo(db *sql.DB) *ItemRepo {
	return &ItemRepo{db: db}
}

// stageWorkflow looks up workflow_id for a given stage; returns (nil, nil, nil) when stageID is nil/empty.
func (r *ItemRepo) stageWorkflow(ctx context.Context, tx *sql.Tx, orgID string, stageID *string) (*string, *string, error) {
	if stageID == nil || *stageID == "" {
		return nil, nil, nil
	}
	var sid, wid string
	q := `SELECT id::text, workflow_id::text FROM workflow_stages WHERE org_id = $1::uuid AND id = $2::uuid`
	var row *sql.Row
	if tx != nil {
		row = tx.QueryRowContext(ctx, q, orgID, *stageID)
	} else {
		row = r.db.QueryRowContext(ctx, q, orgID, *stageID)
	}
	err := row.Scan(&sid, &wid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	return &sid, &wid, err
}

// itemCols is the SELECT column list that matches scanItem.
// NOTE: no table alias — use directly against the items table.
const itemCols = `id, title, description, priority, current_stage_id, workflow_id, version, custom_fields, created_by, created_at, updated_at, closed_at`

// itemColsAliased is itemCols with explicit "i." alias for JOIN queries.
const itemColsAliased = `i.id, i.title, i.description, i.priority, i.current_stage_id, i.workflow_id, i.version, i.custom_fields, i.created_by, i.created_at, i.updated_at, i.closed_at`

func scanItem(row interface{ Scan(...any) error }) (dto.Item, error) {
	var i dto.Item
	var cf []byte
	err := row.Scan(
		&i.ID, &i.Title, &i.Description, &i.Priority,
		&i.CurrentStageID, &i.WorkflowID,
		&i.Version, &cf,
		&i.CreatedBy, &i.CreatedAt, &i.UpdatedAt, &i.ClosedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, ErrNotFound
	}
	if err != nil {
		return dto.Item{}, err
	}
	if len(cf) > 0 {
		i.CustomFields = json.RawMessage(cf)
	}
	return i, nil
}

func (r *ItemRepo) Create(ctx context.Context, orgID, actorID string, req dto.ItemRequest) (dto.Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Item{}, err
	}
	defer tx.Rollback()

	cf := req.CustomFields
	if len(cf) == 0 {
		cf = json.RawMessage("{}")
	}

	// Resolve stage / workflow.
	newStageID, newWorkflowID, err := r.stageWorkflow(ctx, tx, orgID, req.CurrentStageID)
	if err != nil {
		return dto.Item{}, err
	}

	// closed_at on create: only set if caller explicitly passes close=true.
	var closedAt *time.Time
	if req.Close != nil && *req.Close {
		now := time.Now()
		closedAt = &now
	}

	item, err := scanItem(tx.QueryRowContext(ctx, `
		INSERT INTO items (org_id, title, description, priority, current_stage_id, workflow_id, custom_fields, created_by, closed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+itemCols,
		orgID, req.Title, req.Description, req.Priority,
		newStageID, newWorkflowID, []byte(cf), actorID, closedAt,
	))
	if err != nil {
		return dto.Item{}, err
	}

	// Record the initial version in item_changes (before any activity insert).
	if err := insertChange(ctx, tx, orgID, item.ID, item.Version, actorID,
		map[string]any{"type": "created"}); err != nil {
		return dto.Item{}, err
	}

	// If the item starts on a stage, open an unclaimed activity row.
	if newStageID != nil {
		actID, err := r.openActivity(ctx, tx, orgID, item.ID, *newStageID, actorID, item.Version)
		if err != nil {
			return dto.Item{}, err
		}
		if err := mergeChangeKeys(ctx, tx, orgID, item.ID, item.Version,
			map[string]any{"activity_id": actID}); err != nil {
			return dto.Item{}, err
		}
	}

	return item, tx.Commit()
}

// priorityRank orders priorities most urgent first for the given column.
func priorityRank(col string) string {
	return fmt.Sprintf(`CASE %s WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'medium' THEN 2 ELSE 3 END`, col)
}

// itemSorts maps accepted sort keys to their ORDER BY expressions.
// Unknown keys fall back to created_at.
var itemSorts = map[string]string{
	"created_at": "items.created_at",
	"updated_at": "items.updated_at",
	"title":      "lower(items.title)",
	"priority":   priorityRank("items.priority"),
	"status":     "CASE WHEN items.closed_at IS NULL THEN 0 ELSE 1 END",
	"version":    "items.version",
}

// List returns a filtered page of items. Sort and order are whitelisted; all
// other values are passed as query parameters.
func (r *ItemRepo) List(ctx context.Context, orgID string, limit, offset int, f dto.ItemFilter) (dto.Page[dto.Item], error) {
	page := dto.Page[dto.Item]{Data: []dto.Item{}}

	args := []any{orgID}
	ph := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	conds := []string{"items.org_id = $1"}

	if f.Search != "" {
		p := ph("%" + escapeLike(f.Search) + "%")
		conds = append(conds, fmt.Sprintf(
			`(items.title ILIKE %s ESCAPE '\' OR items.description ILIKE %s ESCAPE '\')`, p, p))
	}
	if f.Priority != "" {
		conds = append(conds, "items.priority = "+ph(f.Priority))
	}
	switch f.Status {
	case "open":
		conds = append(conds, "items.closed_at IS NULL")
	case "closed":
		conds = append(conds, "items.closed_at IS NOT NULL")
	}
	if f.WorkflowID != "" {
		conds = append(conds, "items.workflow_id::text = "+ph(f.WorkflowID))
	}
	if f.StageID != "" {
		conds = append(conds, "items.current_stage_id::text = "+ph(f.StageID))
	}
	switch f.AssignedTo {
	case "":
	case "unassigned":
		conds = append(conds, `NOT EXISTS (
			SELECT 1 FROM item_stage_activity a
			WHERE a.org_id = items.org_id AND a.item_id = items.id
			  AND a.released_at IS NULL AND a.assigned_user_id IS NOT NULL)`)
	default:
		conds = append(conds, `EXISTS (
			SELECT 1 FROM item_stage_activity a
			WHERE a.org_id = items.org_id AND a.item_id = items.id
			  AND a.released_at IS NULL AND a.assigned_user_id::text = `+ph(f.AssignedTo)+`)`)
	}

	where := strings.Join(conds, " AND ")
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM items WHERE `+where, args...).Scan(&page.Total); err != nil {
		return page, err
	}

	// id tiebreaker keeps pagination stable for equal sort keys.
	sortSQL := orderBy(itemSorts, "created_at", f.Sort, f.Order) + ", items.id ASC"

	listArgs := append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE `+where+` ORDER BY `+sortSQL+
			fmt.Sprintf(` LIMIT $%d OFFSET $%d`, len(listArgs)-1, len(listArgs)),
		listArgs...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			return page, err
		}
		page.Data = append(page.Data, i)
	}
	return page, rows.Err()
}

func (r *ItemRepo) Get(ctx context.Context, orgID, id string) (dto.Item, error) {
	return scanItem(r.db.QueryRowContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE org_id = $1 AND id = $2`, orgID, id))
}

func (r *ItemRepo) Update(ctx context.Context, orgID, id, actorID string, req dto.ItemRequest) (dto.Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Item{}, err
	}
	defer tx.Rollback()

	// ── 1. Read current state (used for the diff; version is enforced below) ──
	cur, err := scanItem(tx.QueryRowContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE org_id = $1 AND id = $2`, orgID, id))
	if err != nil {
		return dto.Item{}, err
	}
	if cur.Version != req.Version {
		return dto.Item{}, StaleError{CurrentVersion: cur.Version}
	}

	// ── 2. Resolve new stage / workflow ─────────────────────────────────────
	newStageID, newWorkflowID, err := r.stageWorkflow(ctx, tx, orgID, req.CurrentStageID)
	if err != nil {
		return dto.Item{}, err
	}

	cf := req.CustomFields
	if len(cf) == 0 {
		cf = json.RawMessage("{}")
	}

	// ── 3. Resolve closed_at ─────────────────────────────────────────────────
	// req.Close == nil  → leave unchanged
	// req.Close == true → set to now() if not already closed
	// req.Close == false → clear (reopen)
	newClosedAt := cur.ClosedAt
	if req.Close != nil {
		if *req.Close && cur.ClosedAt == nil {
			now := time.Now()
			newClosedAt = &now
		} else if !*req.Close {
			newClosedAt = nil
		}
	}

	// ── 4. Version-checked update: one bump, exactly one new version ────────
	newVersion := cur.Version + 1
	item, err := scanItem(tx.QueryRowContext(ctx, `
		UPDATE items
		SET title = $3, description = $4, priority = $5,
		    current_stage_id = $6, workflow_id = $7,
		    custom_fields = $8,
		    version = version + 1, updated_at = now(),
		    closed_at = $9
		WHERE org_id = $1 AND id = $2 AND version = $10
		RETURNING `+itemCols,
		orgID, id,
		req.Title, req.Description, req.Priority,
		newStageID, newWorkflowID,
		[]byte(cf),
		newClosedAt, req.Version,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, r.staleOrNotFound(ctx, tx, orgID, id)
	}
	if err != nil {
		return dto.Item{}, err
	}

	// ── 5. Build diff payload (fields as [old, new]) ────────────────────────
	stageChanged := !ptrEq(cur.CurrentStageID, newStageID)
	changes := map[string]any{}
	if cur.Title != req.Title {
		changes["title"] = []any{cur.Title, req.Title}
	}
	if cur.Description != req.Description {
		changes["description"] = []any{cur.Description, req.Description}
	}
	if cur.Priority != req.Priority {
		changes["priority"] = []any{cur.Priority, req.Priority}
	}
	if !ptrEq(cur.WorkflowID, newWorkflowID) {
		changes["workflow_id"] = []any{cur.WorkflowID, newWorkflowID}
	}
	if !jsonEq(cur.CustomFields, cf) {
		changes["custom_fields"] = []any{normalizeJSON(cur.CustomFields), normalizeJSON(cf)}
	}
	if !timePtrEq(cur.ClosedAt, newClosedAt) {
		changes["closed_at"] = []any{cur.ClosedAt, newClosedAt}
	}
	if stageChanged {
		changes["type"] = "stage_moved"
		changes["from"] = cur.CurrentStageID
		changes["to"] = newStageID
	} else {
		changes["type"] = "updated"
	}

	// ── 6. Insert item_changes at the new version ───────────────────────────
	if err := insertChange(ctx, tx, orgID, id, newVersion, actorID, changes); err != nil {
		return dto.Item{}, err
	}

	// ── 7. Handle stage activity (after the change row exists) ──────────────
	if stageChanged {
		extra := map[string]any{}
		if cid, err := r.closeOpenActivity(ctx, tx, orgID, id, "stage_changed"); err != nil {
			return dto.Item{}, err
		} else if cid != 0 {
			extra["closed_activity_id"] = cid
		}
		if newStageID != nil {
			nid, err := r.openActivity(ctx, tx, orgID, id, *newStageID, actorID, newVersion)
			if err != nil {
				return dto.Item{}, err
			}
			extra["activity_id"] = nid
		}
		if len(extra) > 0 {
			if err := mergeChangeKeys(ctx, tx, orgID, id, newVersion, extra); err != nil {
				return dto.Item{}, err
			}
		}
	}

	return item, tx.Commit()
}

func (r *ItemRepo) Delete(ctx context.Context, orgID, id string) error {
	return affected(r.db.ExecContext(ctx, `DELETE FROM items WHERE org_id = $1 AND id = $2`, orgID, id))
}

// SetClosed closes or reopens an item. Callers must be an org admin or a leader
// of the team that owns the item's current stage.
func (r *ItemRepo) SetClosed(ctx context.Context, orgID, itemID, callerID string, closed bool, version int64, callerIsAdmin bool) (dto.Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Item{}, err
	}
	defer tx.Rollback()

	cur, err := scanItem(tx.QueryRowContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE org_id = $1 AND id = $2`, orgID, itemID))
	if err != nil {
		return dto.Item{}, err
	}
	if cur.Version != version {
		return dto.Item{}, StaleError{CurrentVersion: cur.Version}
	}

	if !callerIsAdmin {
		ok, err := r.isStageTeamLeader(ctx, tx, orgID, cur.CurrentStageID, callerID)
		if err != nil {
			return dto.Item{}, err
		}
		if !ok {
			return dto.Item{}, ErrForbidden
		}
	}

	newClosedAt := cur.ClosedAt
	if closed && cur.ClosedAt == nil {
		now := time.Now()
		newClosedAt = &now
	} else if !closed {
		newClosedAt = nil
	}

	newVersion := cur.Version + 1
	item, err := scanItem(tx.QueryRowContext(ctx, `
		UPDATE items
		SET version = version + 1, updated_at = now(), closed_at = $3
		WHERE org_id = $1 AND id = $2 AND version = $4
		RETURNING `+itemCols,
		orgID, itemID, newClosedAt, version))
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, r.staleOrNotFound(ctx, tx, orgID, itemID)
	}
	if err != nil {
		return dto.Item{}, err
	}

	if err := insertChange(ctx, tx, orgID, itemID, newVersion, callerID,
		map[string]any{"type": "closed", "closed": closed}); err != nil {
		return dto.Item{}, err
	}

	return item, tx.Commit()
}

// AdvanceStage moves an item to the next stage (by position) in its workflow.
// Callers must be an org admin or a leader of the team that owns the current
// stage. Returns ErrNoNextStage when the item is not on a stage or is already
// on the last stage.
func (r *ItemRepo) AdvanceStage(ctx context.Context, orgID, itemID, callerID string, version int64, callerIsAdmin bool) (dto.Item, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Item{}, err
	}
	defer tx.Rollback()

	var curStageID *string
	if err := tx.QueryRowContext(ctx,
		`SELECT current_stage_id FROM items WHERE org_id = $1 AND id = $2`,
		orgID, itemID).Scan(&curStageID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return dto.Item{}, ErrNotFound
		}
		return dto.Item{}, err
	}
	if curStageID == nil {
		return dto.Item{}, ErrNoNextStage
	}

	if !callerIsAdmin {
		ok, err := r.isStageTeamLeader(ctx, tx, orgID, curStageID, callerID)
		if err != nil {
			return dto.Item{}, err
		}
		if !ok {
			return dto.Item{}, ErrForbidden
		}
	}

	// Locate the current stage's position and owning workflow.
	var curPosition int
	var workflowID string
	if err := tx.QueryRowContext(ctx,
		`SELECT position, workflow_id FROM workflow_stages WHERE org_id = $1 AND id = $2`,
		orgID, *curStageID).Scan(&curPosition, &workflowID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return dto.Item{}, ErrNoNextStage
		}
		return dto.Item{}, err
	}

	// Find the next stage by position within the same workflow.
	var nextStageID string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM workflow_stages
		WHERE org_id = $1 AND workflow_id = $2 AND position > $3
		ORDER BY position ASC LIMIT 1`,
		orgID, workflowID, curPosition).Scan(&nextStageID)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, ErrNoNextStage
	}
	if err != nil {
		return dto.Item{}, err
	}

	// ── Version bump first, then log the change, then move the activity ─────
	newVersion, err := r.bumpVersion(ctx, tx, orgID, itemID, version)
	if err != nil {
		return dto.Item{}, err
	}

	if err := insertChange(ctx, tx, orgID, itemID, newVersion, callerID, map[string]any{
		"type": "stage_moved",
		"from": curStageID,
		"to":   nextStageID,
	}); err != nil {
		return dto.Item{}, err
	}

	cu, err := tx.ExecContext(ctx, `
		UPDATE items
		SET current_stage_id = $3, workflow_id = $4, updated_at = now()
		WHERE org_id = $1 AND id = $2 AND version = $5`,
		orgID, itemID, nextStageID, workflowID, newVersion)
	if err != nil {
		return dto.Item{}, err
	}
	if n, _ := cu.RowsAffected(); n == 0 {
		return dto.Item{}, r.staleOrNotFound(ctx, tx, orgID, itemID)
	}

	extra := map[string]any{}
	if cid, err := r.closeOpenActivity(ctx, tx, orgID, itemID, "stage_changed"); err != nil {
		return dto.Item{}, err
	} else if cid != 0 {
		extra["closed_activity_id"] = cid
	}
	nid, err := r.openActivity(ctx, tx, orgID, itemID, nextStageID, callerID, newVersion)
	if err != nil {
		return dto.Item{}, err
	}
	extra["activity_id"] = nid
	if err := mergeChangeKeys(ctx, tx, orgID, itemID, newVersion, extra); err != nil {
		return dto.Item{}, err
	}

	item, err := scanItem(tx.QueryRowContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE org_id = $1 AND id = $2`, orgID, itemID))
	if err != nil {
		return dto.Item{}, err
	}
	return item, tx.Commit()
}

// Timeline returns one page of the merged item timeline, newest first.
// beforeVersion, when non-nil, returns only entries with a lower version.
func (r *ItemRepo) Timeline(ctx context.Context, orgID, itemID string, beforeVersion *int64, limit int) (dto.TimelinePage, error) {
	page := dto.TimelinePage{Entries: []dto.TimelineEntry{}}

	rows, err := r.db.QueryContext(ctx, `
		SELECT c.version, c.created_at, c.actor_id, COALESCE(au.username, ''),
		       c.changes,
		       a.id, a.stage_id, a.team_id, COALESCE(t.name, ''),
		       a.assigned_user_id, COALESCE(u.username, ''),
		       a.completion_note, a.approval_status, a.approval_note
		FROM item_changes c
		JOIN users au ON au.id = c.actor_id
		LEFT JOIN item_stage_activity a
		       ON a.id = (c.changes->>'activity_id')::bigint
		      AND a.org_id = c.org_id
		LEFT JOIN users u ON u.id = a.assigned_user_id
		LEFT JOIN teams t ON t.org_id = a.org_id AND t.id = a.team_id
		WHERE c.org_id = $1 AND c.item_id = $2
		  AND ($3::bigint IS NULL OR c.version < $3)
		ORDER BY c.version DESC
		LIMIT $4`,
		orgID, itemID, beforeVersion, limit)
	if err != nil {
		return page, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			e              dto.TimelineEntry
			actorName      string
			changes        []byte
			actID          sql.NullInt64
			stageID        sql.NullString
			teamID         sql.NullString
			teamName       sql.NullString
			assignedUserID sql.NullString
			assignedName   sql.NullString
			completionNote sql.NullString
			approvalStatus sql.NullString
			approvalNote   sql.NullString
		)
		if err := rows.Scan(
			&e.Version, &e.CreatedAt, &e.Actor.ID, &actorName,
			&changes,
			&actID, &stageID, &teamID, &teamName,
			&assignedUserID, &assignedName,
			&completionNote, &approvalStatus, &approvalNote,
		); err != nil {
			return page, err
		}
		e.Actor.Username = actorName
		e.Changes = json.RawMessage(changes)
		var meta struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(changes, &meta)
		e.Type = meta.Type

		if actID.Valid {
			act := &dto.TimelineActivity{
				ID:       actID.Int64,
				StageID:  stageID.String,
				TeamID:   teamID.String,
				TeamName: teamName.String,
			}
			if assignedUserID.Valid {
				id := assignedUserID.String
				act.AssignedUserID = &id
				act.AssignedUser = &dto.TimelineUserRef{ID: id, Username: assignedName.String}
			}
			if completionNote.Valid {
				n := completionNote.String
				act.CompletionNote = &n
			}
			if approvalStatus.Valid {
				s := approvalStatus.String
				act.ApprovalStatus = &s
			}
			if approvalNote.Valid {
				n := approvalNote.String
				act.ApprovalNote = &n
			}
			e.Activity = act
		}
		page.Entries = append(page.Entries, e)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}

	if len(page.Entries) == limit && limit > 0 {
		last := page.Entries[len(page.Entries)-1].Version
		page.NextBeforeVersion = &last
	}
	return page, nil
}

// GetOpenActivity returns the single open (unreleased) activity for itemID, or ErrNotFound.
func (r *ItemRepo) GetOpenActivity(ctx context.Context, orgID, itemID string) (dto.ItemStageActivity, error) {
	act, err := scanActivity(r.db.QueryRowContext(ctx,
		activityCols+`WHERE org_id = $1 AND item_id = $2 AND released_at IS NULL`,
		orgID, itemID))
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ItemStageActivity{}, ErrNotFound
	}
	return act, err
}

// ListMy returns a paginated page of items where the caller has an open or pending activity.
func (r *ItemRepo) ListMy(ctx context.Context, orgID, userID string, limit, offset int) (dto.Page[dto.Item], error) {
	page := dto.Page[dto.Item]{Data: []dto.Item{}}
	if err := r.db.QueryRowContext(ctx, `
		SELECT count(DISTINCT i.id)
		FROM items i
		JOIN item_stage_activity a ON a.item_id = i.id
		WHERE i.org_id = $1
		  AND a.assigned_user_id = $2::uuid
		  AND a.released_at IS NULL`, orgID, userID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+itemColsAliased+`
		FROM items i
		JOIN item_stage_activity a ON a.item_id = i.id
		WHERE i.org_id = $1
		  AND a.assigned_user_id = $2::uuid
		  AND a.released_at IS NULL
		GROUP BY i.id, i.title, i.description, i.priority, i.current_stage_id,
		         i.workflow_id, i.version, i.custom_fields, i.created_by,
		         i.created_at, i.updated_at, i.closed_at
		ORDER BY i.created_at DESC
		LIMIT $3 OFFSET $4`, orgID, userID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		i, err := scanItem(rows)
		if err != nil {
			return page, err
		}
		page.Data = append(page.Data, i)
	}
	return page, rows.Err()
}

// Attention returns the caller's open work: items they hold an open activity
// for, plus items whose pending approval names them as the stage approver.
// Approvals come first; an item that is both is listed once as an approval.
func (r *ItemRepo) Attention(ctx context.Context, orgID, userID string, limit int) (dto.Attention, error) {
	res := dto.Attention{Items: []dto.AttentionItem{}}

	if err := r.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM item_stage_activity a
		JOIN items i ON i.org_id = a.org_id AND i.id = a.item_id
		WHERE a.org_id = $1 AND a.released_at IS NULL
		  AND a.assigned_user_id = $2::uuid AND i.closed_at IS NULL`,
		orgID, userID).Scan(&res.AssignedTotal); err != nil {
		return res, err
	}
	if err := r.db.QueryRowContext(ctx, `
		SELECT count(*)
		FROM item_stage_activity a
		JOIN workflow_stages s ON s.org_id = a.org_id AND s.id = a.stage_id
		JOIN items i ON i.org_id = a.org_id AND i.id = a.item_id
		WHERE a.org_id = $1 AND a.released_at IS NULL AND i.closed_at IS NULL
		  AND a.approval_status = 'pending' AND s.approver_user_id = $2::uuid`,
		orgID, userID).Scan(&res.ApprovalTotal); err != nil {
		return res, err
	}

	appendRows := func(rows *sql.Rows, reason string) error {
		defer rows.Close()
		for rows.Next() {
			var it dto.AttentionItem
			if err := rows.Scan(&it.ID, &it.Title, &it.Priority, &it.StageName); err != nil {
				return err
			}
			it.Reason = reason
			res.Items = append(res.Items, it)
		}
		return rows.Err()
	}

	approvals, err := r.db.QueryContext(ctx, `
		SELECT i.id, i.title, i.priority, s.name
		FROM item_stage_activity a
		JOIN workflow_stages s ON s.org_id = a.org_id AND s.id = a.stage_id
		JOIN items i ON i.org_id = a.org_id AND i.id = a.item_id
		WHERE a.org_id = $1 AND a.released_at IS NULL AND i.closed_at IS NULL
		  AND a.approval_status = 'pending' AND s.approver_user_id = $2::uuid
		ORDER BY `+priorityRank("i.priority")+`, a.assigned_at ASC
		LIMIT $3`, orgID, userID, limit)
	if err != nil {
		return res, err
	}
	if err := appendRows(approvals, "approval"); err != nil {
		return res, err
	}

	assigned, err := r.db.QueryContext(ctx, `
		SELECT i.id, i.title, i.priority, s.name
		FROM item_stage_activity a
		JOIN workflow_stages s ON s.org_id = a.org_id AND s.id = a.stage_id
		JOIN items i ON i.org_id = a.org_id AND i.id = a.item_id
		WHERE a.org_id = $1 AND a.released_at IS NULL AND i.closed_at IS NULL
		  AND a.assigned_user_id = $2::uuid
		ORDER BY `+priorityRank("i.priority")+`, a.assigned_at ASC
		LIMIT $3`, orgID, userID, limit)
	if err != nil {
		return res, err
	}
	if err := appendRows(assigned, "assigned"); err != nil {
		return res, err
	}

	// Approvals already occupy the front of the list; drop items that are both
	// assigned to and pending approval from the caller.
	out := res.Items[:0]
	seen := map[string]bool{}
	for _, it := range res.Items {
		if seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		if len(out) < limit {
			out = append(out, it)
		}
	}
	res.Items = out
	return res, nil
}

// ── Activity mutation methods ──────────────────────────────────────────────────

// ClaimActivity lets a team member (editor or leader) or an org admin
// self-assign the open unclaimed activity for itemID. Returns ErrNotFound if
// there is no open unclaimed activity, ErrDuplicate if it is already claimed,
// ErrForbidden if the caller is neither a team editor/leader nor an org admin.
func (r *ItemRepo) ClaimActivity(ctx context.Context, orgID, itemID, callerID string, version int64, callerIsAdmin bool) (dto.ItemStageActivity, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	defer tx.Rollback()

	// Load & lock the open activity.
	var actID int64
	var teamID string
	err = tx.QueryRowContext(ctx, `
		SELECT id, team_id
		FROM item_stage_activity
		WHERE org_id = $1 AND item_id = $2
		  AND released_at IS NULL AND completed_at IS NULL
		FOR UPDATE`,
		orgID, itemID).Scan(&actID, &teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ItemStageActivity{}, ErrNotFound
	}
	if err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Verify caller is an editor or leader in the team (org admins bypass).
	if !callerIsAdmin {
		var role string
		err = tx.QueryRowContext(ctx,
			`SELECT role FROM team_members WHERE org_id = $1 AND team_id = $2 AND user_id = $3`,
			orgID, teamID, callerID).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return dto.ItemStageActivity{}, ErrForbidden
		}
		if err != nil {
			return dto.ItemStageActivity{}, err
		}
		if role != "editor" && role != "leader" {
			return dto.ItemStageActivity{}, ErrForbidden
		}
	}

	// ── One bump, one change row, then the assignment update ────────────────
	newVersion, err := r.bumpVersion(ctx, tx, orgID, itemID, version)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	if err := insertChange(ctx, tx, orgID, itemID, newVersion, callerID, map[string]any{
		"type":        "assigned",
		"activity_id": actID,
		"from":        nil,
		"to":          callerID,
	}); err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Attempt the claim — only succeeds if currently unassigned.
	res, err := tx.ExecContext(ctx, `
		UPDATE item_stage_activity
		SET assigned_user_id = $1, assigned_by = $2, assigned_at = now()
		WHERE id = $3 AND assigned_user_id IS NULL`,
		callerID, callerID, actID)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return dto.ItemStageActivity{}, ConflictError{Message: "activity already claimed"}
	}

	act, err := scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// CompleteActivity marks the open activity as complete. If the stage requires
// approval it is set to pending instead of being released immediately.
func (r *ItemRepo) CompleteActivity(ctx context.Context, orgID, itemID, callerID, note string, version int64) (dto.ItemStageActivity, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	defer tx.Rollback()

	// Load & lock the open activity.
	var actID int64
	var stageID string
	var assignedUser *string
	err = tx.QueryRowContext(ctx, `
		SELECT id, stage_id, assigned_user_id
		FROM item_stage_activity
		WHERE org_id = $1 AND item_id = $2
		  AND released_at IS NULL AND completed_at IS NULL
		FOR UPDATE`,
		orgID, itemID).Scan(&actID, &stageID, &assignedUser)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ItemStageActivity{}, ErrNotFound
	}
	if err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Only the assigned user may complete.
	if assignedUser == nil || *assignedUser != callerID {
		return dto.ItemStageActivity{}, ErrForbidden
	}

	// Check whether the stage requires approval.
	var approvalRequired bool
	if err := tx.QueryRowContext(ctx,
		`SELECT approval_required FROM workflow_stages WHERE org_id = $1 AND id = $2`,
		orgID, stageID).Scan(&approvalRequired); err != nil {
		return dto.ItemStageActivity{}, err
	}

	newVersion, err := r.bumpVersion(ctx, tx, orgID, itemID, version)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	if err := insertChange(ctx, tx, orgID, itemID, newVersion, callerID, map[string]any{
		"type":        "completed",
		"activity_id": actID,
	}); err != nil {
		return dto.ItemStageActivity{}, err
	}

	var act dto.ItemStageActivity
	if approvalRequired {
		// Set to pending — do NOT set completed_at yet to avoid constraint violation.
		if _, err := tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET completed_by    = $1,
			    completion_note = $2,
			    approval_status = 'pending'
			WHERE id = $3`,
			callerID, note, actID); err != nil {
			return dto.ItemStageActivity{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET completed_by    = $1,
			    completion_note = $2,
			    completed_at    = now(),
			    released_at     = now(),
			    release_reason  = 'completed'
			WHERE id = $3`,
			callerID, note, actID); err != nil {
			return dto.ItemStageActivity{}, err
		}
	}
	act, err = scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// ApproveActivity approves or rejects a pending activity. Callers must be the
// stage approver, a team leader, or an org admin. On rejection the row stays
// open (unclaimed) so the team can re-claim and redo the work.
func (r *ItemRepo) ApproveActivity(ctx context.Context, orgID, itemID, callerID string, approved bool, note string, version int64, callerIsAdmin bool) (dto.ItemStageActivity, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	defer tx.Rollback()

	// Load & lock the pending activity.
	var actID int64
	var stageID, teamID string
	err = tx.QueryRowContext(ctx, `
		SELECT id, stage_id, team_id
		FROM item_stage_activity
		WHERE org_id = $1 AND item_id = $2
		  AND approval_status = 'pending'
		  AND released_at IS NULL
		FOR UPDATE`,
		orgID, itemID).Scan(&actID, &stageID, &teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ItemStageActivity{}, ErrNotFound
	}
	if err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Verify caller: org admin, stage approver, or team leader.
	if !callerIsAdmin && !r.isApproverOrLeader(ctx, tx, orgID, stageID, teamID, callerID) {
		return dto.ItemStageActivity{}, ErrForbidden
	}

	newVersion, err := r.bumpVersion(ctx, tx, orgID, itemID, version)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	changeType := "rejected"
	if approved {
		changeType = "approved"
	}
	if err := insertChange(ctx, tx, orgID, itemID, newVersion, callerID, map[string]any{
		"type":        changeType,
		"activity_id": actID,
	}); err != nil {
		return dto.ItemStageActivity{}, err
	}

	if approved {
		_, err = tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET approval_status = 'approved',
			    approved_by     = $1,
			    approved_at     = now(),
			    approval_note   = $2,
			    completed_at    = now(),
			    released_at     = now(),
			    release_reason  = 'completed'
			WHERE id = $3`,
			callerID, note, actID)
	} else {
		// Reject: keep open but clear assignment so team can re-claim.
		// Also clear completed_by/completion_note since the work is rejected.
		_, err = tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET approval_status  = 'rejected',
			    approved_by      = $1,
			    approved_at      = now(),
			    approval_note    = $2,
			    assigned_user_id = NULL,
			    completed_by     = NULL,
			    completion_note  = NULL
			WHERE id = $3`,
			callerID, note, actID)
	}
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	act, err := scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// ReassignActivity releases the current open activity and opens a new one
// assigned to targetUserID. Caller must be an org admin or a team leader.
func (r *ItemRepo) ReassignActivity(ctx context.Context, orgID, itemID, callerID, targetUserID string, version int64, callerIsAdmin bool) (dto.ItemStageActivity, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	defer tx.Rollback()

	// Load & lock the open activity.
	var actID int64
	var stageID, teamID string
	var curAssignee *string
	err = tx.QueryRowContext(ctx, `
		SELECT id, stage_id, team_id, assigned_user_id
		FROM item_stage_activity
		WHERE org_id = $1 AND item_id = $2
		  AND released_at IS NULL AND completed_at IS NULL
		FOR UPDATE`,
		orgID, itemID).Scan(&actID, &stageID, &teamID, &curAssignee)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ItemStageActivity{}, ErrNotFound
	}
	if err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Permission: must be org admin or team leader.
	if !callerIsAdmin {
		var role string
		err = tx.QueryRowContext(ctx,
			`SELECT role FROM team_members WHERE org_id = $1 AND team_id = $2 AND user_id = $3`,
			orgID, teamID, callerID).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) || role != "leader" {
			return dto.ItemStageActivity{}, ErrForbidden
		}
		if err != nil {
			return dto.ItemStageActivity{}, err
		}
	}

	// Verify target user: org admins may assign to any org member; team leaders
	// may only assign to members of the team that owns the stage.
	if callerIsAdmin {
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM organization_members WHERE org_id = $1 AND user_id = $2)`,
			orgID, targetUserID).Scan(&exists); err != nil {
			return dto.ItemStageActivity{}, err
		}
		if !exists {
			return dto.ItemStageActivity{}, ErrNotFound // target not in org
		}
	} else {
		var targetRole string
		err = tx.QueryRowContext(ctx,
			`SELECT role FROM team_members WHERE org_id = $1 AND team_id = $2 AND user_id = $3`,
			orgID, teamID, targetUserID).Scan(&targetRole)
		if errors.Is(err, sql.ErrNoRows) {
			return dto.ItemStageActivity{}, ErrNotFound // target not in team
		}
		if err != nil {
			return dto.ItemStageActivity{}, err
		}
	}

	// ── One bump, one change row, then swap the activity rows ───────────────
	newVersion, err := r.bumpVersion(ctx, tx, orgID, itemID, version)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	if err := insertChange(ctx, tx, orgID, itemID, newVersion, callerID, map[string]any{
		"type": "assigned",
		"from": curAssignee,
		"to":   targetUserID,
	}); err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Close the old row before inserting the new one (partial unique index).
	if _, err := tx.ExecContext(ctx, `
		UPDATE item_stage_activity
		SET released_at = now(), release_reason = 'reassigned'
		WHERE id = $1`, actID); err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Insert new row for the target user at the new version.
	var newActID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO item_stage_activity
		    (org_id, item_id, stage_id, team_id, assigned_user_id, assigned_by, item_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		orgID, itemID, stageID, teamID, targetUserID, callerID, newVersion,
	).Scan(&newActID); err != nil {
		if isUniqueViolation(err) {
			return dto.ItemStageActivity{}, ConflictError{Message: "activity version conflict"}
		}
		if isSerializationFailure(err) {
			return dto.ItemStageActivity{}, ConflictError{CurrentVersion: newVersion, Message: "stale item_version"}
		}
		return dto.ItemStageActivity{}, err
	}
	if err := mergeChangeKeys(ctx, tx, orgID, itemID, newVersion,
		map[string]any{"activity_id": newActID}); err != nil {
		return dto.ItemStageActivity{}, err
	}

	act, err := scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", newActID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// ── internal helpers ──────────────────────────────────────────────────────────

// bumpVersion performs the version-checked UPDATE that starts every mutation.
// It returns StaleError with the current version when 0 rows match, or
// ErrNotFound when the item does not exist.
func (r *ItemRepo) bumpVersion(ctx context.Context, tx *sql.Tx, orgID, itemID string, expected int64) (int64, error) {
	var newVersion int64
	err := tx.QueryRowContext(ctx, `
		UPDATE items
		SET version = version + 1, updated_at = now()
		WHERE org_id = $1::uuid AND id = $2::uuid AND version = $3
		RETURNING version`, orgID, itemID, expected).Scan(&newVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, r.staleOrNotFound(ctx, tx, orgID, itemID)
	}
	if err != nil {
		return 0, err
	}
	return newVersion, nil
}

// staleOrNotFound reports the current version when the item exists, otherwise
// ErrNotFound. Safe to call after a 0-row (non-error) update.
func (r *ItemRepo) staleOrNotFound(ctx context.Context, tx *sql.Tx, orgID, itemID string) error {
	var current int64
	err := tx.QueryRowContext(ctx,
		`SELECT version FROM items WHERE org_id = $1::uuid AND id = $2::uuid`,
		orgID, itemID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return StaleError{CurrentVersion: current}
}

// insertChange writes exactly one item_changes row at the given version.
func insertChange(ctx context.Context, tx *sql.Tx, orgID, itemID string, version int64, actorID string, changes any) error {
	b, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO item_changes (org_id, item_id, version, actor_id, changes)
		VALUES ($1, $2, $3, $4, $5)`,
		orgID, itemID, version, actorID, b)
	if isUniqueViolation(err) {
		return ConflictError{Message: "version already logged"}
	}
	return err
}

// mergeChangeKeys shallow-merges keys into the changes JSONB of one change row.
// Used to record activity ids learned after the activity rows are written.
func mergeChangeKeys(ctx context.Context, tx *sql.Tx, orgID, itemID string, version int64, extra map[string]any) error {
	b, err := json.Marshal(extra)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE item_changes
		SET changes = changes || $4::jsonb
		WHERE org_id = $1 AND item_id = $2 AND version = $3`,
		orgID, itemID, version, b)
	return err
}

// openActivity inserts an unclaimed (assigned_user_id = NULL) activity row for
// the given stage. It looks up the assigned_team_id from workflow_stages.
func (r *ItemRepo) openActivity(ctx context.Context, tx *sql.Tx, orgID, itemID, stageID, actorID string, version int64) (int64, error) {
	var teamID string
	err := tx.QueryRowContext(ctx,
		`SELECT assigned_team_id::text FROM workflow_stages WHERE org_id = $1 AND id = $2`,
		orgID, stageID).Scan(&teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}

	var actID int64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO item_stage_activity
		    (org_id, item_id, stage_id, team_id, assigned_by, item_version)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		orgID, itemID, stageID, teamID, actorID, version).Scan(&actID)
	if isUniqueViolation(err) {
		return 0, ConflictError{Message: "activity version conflict"}
	}
	if isSerializationFailure(err) {
		return 0, ConflictError{CurrentVersion: version, Message: "stale item_version"}
	}
	return actID, err
}

// closeOpenActivity releases the item's open activity with the given reason and
// returns its id (0 when there was none).
func (r *ItemRepo) closeOpenActivity(ctx context.Context, tx *sql.Tx, orgID, itemID, reason string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		UPDATE item_stage_activity
		SET released_at = now(), release_reason = $3
		WHERE org_id = $1 AND item_id = $2 AND released_at IS NULL
		RETURNING id`,
		orgID, itemID, reason).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// isApproverOrLeader checks whether callerID is the stage approver or a team leader.
func (r *ItemRepo) isApproverOrLeader(ctx context.Context, tx *sql.Tx, orgID, stageID, teamID, callerID string) bool {
	// Check stage approver.
	var approverID *string
	tx.QueryRowContext(ctx,
		`SELECT approver_user_id::text FROM workflow_stages WHERE org_id = $1 AND id = $2`,
		orgID, stageID).Scan(&approverID)
	if approverID != nil && *approverID == callerID {
		return true
	}

	// Check team leader.
	var role string
	tx.QueryRowContext(ctx,
		`SELECT role FROM team_members WHERE org_id = $1 AND team_id = $2 AND user_id = $3`,
		orgID, teamID, callerID).Scan(&role)
	return role == "leader"
}

// isStageTeamLeader reports whether callerID is a leader of the team assigned to
// the given stage. Returns false for an unset stage or a stage without a team.
func (r *ItemRepo) isStageTeamLeader(ctx context.Context, tx *sql.Tx, orgID string, stageID *string, callerID string) (bool, error) {
	if stageID == nil || *stageID == "" {
		return false, nil
	}
	var role string
	err := tx.QueryRowContext(ctx, `
		SELECT tm.role
		FROM workflow_stages ws
		JOIN team_members tm ON tm.org_id = ws.org_id AND tm.team_id = ws.assigned_team_id
		WHERE ws.org_id = $1 AND ws.id = $2 AND tm.user_id = $3`,
		orgID, *stageID, callerID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return role == "leader", nil
}

// activityCols is the SELECT prefix used when loading a single activity row by id.
const activityCols = `
	SELECT id, item_id, stage_id, team_id,
	       assigned_user_id, assigned_by, assigned_at, item_version,
	       completed_by, completion_note, completed_at,
	       released_at, release_reason,
	       approval_status, approved_by, approved_at, approval_note
	FROM item_stage_activity `

func scanActivity(row interface{ Scan(...any) error }) (dto.ItemStageActivity, error) {
	var a dto.ItemStageActivity
	err := row.Scan(
		&a.ID, &a.ItemID, &a.StageID, &a.TeamID,
		&a.AssignedUserID, &a.AssignedBy, &a.AssignedAt, &a.ItemVersion,
		&a.CompletedBy, &a.CompletionNote, &a.CompletedAt,
		&a.ReleasedAt, &a.ReleaseReason,
		&a.ApprovalStatus, &a.ApprovedBy, &a.ApprovedAt, &a.ApprovalNote,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.ItemStageActivity{}, ErrNotFound
	}
	return a, err
}

// ── shared helpers ──────────────────────────────────────────────────────────────

func ptrEq(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func timePtrEq(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return json.RawMessage(b)
}

func jsonEq(a, b json.RawMessage) bool {
	return string(normalizeJSON(a)) == string(normalizeJSON(b))
}
