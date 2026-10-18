package repository

import (
	"context"
	"database/sql"
	"errors"

	"ScaleOfCoherence/internal/api/dto"
)

type WorkflowRepo struct {
	db *sql.DB
}

func NewWorkflowRepo(db *sql.DB) *WorkflowRepo {
	return &WorkflowRepo{db: db}
}

func (r *WorkflowRepo) Create(ctx context.Context, orgID, userID string, req dto.WorkflowRequest) (dto.Workflow, error) {
	w := dto.Workflow{Name: req.Name, Description: req.Description}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO workflows (org_id, created_by, name, description) VALUES ($1, $2, $3, $4) RETURNING id, created_at`,
		orgID, userID, req.Name, req.Description).Scan(&w.ID, &w.CreatedAt)
	if isUniqueViolation(err) {
		return dto.Workflow{}, ErrDuplicate
	}
	return w, err
}

func (r *WorkflowRepo) List(ctx context.Context, orgID string, limit, offset int) (dto.Page[dto.Workflow], error) {
	page := dto.Page[dto.Workflow]{Data: []dto.Workflow{}}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM workflows WHERE org_id = $1`, orgID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, description, created_at FROM workflows
		WHERE org_id = $1 ORDER BY name LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var w dto.Workflow
		if err := rows.Scan(&w.ID, &w.Name, &w.Description, &w.CreatedAt); err != nil {
			return page, err
		}
		page.Data = append(page.Data, w)
	}
	return page, rows.Err()
}

func (r *WorkflowRepo) Get(ctx context.Context, orgID, id string) (dto.Workflow, error) {
	var w dto.Workflow
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, description, created_at FROM workflows WHERE org_id = $1 AND id = $2`, orgID, id).
		Scan(&w.ID, &w.Name, &w.Description, &w.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Workflow{}, ErrNotFound
	}
	return w, err
}

func (r *WorkflowRepo) Update(ctx context.Context, orgID, id string, req dto.WorkflowRequest) (dto.Workflow, error) {
	w := dto.Workflow{ID: id, Name: req.Name, Description: req.Description}
	err := r.db.QueryRowContext(ctx, `
		UPDATE workflows SET name = $3, description = $4, updated_at = now()
		WHERE org_id = $1 AND id = $2 RETURNING created_at`,
		orgID, id, req.Name, req.Description).Scan(&w.CreatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return dto.Workflow{}, ErrNotFound
	case isUniqueViolation(err):
		return dto.Workflow{}, ErrDuplicate
	}
	return w, err
}

func (r *WorkflowRepo) Delete(ctx context.Context, orgID, id string) error {
	err := affected(r.db.ExecContext(ctx, `DELETE FROM workflows WHERE org_id = $1 AND id = $2`, orgID, id))
	if isForeignKeyViolation(err) {
		return ErrInUse
	}
	return err
}

const stageCols = `id, workflow_id, name, description, assigned_team_id, position, completion_requirements, approval_required, approver_user_id, created_at, updated_at`

func scanStage(row interface{ Scan(...any) error }) (dto.Stage, error) {
	var s dto.Stage
	err := row.Scan(&s.ID, &s.WorkflowID, &s.Name, &s.Description, &s.AssignedTeamID, &s.Position,
		&s.CompletionRequirements, &s.ApprovalRequired, &s.ApproverUserID, &s.CreatedAt, &s.UpdatedAt)
	return s, err
}

func (r *WorkflowRepo) ListStages(ctx context.Context, orgID, workflowID string) ([]dto.Stage, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+stageCols+` FROM workflow_stages WHERE org_id = $1 AND workflow_id = $2 ORDER BY position`, orgID, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stages := []dto.Stage{}
	for rows.Next() {
		s, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		stages = append(stages, s)
	}
	return stages, rows.Err()
}

func (r *WorkflowRepo) GetStage(ctx context.Context, orgID, workflowID, id string) (dto.Stage, error) {
	s, err := scanStage(r.db.QueryRowContext(ctx,
		`SELECT `+stageCols+` FROM workflow_stages WHERE org_id = $1 AND workflow_id = $2 AND id = $3`, orgID, workflowID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Stage{}, ErrNotFound
	}
	return s, err
}

// CreateStage inserts at the requested position (or the end), shifting later stages down.
func (r *WorkflowRepo) CreateStage(ctx context.Context, orgID, workflowID string, req dto.StageRequest) (dto.Stage, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Stage{}, err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM workflow_stages WHERE workflow_id = $1`, workflowID).Scan(&count); err != nil {
		return dto.Stage{}, err
	}
	pos := clamp(req.Position, count+1)
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_stages SET position = position + 1 WHERE workflow_id = $1 AND position >= $2`, workflowID, pos); err != nil {
		return dto.Stage{}, err
	}
	s, err := scanStage(tx.QueryRowContext(ctx, `
		INSERT INTO workflow_stages (org_id, workflow_id, assigned_team_id, name, description, position, completion_requirements, approval_required, approver_user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+stageCols,
		orgID, workflowID, req.AssignedTeamID, req.Name, req.Description, pos, []byte(req.CompletionRequirements), req.ApprovalRequired, req.ApproverUserID))
	if err := stageErr(err); err != nil {
		return dto.Stage{}, err
	}
	return s, tx.Commit()
}

