package repository

import (
	"context"
	"database/sql"
	"errors"

	"ScaleOfCoherence/internal/api/dto"
)

// CommentRepo stores item comments. Comments never modify the item; they are
// anchored to the item version that was current when they were created.
type CommentRepo struct {
	db *sql.DB
}

func NewCommentRepo(db *sql.DB) *CommentRepo {
	return &CommentRepo{db: db}
}

const commentCols = `c.id, c.item_id, c.author_id, u.username, c.version, c.body, c.created_at`

// List returns one page of comments for an item, newest first.
// Readable by any org member (same visibility as the timeline).
func (r *CommentRepo) List(ctx context.Context, orgID, itemID string, limit, offset int) (dto.Page[dto.Comment], error) {
	page := dto.Page[dto.Comment]{Data: []dto.Comment{}}

	if err := r.db.QueryRowContext(ctx,
		`SELECT count(*) FROM item_comments WHERE org_id = $1 AND item_id = $2`,
		orgID, itemID).Scan(&page.Total); err != nil {
		return page, err
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT `+commentCols+`
		FROM item_comments c
		JOIN users u ON u.id = c.author_id
		WHERE c.org_id = $1 AND c.item_id = $2
		ORDER BY c.created_at DESC, c.id DESC
		LIMIT $3 OFFSET $4`,
		orgID, itemID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var c dto.Comment
		if err := rows.Scan(&c.ID, &c.ItemID, &c.AuthorID, &c.Author, &c.Version, &c.Body, &c.CreatedAt); err != nil {
			return page, err
		}
		page.Data = append(page.Data, c)
	}
	return page, rows.Err()
}

// Create adds a comment anchored to the item's current version. The version is
// read from items in the same statement as the insert, so it always references
// a logged item change.
//
// Permissions: org admins may always comment; other callers must hold the
// editor, commentator, or leader role in the team responsible for the item's
// current stage.
func (r *CommentRepo) Create(ctx context.Context, orgID, itemID, authorID, body string, callerIsAdmin bool) (dto.Comment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dto.Comment{}, err
	}
	defer tx.Rollback()

	var stageID *string
	err = tx.QueryRowContext(ctx,
		`SELECT current_stage_id FROM items WHERE org_id = $1 AND id = $2`,
		orgID, itemID).Scan(&stageID)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Comment{}, ErrNotFound
	}
	if err != nil {
		return dto.Comment{}, err
	}

	if !callerIsAdmin {
		if stageID == nil {
			return dto.Comment{}, ErrForbidden
		}
		var role string
		err := tx.QueryRowContext(ctx, `
			SELECT tm.role FROM workflow_stages s
			JOIN team_members tm
			  ON tm.org_id = s.org_id AND tm.team_id = s.assigned_team_id
			WHERE s.org_id = $1 AND s.id = $2 AND tm.user_id = $3`,
			orgID, *stageID, authorID).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return dto.Comment{}, ErrForbidden
		}
		if err != nil {
			return dto.Comment{}, err
		}
		if role != "editor" && role != "commentator" && role != "leader" {
			return dto.Comment{}, ErrForbidden
		}
	}

	var c dto.Comment
	err = tx.QueryRowContext(ctx, `
		INSERT INTO item_comments (org_id, item_id, author_id, version, body)
		SELECT $1, i.id, $2, i.version, $3
		FROM items i
		WHERE i.org_id = $1 AND i.id = $4
		RETURNING id, item_id, author_id, version, body, created_at`,
		orgID, authorID, body, itemID).Scan(
		&c.ID, &c.ItemID, &c.AuthorID, &c.Version, &c.Body, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Comment{}, ErrNotFound
	}
	if err != nil {
		return dto.Comment{}, err
	}
	if err := tx.QueryRowContext(ctx,
		`SELECT username FROM users WHERE id = $1`, authorID).Scan(&c.Author); err != nil {
		return dto.Comment{}, err
	}
	return c, tx.Commit()
}

// Delete removes a comment. The author may delete their own comment; org
// admins and leaders of the team responsible for the item's current stage may
// delete any comment on the item.
func (r *CommentRepo) Delete(ctx context.Context, orgID, itemID string, commentID int64, callerID string, callerIsAdmin bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var authorID string
	var isLeader bool
	err = tx.QueryRowContext(ctx, `
		SELECT c.author_id,
		       EXISTS (
		           SELECT 1 FROM items i
		           JOIN workflow_stages s
		             ON s.org_id = i.org_id AND s.id = i.current_stage_id
		           JOIN team_members tm
		             ON tm.org_id = s.org_id AND tm.team_id = s.assigned_team_id
		           WHERE i.org_id = c.org_id AND i.id = c.item_id
		             AND tm.user_id = $4 AND tm.role = 'leader'
		       )
		FROM item_comments c
		WHERE c.org_id = $1 AND c.item_id = $2 AND c.id = $3`,
		orgID, itemID, commentID, callerID).Scan(&authorID, &isLeader)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	if callerID != authorID && !callerIsAdmin && !isLeader {
		return ErrForbidden
	}

	if err := affected(tx.ExecContext(ctx,
		`DELETE FROM item_comments WHERE org_id = $1 AND id = $2`, orgID, commentID)); err != nil {
		return err
	}
	return tx.Commit()
}
