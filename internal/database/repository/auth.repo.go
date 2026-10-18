package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"ScaleOfCoherence/internal/api/dto"
)

type AuthRepo struct {
	db *sql.DB
}

func NewAuthRepo(db *sql.DB) *AuthRepo {
	return &AuthRepo{db: db}
}

func (r *AuthRepo) CreateOrgWithAdmin(ctx context.Context, orgName, email, username, hash string) (dto.AuthUser, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.AuthUser{}, err
	}
	defer tx.Rollback()

	u := dto.AuthUser{PasswordHash: hash, Usertype: "admin"}
	if err := tx.QueryRowContext(ctx, `INSERT INTO organization (name) VALUES ($1) RETURNING id`, orgName).Scan(&u.OrgID); err != nil {
		return dto.AuthUser{}, err
	}
	if err := insertMember(ctx, tx, &u, email, username); err != nil {
		return dto.AuthUser{}, err
	}
	return u, tx.Commit()
}

func (r *AuthRepo) CreateMember(ctx context.Context, orgID, email, username, hash, usertype string) (dto.AuthUser, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.AuthUser{}, err
	}
	defer tx.Rollback()

	u := dto.AuthUser{PasswordHash: hash, OrgID: orgID, Usertype: usertype}
	if err := insertMember(ctx, tx, &u, email, username); err != nil {
		return dto.AuthUser{}, err
	}
	return u, tx.Commit()
}

func (r *AuthRepo) GetByEmail(ctx context.Context, email string) (dto.AuthUser, error) {
	var u dto.AuthUser
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, m.org_id, m.usertype
		FROM users u JOIN organization_members m ON m.user_id = u.id
		WHERE lower(u.email) = lower($1)
		ORDER BY m.created_at LIMIT 1`, email).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.OrgID, &u.Usertype)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.AuthUser{}, ErrNotFound
	}
	return u, err
}

var orgUserSorts = map[string]string{
	"username": "lower(u.username)",
	"usertype": "om.usertype",
}

func (r *AuthRepo) ListUsers(ctx context.Context, orgID string, p dto.ListParams) ([]dto.OrgUser, error) {
	args := []any{orgID}
	where := "om.org_id = $1"
	if p.Search != "" {
		args = append(args, "%"+escapeLike(p.Search)+"%")
		where += fmt.Sprintf(` AND u.username ILIKE $%d ESCAPE '\'`, len(args))
	}
	order := orderBy(orgUserSorts, "username", p.Sort, p.Order)
	if p.Sort == "usertype" {
		order += ", lower(u.username)" // deterministic order within a role
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.username, om.usertype FROM organization_members om
		JOIN users u ON u.id = om.user_id WHERE `+where+
		` ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []dto.OrgUser{}
	for rows.Next() {
		var u dto.OrgUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Usertype); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ListUserNames returns id/username pairs for every member of the org. It is
// safe for any org member to read (no emails, no usertypes) and is always
// scoped by the caller's org.
func (r *AuthRepo) ListUserNames(ctx context.Context, orgID string) ([]dto.UserName, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT u.id, u.username FROM organization_members om
		JOIN users u ON u.id = om.user_id WHERE om.org_id = $1 ORDER BY u.username`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []dto.UserName{}
	for rows.Next() {
		var u dto.UserName
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		names = append(names, u)
	}
	return names, rows.Err()
}

// DeleteUser removes a user only if they belong to the given org.
func (r *AuthRepo) DeleteUser(ctx context.Context, orgID, userID string) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM users WHERE id = $1
		AND EXISTS (SELECT 1 FROM organization_members WHERE user_id = $1 AND org_id = $2)`, userID, orgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func insertMember(ctx context.Context, tx *sql.Tx, u *dto.AuthUser, email, username string) error {
	err := tx.QueryRowContext(ctx,
		`INSERT INTO users (email, username, password_hash) VALUES ($1, $2, $3) RETURNING id`,
		email, username, u.PasswordHash).Scan(&u.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	u.Username = username
	_, err = tx.ExecContext(ctx,
		`INSERT INTO organization_members (org_id, user_id, usertype) VALUES ($1, $2, $3)`,
		u.OrgID, u.ID, u.Usertype)
	return err
}
