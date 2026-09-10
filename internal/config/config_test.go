package config_test

import (
	"strings"
	"testing"

	"transport-app/internal/config"
)

func TestLoad_DefaultsAndEnv(t *testing.T) {
	t.Run("production env", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("PORT", "9090")
		t.Setenv("MAX_UPLOAD_SIZE", "20")
		t.Setenv("SESSION_MAX_AGE", "48")

		cfg := config.Load()

		if !cfg.IsProduction() || cfg.IsDevelopment() {
			t.Fatalf("expected production environment")
		}

		if cfg.Port != "9090" {
			t.Fatalf("expected port 9090, got %s", cfg.Port)
		}
	})

	t.Run("development default", func(t *testing.T) {
		devCfg := config.Load()
		if !devCfg.IsDevelopment() || devCfg.IsProduction() {
			t.Fatalf("expected development environment default")
		}
	})
}

func TestValidate_DatabaseURL(t *testing.T) {
	valid := []string{
		"file:transport.db?mode=rwc&cache=shared&_foreign_keys=on&_journal_mode=WAL",
		"file:/abs/path/data.sqlite",
		"file:path.db",
		"transport.db",
		"./uploads/transport.sqlite3",
		"data/transport.db",
		"UPPER.DB",
	}
	for _, raw := range valid {
		cfg := &config.Config{DatabaseURL: raw}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", raw, err)
		}
	}

	invalid := []string{
		"http://example.com/db.sqlite",
		"postgres://user:pass@host/db",
		"mysql://host/db",
		"notes.txt",
	}
	for _, raw := range invalid {
		cfg := &config.Config{DatabaseURL: raw}
		err := cfg.Validate()
		if err == nil {
			t.Errorf("Validate(%q) = nil, want error", raw)
			continue
		}
		if !strings.Contains(err.Error(), "DATABASE_URL") {
			t.Errorf("Validate(%q) error %q missing key name DATABASE_URL", raw, err)
		}
	}
}

func TestLoad_MaxUploadSizeMalformedKeepsDefault(t *testing.T) {
	t.Setenv("MAX_UPLOAD_SIZE", "not-a-number")

	cfg := config.Load()
	if cfg.MaxUploadSize != int64(10<<20) {
		t.Fatalf("expected default max upload size %d, got %d", int64(10<<20), cfg.MaxUploadSize)
	}
}

func TestValidate_DatabaseDriver(t *testing.T) {
	t.Run("postgres with DSN valid", func(t *testing.T) {
		cfg := &config.Config{
			DatabaseURL: "postgres://user:pass@host/db",
			Database: config.DatabaseConfig{
				Driver: "postgres",
				URL:    "postgres://user:pass@host/db",
			},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate(postgres DSN) = %v, want nil", err)
		}
	})

	t.Run("postgresql alias accepted", func(t *testing.T) {
		cfg := &config.Config{
			DatabaseURL: "postgres://host/db",
			Database:    config.DatabaseConfig{Driver: "postgresql", URL: "postgres://host/db"},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate(postgresql) = %v, want nil", err)
		}
	})

	t.Run("postgres without DSN invalid", func(t *testing.T) {
		cfg := &config.Config{Database: config.DatabaseConfig{Driver: "postgres"}}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
			t.Errorf("Validate = %v, want error mentioning DATABASE_URL", err)
		}
	})

	t.Run("unknown driver rejected", func(t *testing.T) {
		cfg := &config.Config{
			DatabaseURL: "file:x.db",
			Database:    config.DatabaseConfig{Driver: "oracle", URL: "file:x.db"},
		}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "DATABASE_DRIVER") {
			t.Errorf("Validate = %v, want error mentioning DATABASE_DRIVER", err)
		}
	})
}

func TestValidate_CacheDriver(t *testing.T) {
	valid := []string{"none", "memory", "redis", ""}
	for _, driver := range valid {
		cfg := &config.Config{
			DatabaseURL: "transport.db",
			Cache:       config.CacheConfig{Driver: driver, RedisAddr: "localhost:6379"},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate(cache=%q) = %v, want nil", driver, err)
		}
	}

	cfg := &config.Config{
		DatabaseURL: "transport.db",
		Cache:       config.CacheConfig{Driver: "memcached"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "CACHE_DRIVER") {
		t.Errorf("Validate(cache=memcached) = %v, want error mentioning CACHE_DRIVER", err)
	}

	cfg = &config.Config{
		DatabaseURL: "transport.db",
		Cache:       config.CacheConfig{Driver: "redis", RedisAddr: ""},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "REDIS_ADDR") {
		t.Errorf("Validate(redis without addr) = %v, want error mentioning REDIS_ADDR", err)
	}
}

func TestLoad_DatabaseAndCacheDefaults(t *testing.T) {
	cfg := config.Load()
	if cfg.Database.Driver != "sqlite" {
		t.Errorf("default DATABASE_DRIVER = %q, want sqlite", cfg.Database.Driver)
	}
	if cfg.DatabaseURL != cfg.Database.URL {
		t.Errorf("DatabaseURL %q must mirror Database.URL %q", cfg.DatabaseURL, cfg.Database.URL)
	}
	if cfg.Cache.Driver != "memory" {
		t.Errorf("default CACHE_DRIVER = %q, want memory", cfg.Cache.Driver)
	}
}
