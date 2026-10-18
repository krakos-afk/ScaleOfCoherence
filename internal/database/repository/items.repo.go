package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

	// Record the initial version in item_changes.
	diff := buildDiff(nil, newStageID, req)
	diffJSON, err := json.Marshal(diff)
	if err != nil {
		return dto.Item{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO item_changes (org_id, item_id, version, actor_id, changes)
		VALUES ($1, $2, $3, $4, $5)`,
		orgID, item.ID, item.Version, actorID, diffJSON,
	); err != nil {
		return dto.Item{}, err
	}

	// If the item starts on a stage, open an unclaimed activity row.
	if newStageID != nil {
		if err := r.openActivity(ctx, tx, orgID, item.ID, *newStageID, actorID, item.Version); err != nil {
			return dto.Item{}, err
		}
	}

	return item, tx.Commit()
}

func (r *ItemRepo) List(ctx context.Context, orgID string, limit, offset int) (dto.Page[dto.Item], error) {
	page := dto.Page[dto.Item]{Data: []dto.Item{}}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE org_id = $1`, orgID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM items WHERE org_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		orgID, limit, offset)
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

	// ── 1. Read current state under lock ────────────────────────────────────
	var curVersion int64
	var curStageID *string
	var curClosedAt *time.Time
	err = tx.QueryRowContext(ctx,
		`SELECT version, current_stage_id, closed_at FROM items WHERE org_id = $1 AND id = $2 FOR UPDATE`,
		orgID, id).Scan(&curVersion, &curStageID, &curClosedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, ErrNotFound
	}
	if err != nil {
		return dto.Item{}, err
	}
	if curVersion != req.Version {
		return dto.Item{}, ErrStale
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
	newClosedAt := curClosedAt
	if req.Close != nil {
		if *req.Close && curClosedAt == nil {
			now := time.Now()
			newClosedAt = &now
		} else if !*req.Close {
			newClosedAt = nil
		}
	}

	// ── 4. Update item (version + 1) ────────────────────────────────────────
	newVersion := curVersion + 1
	item, err := scanItem(tx.QueryRowContext(ctx, `
		UPDATE items
		SET title = $3, description = $4, priority = $5,
		    current_stage_id = $6, workflow_id = $7,
		    custom_fields = $8,
		    version = $9, updated_at = now(),
		    closed_at = $10
		WHERE org_id = $1 AND id = $2
		RETURNING `+itemCols,
		orgID, id,
		req.Title, req.Description, req.Priority,
		newStageID, newWorkflowID,
		[]byte(cf),
		newVersion,
		newClosedAt,
	))
	if err != nil {
		return dto.Item{}, err
	}

	// ── 5. Build diff payload ────────────────────────────────────────────────
	diff := buildDiff(curStageID, newStageID, req)
	if req.Close != nil {
		diff["closed"] = *req.Close
	}
	diffJSON, err := json.Marshal(diff)
	if err != nil {
		return dto.Item{}, err
	}

	// ── 6. Insert item_changes at new version ───────────────────────────────
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO item_changes (org_id, item_id, version, actor_id, changes)
		VALUES ($1, $2, $3, $4, $5)`,
		orgID, id, newVersion, actorID, diffJSON,
	); err != nil {
		return dto.Item{}, err
	}

	// ── 7. Handle stage activity ────────────────────────────────────────────
	stageChanged := !ptrEq(curStageID, newStageID)
	if stageChanged {
		// Release the open activity (if any) with reason "stage_changed".
		if _, err := tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET released_at = now(), release_reason = 'stage_changed'
			WHERE item_id = $1 AND released_at IS NULL`,
			id,
		); err != nil {
			return dto.Item{}, err
		}

		// Open a new unclaimed activity row for the new stage (if any).
		if newStageID != nil {
			if err := r.openActivity(ctx, tx, orgID, id, *newStageID, actorID, newVersion); err != nil {
				return dto.Item{}, err
			}
		}
	}

	return item, tx.Commit()
}

