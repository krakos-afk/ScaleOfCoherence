package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrIdempotencyInProgress means another request with the same key is
	// still running; the caller should retry later.
	ErrIdempotencyInProgress = errors.New("request with this idempotency key is already in progress")
	// ErrIdempotencyMismatch means the key was already used for a different
	// request body; reusing it would be a client bug.
	ErrIdempotencyMismatch = errors.New("idempotency key was already used with a different request")
)

// StoredResponse is a completed request's response captured for replay.
type StoredResponse struct {
	Status      int
	Body        []byte
	ContentType string
}

// IdempotencyRepo stores per-(org, user) idempotency keys so mutating requests
// can be retried safely. See migrations/000010_idempotency_keys.up.sql.
type IdempotencyRepo struct {
	db *sql.DB
}

func NewIdempotencyRepo(db *sql.DB) *IdempotencyRepo {
	return &IdempotencyRepo{db: db}
}

// Claim atomically reserves the key for a request with the given body hash.
//
//   - fresh key: returns (nil, nil) and the caller must eventually Complete
//     or Delete it;
//   - completed key with the same hash: returns the stored response to replay;
//   - completed/in-progress key with a different hash: ErrIdempotencyMismatch;
//   - in-progress key: ErrIdempotencyInProgress.
func (r *IdempotencyRepo) Claim(ctx context.Context, orgID, userID, key, requestHash string) (*StoredResponse, error) {
	// A concurrent cleanup can delete the row between the insert attempt and
	// the lookup, which then looks like a fresh key again; one retry covers it.
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		var inserted bool
		err := r.db.QueryRowContext(ctx, `
			INSERT INTO idempotency_keys (org_id, user_id, key, request_hash)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (org_id, user_id, key) DO NOTHING
			RETURNING true`,
			orgID, userID, key, requestHash).Scan(&inserted)
		if err == nil {
			return nil, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		stored, hash, err := r.lookup(ctx, orgID, userID, key)
		if errors.Is(err, ErrNotFound) {
			lastErr = err
			continue
		}
		if err != nil {
			return nil, err
		}
		if hash != requestHash {
			return nil, ErrIdempotencyMismatch
		}
		if stored == nil {
			return nil, ErrIdempotencyInProgress
		}
		return stored, nil
	}
	return nil, fmt.Errorf("idempotency claim kept losing races: %w", lastErr)
}

// lookup returns the stored response (nil while in progress) and the request
// hash for an existing key.
func (r *IdempotencyRepo) lookup(ctx context.Context, orgID, userID, key string) (*StoredResponse, string, error) {
	var hash string
	var status sql.NullInt64
	var body []byte
	var contentType sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT request_hash, status_code, response_body, content_type
		FROM idempotency_keys
		WHERE org_id = $1 AND user_id = $2 AND key = $3`,
		orgID, userID, key).Scan(&hash, &status, &body, &contentType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if !status.Valid {
		return nil, hash, nil
	}
	return &StoredResponse{Status: int(status.Int64), Body: body, ContentType: contentType.String}, hash, nil
}

// Complete records the response so later requests with the same key can be
// replayed without re-executing them.
func (r *IdempotencyRepo) Complete(ctx context.Context, orgID, userID, key string, status int, body []byte, contentType string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET status_code = $4, response_body = $5, content_type = $6, completed_at = now()
		WHERE org_id = $1 AND user_id = $2 AND key = $3`,
		orgID, userID, key, status, body, contentType)
	return err
}

// Delete releases a claim so a failed request can be retried.
func (r *IdempotencyRepo) Delete(ctx context.Context, orgID, userID, key string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM idempotency_keys
		WHERE org_id = $1 AND user_id = $2 AND key = $3`,
		orgID, userID, key)
	return err
}

// DeleteExpired removes keys older than the cutoff and reports how many rows
// went away.
func (r *IdempotencyRepo) DeleteExpired(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
