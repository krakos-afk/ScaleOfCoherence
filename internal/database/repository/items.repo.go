package repository

import (
	"context"
	"database/sql"
	"errors"

	"ScaleOfCoherence/internal/api/dto"
)

type ItemRepo struct {
	db *sql.DB
}

func NewItemRepo(db *sql.DB) *ItemRepo {
	return &ItemRepo{db: db}
}

func (r *ItemRepo) Create(ctx context.Context, orgID, userID string, req dto.ItemRequest) (dto.Item, error) {
	i := dto.Item{Title: req.Title, Description: req.Description, Priority: req.Priority}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO items (org_id, created_by, title, description, priority) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		orgID, userID, req.Title, req.Description, req.Priority).Scan(&i.ID, &i.CreatedAt)
	return i, err
}

func (r *ItemRepo) List(ctx context.Context, orgID string, limit, offset int) (dto.Page[dto.Item], error) {
	page := dto.Page[dto.Item]{Data: []dto.Item{}}
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE org_id = $1`, orgID).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, title, description, priority, created_at FROM items
		WHERE org_id = $1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var i dto.Item
		if err := rows.Scan(&i.ID, &i.Title, &i.Description, &i.Priority, &i.CreatedAt); err != nil {
			return page, err
		}
		page.Data = append(page.Data, i)
	}
	return page, rows.Err()
}

func (r *ItemRepo) Get(ctx context.Context, orgID, id string) (dto.Item, error) {
	var i dto.Item
	err := r.db.QueryRowContext(ctx,
		`SELECT id, title, description, priority, created_at FROM items WHERE org_id = $1 AND id = $2`, orgID, id).
		Scan(&i.ID, &i.Title, &i.Description, &i.Priority, &i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, ErrNotFound
	}
	return i, err
}

func (r *ItemRepo) Update(ctx context.Context, orgID, id string, req dto.ItemRequest) (dto.Item, error) {
	i := dto.Item{ID: id, Title: req.Title, Description: req.Description, Priority: req.Priority}
	err := r.db.QueryRowContext(ctx, `
		UPDATE items SET title = $3, description = $4, priority = $5, version = version + 1, updated_at = now()
		WHERE org_id = $1 AND id = $2 RETURNING created_at`,
		orgID, id, req.Title, req.Description, req.Priority).Scan(&i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return dto.Item{}, ErrNotFound
	}
	return i, err
}

func (r *ItemRepo) Delete(ctx context.Context, orgID, id string) error {
	return affected(r.db.ExecContext(ctx, `DELETE FROM items WHERE org_id = $1 AND id = $2`, orgID, id))
}