func (r *ItemRepo) Delete(ctx context.Context, orgID, id string) error {
	return affected(r.db.ExecContext(ctx, `DELETE FROM items WHERE org_id = $1 AND id = $2`, orgID, id))
}

// ListChanges returns the change history for an item, newest first.
func (r *ItemRepo) ListChanges(ctx context.Context, orgID, itemID string, limit, offset int) (dto.Page[dto.ItemChange], error) {
	page := dto.Page[dto.ItemChange]{Data: []dto.ItemChange{}}
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM item_changes WHERE org_id = $1 AND item_id = $2`, orgID, itemID,
	).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, item_id, version, actor_id, changes, created_at
		FROM item_changes
		WHERE org_id = $1 AND item_id = $2
		ORDER BY version DESC
		LIMIT $3 OFFSET $4`,
		orgID, itemID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var c dto.ItemChange
		var ch []byte
		if err := rows.Scan(&c.ID, &c.ItemID, &c.Version, &c.ActorID, &ch, &c.CreatedAt); err != nil {
			return page, err
		}
		c.Changes = json.RawMessage(ch)
		page.Data = append(page.Data, c)
	}
	return page, rows.Err()
}

// ListActivity returns the stage activity log for an item, newest first.
func (r *ItemRepo) ListActivity(ctx context.Context, orgID, itemID string, limit, offset int) (dto.Page[dto.ItemStageActivity], error) {
	page := dto.Page[dto.ItemStageActivity]{Data: []dto.ItemStageActivity{}}
	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM item_stage_activity WHERE org_id = $1 AND item_id = $2`, orgID, itemID,
	).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, item_id, stage_id, team_id,
		       assigned_user_id, assigned_by, assigned_at, item_version,
		       completed_by, completion_note, completed_at,
		       released_at, release_reason,
		       approval_status, approved_by, approved_at, approval_note
		FROM item_stage_activity
		WHERE org_id = $1 AND item_id = $2
		ORDER BY assigned_at DESC
		LIMIT $3 OFFSET $4`,
		orgID, itemID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scanActivity(rows)
		if err != nil {
			return page, err
		}
		page.Data = append(page.Data, a)
	}
	return page, rows.Err()
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

// ── Activity mutation methods ──────────────────────────────────────────────────

// ClaimActivity lets a team member (editor or leader) self-assign the open
// unclaimed activity for itemID. Returns ErrNotFound if there is no open
// unclaimed activity, ErrDuplicate if it is already claimed.
func (r *ItemRepo) ClaimActivity(ctx context.Context, orgID, itemID, callerID string) (dto.ItemStageActivity, error) {
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

	// Verify caller is an editor or leader in the team.
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

	// Attempt the claim — only succeeds if currently unassigned.
	res, err := tx.ExecContext(ctx, `
		UPDATE item_stage_activity
		SET assigned_user_id = $1
		WHERE id = $2 AND assigned_user_id IS NULL`,
		callerID, actID)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return dto.ItemStageActivity{}, ErrDuplicate // already claimed
	}

	act, err := scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// CompleteActivity marks the open activity as complete. If the stage requires
