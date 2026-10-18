package repository

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrDuplicate   = errors.New("already exists")
	ErrInUse       = errors.New("in use")
	ErrStale       = errors.New("stale version")
	ErrForbidden   = errors.New("forbidden")
	ErrNoNextStage = errors.New("no next stage")
)

// StaleError is returned when a version-checked mutation did not match the
// item's current version. It reports the current version so the API can return
// it to the client on a 409. It satisfies errors.Is(err, ErrStale).
type StaleError struct {
	CurrentVersion int64
}

func (e StaleError) Error() string {
	return fmt.Sprintf("stale version: current version is %d", e.CurrentVersion)
}

func (e StaleError) Is(target error) bool { return target == ErrStale }

// ConflictError is returned for uniqueness races that map to HTTP 409 (for
// example an item_changes or activity row already using the version).
type ConflictError struct {
	CurrentVersion int64
	Message        string
}

func (e ConflictError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "conflict"
}

func (e ConflictError) Is(target error) bool { return target == ErrDuplicate }

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// isSerializationFailure reports SQLSTATE 40001, raised by trg_activity_version
// when an activity insert carries a stale item_version.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
