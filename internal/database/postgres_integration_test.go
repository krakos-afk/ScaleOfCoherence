package database

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"ScaleOfCoherence/internal/config"
)

// TestConnectAppliesPoolLimitsAndTimeouts verifies that pool limits and the
// server-side guardrails are applied to every pooled connection. It skips when
// no Postgres is reachable.
func TestConnectAppliesPoolLimitsAndTimeouts(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/scaleco_test?sslmode=disable"
	}
	// The settings are per-connection, so any database works; use the admin
	// database to avoid depending on the schema being migrated.
	if u, err := url.Parse(dsn); err == nil {
		u.Path = "/postgres"
		dsn = u.String()
	}

	cfg := config.DatabaseConfig{
		URL:                      dsn,
		MaxOpenConns:             3,
		MaxIdleConns:             2,
		ConnMaxLifetime:          time.Minute,
		ConnMaxIdleTime:          time.Minute,
		LockTimeout:              5 * time.Second,
		StatementTimeout:         15 * time.Second,
		IdleInTransactionTimeout: 30 * time.Second,
	}

	db, err := Connect(cfg)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer db.Close()

	if got := db.DB().Stats().MaxOpenConnections; got != cfg.MaxOpenConns {
		t.Errorf("MaxOpenConnections = %d, want %d", got, cfg.MaxOpenConns)
	}

	var lock, stmt, idle string
	err = db.DB().QueryRow(
		`SELECT current_setting('lock_timeout'),
		        current_setting('statement_timeout'),
		        current_setting('idle_in_transaction_session_timeout')`).
		Scan(&lock, &stmt, &idle)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	for name, got := range map[string]string{
		"lock_timeout":                        lock,
		"statement_timeout":                   stmt,
		"idle_in_transaction_session_timeout": idle,
	} {
		want := map[string]string{
			"lock_timeout":                        "5s",
			"statement_timeout":                   "15s",
			"idle_in_transaction_session_timeout": "30s",
		}[name]
		if strings.TrimSpace(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}