// approval it is set to pending instead of being released immediately.
func (r *ItemRepo) CompleteActivity(ctx context.Context, orgID, itemID, callerID, note string) (dto.ItemStageActivity, error) {
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

	var act dto.ItemStageActivity
	if approvalRequired {
		// Set to pending — do NOT set completed_at yet to avoid constraint violation.
		// The completed_at/released_at pair is only set together on approval.
		_, err2 := tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET completed_by    = $1,
			    completion_note = $2,
			    approval_status = 'pending'
			WHERE id = $3`,
			callerID, note, actID)
		if err2 != nil {
			return dto.ItemStageActivity{}, err2
		}
		// Re-read for accurate response.
		act, err = scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	} else {
		// Release immediately.
		_, err = tx.ExecContext(ctx, `
			UPDATE item_stage_activity
			SET completed_by    = $1,
			    completion_note = $2,
			    completed_at    = now(),
			    released_at     = now(),
			    release_reason  = 'completed'
			WHERE id = $3`,
			callerID, note, actID)
		if err != nil {
			return dto.ItemStageActivity{}, err
		}
		act, err = scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	}
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// ApproveActivity approves or rejects a pending activity. Callers must be the
// stage approver or a team leader. On rejection the row stays open (unclaimed)
// so the team can re-claim and redo the work.
func (r *ItemRepo) ApproveActivity(ctx context.Context, orgID, itemID, callerID string, approved bool, note string) (dto.ItemStageActivity, error) {
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

	// Verify caller: stage approver OR team leader.
	if !r.isApproverOrLeader(ctx, tx, orgID, stageID, teamID, callerID) {
		return dto.ItemStageActivity{}, ErrForbidden
	}

	var act dto.ItemStageActivity
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
	act, err = scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", actID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// ReassignActivity releases the current open activity and opens a new one
// assigned to targetUserID. Caller must be an org admin or a team leader.
func (r *ItemRepo) ReassignActivity(ctx context.Context, orgID, itemID, callerID, targetUserID string, callerIsAdmin bool) (dto.ItemStageActivity, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	defer tx.Rollback()

	// Load & lock the open activity.
	var actID int64
	var stageID, teamID string
	var curVersion int64
	err = tx.QueryRowContext(ctx, `
		SELECT id, stage_id, team_id, item_version
		FROM item_stage_activity
		WHERE org_id = $1 AND item_id = $2
		  AND released_at IS NULL AND completed_at IS NULL
		FOR UPDATE`,
		orgID, itemID).Scan(&actID, &stageID, &teamID, &curVersion)
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

	// Verify target user is a member of the team.
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

	// Release current row.
	if _, err := tx.ExecContext(ctx, `
		UPDATE item_stage_activity
		SET released_at = now(), release_reason = 'reassigned'
		WHERE id = $1`, actID); err != nil {
		return dto.ItemStageActivity{}, err
	}

	// Insert new row for the target user.
	var newActID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO item_stage_activity
		    (org_id, item_id, stage_id, team_id, assigned_user_id, assigned_by, item_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id`,
		orgID, itemID, stageID, teamID, targetUserID, callerID, curVersion,
	).Scan(&newActID); err != nil {
		if isUniqueViolation(err) {
			return dto.ItemStageActivity{}, ErrDuplicate
		}
		return dto.ItemStageActivity{}, err
	}

	act, err := scanActivity(tx.QueryRowContext(ctx, activityCols+"WHERE id = $1", newActID))
	if err != nil {
		return dto.ItemStageActivity{}, err
	}
	return act, tx.Commit()
}

// ── internal helpers ──────────────────────────────────────────────────────────

// openActivity inserts an unclaimed (assigned_user_id = NULL) activity row for
// the given stage. It looks up the assigned_team_id from workflow_stages.
func (r *ItemRepo) openActivity(ctx context.Context, tx *sql.Tx, orgID, itemID, stageID, actorID string, version int64) error {
	var teamID string
	err := tx.QueryRowContext(ctx,
		`SELECT assigned_team_id::text FROM workflow_stages WHERE org_id = $1 AND id = $2`,
		orgID, stageID).Scan(&teamID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO item_stage_activity
		    (org_id, item_id, stage_id, team_id, assigned_by, item_version)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		orgID, itemID, stageID, teamID, actorID, version)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
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

// buildDiff constructs the JSONB diff recorded in item_changes.
// It always includes every field so the record is self-contained.
func buildDiff(oldStageID, newStageID *string, req dto.ItemRequest) map[string]any {
	d := map[string]any{
		"title":       req.Title,
		"description": req.Description,
		"priority":    req.Priority,
	}
	if oldStageID != nil {
		d["old_stage_id"] = *oldStageID
	} else {
		d["old_stage_id"] = nil
	}
	if newStageID != nil {
		d["new_stage_id"] = *newStageID
	} else {
		d["new_stage_id"] = nil
	}
	if req.WorkflowID != nil {
		d["workflow_id"] = *req.WorkflowID
	} else {
		d["workflow_id"] = nil
	}
	return d
}
