package repository

import (
	"context"
	"database/sql"
	"errors"

	"ScaleOfCoherence/internal/api/dto"
)

type TeamRepo struct {
	db *sql.DB
}

func NewTeamRepo(db *sql.DB) *TeamRepo {
	return &TeamRepo{db: db}
}

func (r *TeamRepo) Create(ctx context.Context, orgID, name string) (dto.Team, error) {
	t := dto.Team{Name: name}
	err := r.db.QueryRowContext(ctx, `INSERT INTO teams (org_id, name) VALUES ($1, $2) RETURNING id, created_at`, orgID, name).
		Scan(&t.ID, &t.CreatedAt)
	if isUniqueViolation(err) {
		return dto.Team{}, ErrDuplicate
	}
	return t, err
}

func (r *TeamRepo) List(ctx context.Context, orgID string, limit, offset int) (dto.Page[dto.Team], error) {
	page := dto.Page[dto.Team]{Data: []dto.Team{}}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM teams WHERE org_id = $1`, orgID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, created_at FROM teams WHERE org_id = $1 ORDER BY name LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var t dto.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.CreatedAt); err != nil {
			return page, err
		}
		page.Data = append(page.Data, t)
	}
	return page, rows.Err()
}

func (r *TeamRepo) Get(ctx context.Context, orgID, id string) (dto.Team, error) {
	var t dto.Team
	err := r.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM teams WHERE org_id = $1 AND id = $2`, orgID, id).
		Scan(&t.ID, &t.Name, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Team{}, ErrNotFound
	}
	return t, err
}

func (r *TeamRepo) Update(ctx context.Context, orgID, id, name string) (dto.Team, error) {
	t := dto.Team{ID: id}
	err := r.db.QueryRowContext(ctx,
		`UPDATE teams SET name = $3, updated_at = now() WHERE org_id = $1 AND id = $2 RETURNING name, created_at`,
		orgID, id, name).Scan(&t.Name, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Team{}, ErrNotFound
	}
	if isUniqueViolation(err) {
		return dto.Team{}, ErrDuplicate
	}
	return t, err
}

func (r *TeamRepo) Delete(ctx context.Context, orgID, id string) error {
	return affected(r.db.ExecContext(ctx, `DELETE FROM teams WHERE org_id = $1 AND id = $2`, orgID, id))
}

func (r *TeamRepo) ListMembers(ctx context.Context, orgID, teamID string, limit, offset int) (dto.Page[dto.TeamMember], error) {
	page := dto.Page[dto.TeamMember]{Data: []dto.TeamMember{}}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM team_members WHERE org_id = $1 AND team_id = $2`, orgID, teamID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT tm.user_id, u.username, tm.role
		FROM team_members tm JOIN users u ON u.id = tm.user_id
		WHERE tm.org_id = $1 AND tm.team_id = $2 ORDER BY u.username LIMIT $3 OFFSET $4`, orgID, teamID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var m dto.TeamMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.Role); err != nil {
			return page, err
		}
		page.Data = append(page.Data, m)
	}
	return page, rows.Err()
}

// GetMemberRole returns the role of userID in the given team, or ErrNotFound if
// they are not a member.
func (r *TeamRepo) GetMemberRole(ctx context.Context, orgID, teamID, userID string) (string, error) {
	var role string
	err := r.db.QueryRowContext(ctx,
		`SELECT role FROM team_members WHERE org_id = $1 AND team_id = $2 AND user_id = $3`,
		orgID, teamID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

// AddMember fails with ErrNotFound if the team or user is not in the org.
func (r *TeamRepo) AddMember(ctx context.Context, orgID, teamID, userID, role string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO team_members (org_id, team_id, user_id, role) VALUES ($1, $2, $3, $4)`,
		orgID, teamID, userID, role)
	switch {
	case isUniqueViolation(err):
		return ErrDuplicate
	case isForeignKeyViolation(err):
		return ErrNotFound
	}
	return err
}

func (r *TeamRepo) RemoveMember(ctx context.Context, orgID, teamID, userID string) error {
	return affected(r.db.ExecContext(ctx,
		`DELETE FROM team_members WHERE org_id = $1 AND team_id = $2 AND user_id = $3`, orgID, teamID, userID))
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
