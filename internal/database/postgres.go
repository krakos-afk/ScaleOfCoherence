package database

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type DatabasePG struct {
	db *sql.DB
}

func Connect() (*DatabasePG, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		host := envOrDefault("DB_HOST", "localhost")
		port := envOrDefault("DB_PORT", "5432")
		user := envOrDefault("DB_USER", "postgres")
		password := envOrDefault("DB_PASSWORD", "postgres")
		dbName := envOrDefault("DB_NAME", "scaleco")
		sslMode := envOrDefault("DB_SSLMODE", "disable")
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", user, password, host, port, dbName, sslMode)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &DatabasePG{db: db}, nil
}

func (database *DatabasePG) Migrate() error {
	driver, err := postgres.WithInstance(database.db, &postgres.Config{})
	if err != nil {
		return err
	}

	migration, err := migrate.NewWithDatabaseInstance("file://migrations", "postgres", driver)
	if err != nil {
		return err
	}
	// migration.Close() would also close the shared *sql.DB, so it is intentionally skipped.

	if err := migration.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (database *DatabasePG) DB() *sql.DB {
	return database.db
}