// UpdateStage edits a stage and moves it to the requested position, shifting the stages in between.
func (r *WorkflowRepo) UpdateStage(ctx context.Context, orgID, workflowID, id string, req dto.StageRequest) (dto.Stage, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Stage{}, err
	}
	defer tx.Rollback()
	var old, count int
	err = tx.QueryRowContext(ctx, `
		SELECT position, (SELECT count(*) FROM workflow_stages WHERE workflow_id = $1)
		FROM workflow_stages WHERE org_id = $2 AND workflow_id = $1 AND id = $3`, workflowID, orgID, id).Scan(&old, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Stage{}, ErrNotFound
	}
	if err != nil {
		return dto.Stage{}, err
	}
	pos := old
	if req.Position > 0 {
		pos = clamp(req.Position, count)
	}
	if pos < old {
		_, err = tx.ExecContext(ctx, `UPDATE workflow_stages SET position = position + 1 WHERE workflow_id = $1 AND position >= $2 AND position < $3`, workflowID, pos, old)
	} else if pos > old {
		_, err = tx.ExecContext(ctx, `UPDATE workflow_stages SET position = position - 1 WHERE workflow_id = $1 AND position > $2 AND position <= $3`, workflowID, old, pos)
	}
	if err != nil {
		return dto.Stage{}, err
	}
	s, err := scanStage(tx.QueryRowContext(ctx, `
		UPDATE workflow_stages SET assigned_team_id = $4, name = $5, description = $6, position = $7,
			completion_requirements = $8, approval_required = $9, approver_user_id = $10, updated_at = now()
		WHERE org_id = $1 AND workflow_id = $2 AND id = $3 RETURNING `+stageCols,
		orgID, workflowID, id, req.AssignedTeamID, req.Name, req.Description, pos, []byte(req.CompletionRequirements), req.ApprovalRequired, req.ApproverUserID))
	if err := stageErr(err); err != nil {
		return dto.Stage{}, err
	}
	return s, tx.Commit()
}

// DeleteStage removes a stage and closes the gap in positions.
func (r *WorkflowRepo) DeleteStage(ctx context.Context, orgID, workflowID, id string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pos int
	err = tx.QueryRowContext(ctx,
		`DELETE FROM workflow_stages WHERE org_id = $1 AND workflow_id = $2 AND id = $3 RETURNING position`, orgID, workflowID, id).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if isForeignKeyViolation(err) {
		return ErrInUse
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workflow_stages SET position = position - 1 WHERE workflow_id = $1 AND position > $2`, workflowID, pos); err != nil {
		return err
	}
	return tx.Commit()
}

func stageErr(err error) error {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case isUniqueViolation(err):
		return ErrDuplicate
	case isForeignKeyViolation(err):
		return ErrNotFound
	}
	return err
}

func clamp(pos, max int) int {
	if pos < 1 || pos > max {
		return max
	}
	return pos
}
