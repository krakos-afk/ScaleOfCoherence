package config

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestLoadDefaults(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	clearOptional(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.JWTTTL != time.Hour {
		t.Errorf("JWTTTL = %v, want 1h", cfg.JWTTTL)
	}
	if !cfg.MigrateOnBoot {
		t.Error("MigrateOnBoot = false, want true by default")
	}
	if cfg.Database.MaxOpenConns != 20 || cfg.Database.MaxIdleConns != 20 {
		t.Errorf("pool = %d/%d, want 20/20", cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	}
	if cfg.Database.ConnMaxLifetime != 30*time.Minute || cfg.Database.ConnMaxIdleTime != 5*time.Minute {
		t.Errorf("pool lifetimes = %v/%v", cfg.Database.ConnMaxLifetime, cfg.Database.ConnMaxIdleTime)
	}
	if !strings.Contains(cfg.Database.URL, "localhost:5432/scaleco") {
		t.Errorf("DSN = %q, want localhost default", cfg.Database.URL)
	}
}

func TestLoadRequiresStrongSecret(t *testing.T) {
	clearOptional(t)

	t.Setenv("JWT_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an empty JWT_SECRET")
	}

	t.Setenv("JWT_SECRET", "too-short")
	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a short JWT_SECRET")
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)
	t.Setenv("PORT", "9999")
	t.Setenv("JWT_TTL", "15m")
	t.Setenv("MIGRATE_ON_BOOT", "false")
	t.Setenv("DATABASE_URL", "postgres://u:p@db:5432/x?sslmode=disable")
	t.Setenv("DB_MAX_OPEN_CONNS", "5")
	t.Setenv("DB_MAX_IDLE_CONNS", "2")
	t.Setenv("DB_CONN_MAX_LIFETIME", "1m")
	t.Setenv("DB_CONN_MAX_IDLE_TIME", "30s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9999" || cfg.JWTTTL != 15*time.Minute || cfg.MigrateOnBoot {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.Database.URL != "postgres://u:p@db:5432/x?sslmode=disable" {
		t.Errorf("URL = %q", cfg.Database.URL)
	}
	if cfg.Database.MaxOpenConns != 5 || cfg.Database.MaxIdleConns != 2 {
		t.Errorf("pool = %d/%d", cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Setenv("JWT_SECRET", testSecret)

	for _, tc := range []struct{ key, value string }{
		{"JWT_TTL", "soon"},
		{"DB_MAX_OPEN_CONNS", "many"},
		{"DB_MAX_OPEN_CONNS", "-1"},
		{"DB_CONN_MAX_LIFETIME", "0s"},
		{"MIGRATE_ON_BOOT", "maybe"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s=%q", tc.key, tc.value)
			}
		})
	}
}

// clearOptional blanks the optional variables so defaults apply regardless of
// the developer's shell environment. Empty values are treated as unset.
func clearOptional(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"PORT", "SPA_DIR", "JWT_TTL", "MIGRATE_ON_BOOT", "DATABASE_URL",
		"DB_HOST", "DB_PORT", "DB_USER", "DB_PASSWORD", "DB_NAME", "DB_SSLMODE",
		"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME", "DB_CONN_MAX_IDLE_TIME",
		"SEED_DEMO",
	} {
		t.Setenv(key, "")
	}
}
