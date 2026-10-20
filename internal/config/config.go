// Package config loads and validates all runtime configuration in one place.
// Every setting comes from the environment; Load fails with a clear message
// instead of letting the server start half-configured.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// MinSecretLen is the smallest accepted JWT signing secret (HS256 wants at
// least 256 bits of key material).
const MinSecretLen = 32

type Config struct {
	Port          string
	SPADir        string
	JWTSecret     string
	JWTTTL        time.Duration
	MigrateOnBoot bool
	SeedDemo      bool
	Database      DatabaseConfig
}

type DatabaseConfig struct {
	URL                      string
	MaxOpenConns             int
	MaxIdleConns             int
	ConnMaxLifetime          time.Duration
	ConnMaxIdleTime          time.Duration
	LockTimeout              time.Duration
	StatementTimeout         time.Duration
	IdleInTransactionTimeout time.Duration
}

// Load reads configuration from the environment and validates it. Defaults
// match compose.dev.yaml so a plain `go run ./cmd/server` works locally.
func Load() (Config, error) {
	var cfg Config

	cfg.Port = envOrDefault("PORT", "8080")
	cfg.SPADir = os.Getenv("SPA_DIR")

	secret := os.Getenv("JWT_SECRET")
	if len(secret) < MinSecretLen {
		return cfg, fmt.Errorf("JWT_SECRET must be set and at least %d bytes (got %d)", MinSecretLen, len(secret))
	}
	cfg.JWTSecret = secret

	ttl, err := envDuration("JWT_TTL", time.Hour)
	if err != nil {
		return cfg, err
	}
	cfg.JWTTTL = ttl

	migrateOnBoot, err := envBool("MIGRATE_ON_BOOT", true)
	if err != nil {
		return cfg, err
	}
	cfg.MigrateOnBoot = migrateOnBoot

	seedDemo, err := envBool("SEED_DEMO", true)
	if err != nil {
		return cfg, err
	}
	cfg.SeedDemo = seedDemo

	db, err := loadDatabase()
	if err != nil {
		return cfg, err
	}
	cfg.Database = db

	return cfg, nil
}

func loadDatabase() (DatabaseConfig, error) {
	var cfg DatabaseConfig

	cfg.URL = os.Getenv("DATABASE_URL")
	if cfg.URL == "" {
		cfg.URL = buildDSN()
	}

	var err error
	if cfg.MaxOpenConns, err = envInt("DB_MAX_OPEN_CONNS", 20); err != nil {
		return cfg, err
	}
	if cfg.MaxIdleConns, err = envInt("DB_MAX_IDLE_CONNS", 20); err != nil {
		return cfg, err
	}
	if cfg.ConnMaxLifetime, err = envDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute); err != nil {
		return cfg, err
	}
	if cfg.ConnMaxIdleTime, err = envDuration("DB_CONN_MAX_IDLE_TIME", 5*time.Minute); err != nil {
		return cfg, err
	}

	// Server-side guardrails applied to every pooled connection.
	cfg.LockTimeout = 5 * time.Second
	cfg.StatementTimeout = 15 * time.Second
	cfg.IdleInTransactionTimeout = 30 * time.Second

	return cfg, nil
}

func buildDSN() string {
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(envOrDefault("DB_USER", "postgres"), envOrDefault("DB_PASSWORD", "postgres")),
		Host:   net.JoinHostPort(envOrDefault("DB_HOST", "localhost"), envOrDefault("DB_PORT", "5432")),
		Path:   "/" + envOrDefault("DB_NAME", "scaleco"),
	}
	q := url.Values{}
	q.Set("sslmode", envOrDefault("DB_SSLMODE", "disable"))
	u.RawQuery = q.Encode()
	return u.String()
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer (got %q)", key, raw)
	}
	return v, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration like 30m (got %q)", key, raw)
	}
	return v, nil
}

func envBool(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean like true or false (got %q)", key, raw)
	}
	return v, nil
}
