// Package taxverify is the seam for live GSTIN registration verification.
//
// Offline checks (format in validate.ValidGSTIN, mod-36 check digit in
// validate.ValidGSTINChecksum) prove a number is well-formed. Only a live
// lookup against the GST portal (via a GSP or a vendor API) proves the
// taxpayer is registered — and that needs credentials nobody has configured
// yet. Until then every constructor returns the disabled provider, whose
// VerifyGSTIN fails with ErrNotConfigured instead of pretending.
//
// When keys arrive: add a provider implementation behind Provider, select it
// in New by Config.Provider, and have the onboarding/worker path record the
// outcome in tenant_company_profiles.gstin_verify_status (00130).
package taxverify

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

// Verify states mirror tenant_company_profiles.gstin_verify_status (00130).
type Status string

const (
	StatusUnverified Status = "UNVERIFIED"
	StatusPending    Status = "PENDING"
	StatusVerified   Status = "VERIFIED"
	StatusFailed     Status = "FAILED"
)

// ErrNotConfigured is returned when no live provider is wired. Callers must
// treat it as "unknown", never as "invalid" — an unconfigured lookup says
// nothing about the GSTIN.
var ErrNotConfigured = errors.New("taxverify: no live provider configured (set TAXVERIFY_ENABLED=1 with a provider)")

// Result is one live lookup outcome.
type Result struct {
	Verified  bool
	LegalName string
	StateCode string
	CheckedAt time.Time
}

// Provider performs live GSTIN registration lookups. Implementations must
// accept only format+checksum-valid GSTINs (validate first) so paid API
// calls are never spent on numbers math already rejects.
type Provider interface {
	Name() string
	VerifyGSTIN(ctx context.Context, gstin string) (Result, error)
}

// Config selects the live provider. Zero value = disabled.
type Config struct {
	Enabled  bool
	Provider string
}

// ConfigFromEnv reads TAXVERIFY_ENABLED (1/true) and TAXVERIFY_PROVIDER.
func ConfigFromEnv() Config {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TAXVERIFY_ENABLED")))
	return Config{
		Enabled:  v == "1" || v == "true",
		Provider: strings.ToLower(strings.TrimSpace(os.Getenv("TAXVERIFY_PROVIDER"))),
	}
}

type disabledProvider struct{}

func (disabledProvider) Name() string { return "disabled" }

func (disabledProvider) VerifyGSTIN(_ context.Context, _ string) (Result, error) {
	return Result{}, ErrNotConfigured
}

// New returns the configured live provider. With no provider configured it
// returns the disabled provider (never nil); an explicitly enabled but
// unknown provider name is a configuration error.
func New(cfg Config) (Provider, error) {
	if !cfg.Enabled || cfg.Provider == "" {
		return disabledProvider{}, nil
	}
	return nil, errors.New("taxverify: unknown provider " + strconv.Quote(cfg.Provider))
}
