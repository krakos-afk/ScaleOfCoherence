package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	scaleco "ScaleOfCoherence"
	"ScaleOfCoherence/internal/config"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type DatabasePG struct {
	db *sql.DB
}

// Connect opens the pool, applies pool limits and server-side timeouts
// (lock_timeout, statement_timeout, idle_in_transaction_session_timeout) to
// every connection, and verifies the database is reachable.
func Connect(cfg config.DatabaseConfig) (*DatabasePG, error) {
	connConfig, err := pgx.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	connConfig.RuntimeParams["lock_timeout"] = fmt.Sprintf("%dms", cfg.LockTimeout.Milliseconds())
	connConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%dms", cfg.StatementTimeout.Milliseconds())
	connConfig.RuntimeParams["idle_in_transaction_session_timeout"] = fmt.Sprintf("%dms", cfg.IdleInTransactionTimeout.Milliseconds())

	db := stdlib.OpenDB(*connConfig)
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &DatabasePG{db: db}, nil
}

// Ping reports whether the database is reachable. Used by the readiness probe.
func (database *DatabasePG) Ping(ctx context.Context) error {
	return database.db.PingContext(ctx)
}

// Migrate applies all pending migrations from the embedded files.
func (database *DatabasePG) Migrate() error {
	driver, err := postgres.WithInstance(database.db, &postgres.Config{})
	if err != nil {
		return err
	}

	source, err := iofs.New(scaleco.Migrations, "migrations")
	if err != nil {
		return err
	}

	migration, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return err
	}
	// migration.Close() would also close the shared *sql.DB, so it is intentionally skipped.

	if err := migration.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

func (database *DatabasePG) Close() error {
	return database.db.Close()
}

func (database *DatabasePG) DB() *sql.DB {
	return database.db
}
