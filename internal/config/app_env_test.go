package config_test

import (
	"testing"

	"transport-app/internal/config"
)

// TestAppEnvFromEnv pins the audit 2026-11-12 H6 signal: config keeps its
// documented development default for an unset APP_ENV (so local runs work),
// but callers can tell an explicit "development" from an unset value and
// refuse to start a deployment that silently downgraded the secret guard.
func TestAppEnvFromEnv(t *testing.T) {
	t.Setenv("APP_ENV", "")
	if env, explicit := config.AppEnvFromEnv(); explicit || env != "development" {
		t.Fatalf("unset APP_ENV must default to development and report explicit=false, got env=%q explicit=%v", env, explicit)
	}

	t.Setenv("APP_ENV", "production")
	if env, explicit := config.AppEnvFromEnv(); !explicit || env != "production" {
		t.Fatalf("set APP_ENV must be reported explicitly, got env=%q explicit=%v", env, explicit)
	}
}
