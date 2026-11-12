package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Architecture ratchet (docs/tech-specs/handlers-end-state-adr.md).
//
// The handlers/ package and agent/tools.go must not grow new raw SQL or new
// god-files while domains migrate to vertical slices. Caps may only be
// LOWERED in the same PR that shrinks the file; raising one requires an ADR
// amendment. The baseline was measured at head 41345ca (2026-11-10).

// handlerSQLCaps is the per-file raw-SQL call-site baseline.
func handlerSQLCaps() map[string]int {
	return map[string]int{
		"assignment.go":           0,
		"backhaul.go":             0,
		"bookings.go":             0,
		"compliance.go":           0,
		"console.go":              0,
		"contact.go":              0,
		"customer_portal.go":      0,
		"customers.go":            0,
		"driver_money.go":         0,
		"drivers.go":              0,
		"ewaybill.go":             0,
		"failpage.go":             0,
		"fuel_issues.go":          0,
		"invoices.go":             0,
		"invoices_consolidate.go": 0,
		"kharcha.go":              0,
		"kharcha_verify.go":       0,
		"kpis.go":                 0,
		"maintenance.go":          0,
		"maintenance_plans.go":    0,
		"mqtt_credentials.go":     0,
		"otp.go":                  0,
		"outbox_batch.go":         0,
		"public_payment.go":       0,
		"routes.go":               0,
		"scorecard.go":            0,
		"search.go":               0,
		"settings.go":             0,
		"share.go":                0,
		"sos.go":                  0,
		"telemetry_devices.go":    0,
		"tenant_purge.go":         0,
		"tenants.go":              0,
		"trips.go":                0,
		"vehicles.go":             0,
		"work_orders_web.go":      0,
		"zmotm_reports.go":        0,
	}
}

// handlerLineCaps pins the current god-files; they may only shrink.
func handlerLineCaps() map[string]int {
	return map[string]int{
		"trips.go":                1931,
		"app.go":                  1765,
		"invoices_consolidate.go": 920,
		"sto.go":                  1287,
		"invoices.go":             952,
		"zmotm_reports.go":        476,
		"reports.go":              1146,
		"maintenance.go":          1081,
		"auth.go":                 1011,
		"share.go":                826,
		"vehicles.go":             770,
		"maintenance_plans.go":    898,
		"drivers.go":              575,
		"customer_portal.go":      477,
		"assignment.go":           78,
		"tenants.go":              465,
		"public_payment.go":       305,
		"tenant_purge.go":         56,
		"routes.go":               335,
		"sos.go":                  165,
		"ewaybill.go":             371,
		"console.go":              321,
		"work_orders_web.go":      139,
		"kharcha.go":              505,
		"driver_money.go":         160,
		"kharcha_verify.go":       61,
		"mqtt_credentials.go":     135,
		"telemetry_devices.go":    700,
		"compliance.go":           329,
		"contact.go":              273,
		"search.go":               112,
		"settings.go":             576,
		"scorecard.go":            670,
		"outbox_batch.go":         65,
		"otp.go":                  135,
		"fuel_issues.go":          538,
		"failpage.go":             315,
		"customers.go":            416,
		"bookings.go":             458,
		"backhaul.go":             246,
	}
}

func countSQL(text string) int {
	total := 0
	for _, token := range []string{"QueryContext(", "ExecContext(", "QueryRowContext("} {
		total += strings.Count(text, token)
	}
	return total
}

func lineCount(text string) int {
	return strings.Count(text, string(rune(10))) + 1
}

func TestArchitectureRatchet_HandlersSQLAndGodFiles(t *testing.T) {
	dir := filepath.Join("..", "handlers")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	totalSQL := 0
	sqlCaps := handlerSQLCaps()
	lineCaps := handlerLineCaps()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		text := string(data)
		sql := countSQL(text)
		totalSQL += sql
		if capSQL, ok := sqlCaps[name]; ok {
			assert.LessOrEqual(t, sql, capSQL,
				"%s raw SQL grew to %d (cap %d): move SQL into the owning domain repository, then lower the cap (handlers-end-state ADR)", name, sql, capSQL)
		} else {
			assert.Zero(t, sql, "%s must not add raw SQL: handlers/ is an HTTP layer (handlers-end-state ADR)", name)
		}
		if capLines, ok := lineCaps[name]; ok {
			assert.LessOrEqual(t, lineCount(text), capLines,
				"%s grew to %d lines (cap %d): split at the adapter boundary and lower the cap (handlers-end-state ADR)", name, lineCount(text), capLines)
		}
	}
	// Phase 2 is complete: handlers/ reached zero raw SQL (baseline 245 at
	// 41345ca). Zero is the invariant, not a cap to grow into — any new SQL
	// here must move into the owning domain repository instead.
	assert.Zero(t, totalSQL,
		"handlers/ gained %d raw SQL call site(s): handlers/ is an HTTP layer and must stay at zero (handlers-end-state ADR Phase 2 complete)", totalSQL)

	agentPath := filepath.Join("..", "agent", "tools.go")
	agentData, err := os.ReadFile(agentPath)
	require.NoError(t, err)
	agentText := string(agentData)
	// Phase 3 increments: alert reads moved to internal/alerts/application and
	// the work-order tools moved to the internal/maintenance facade, so
	// tools.go is at zero raw SQL (0 is the invariant, not a cap to grow into)
	// and shrank by the removed repository plumbing.
	assert.Zero(t, countSQL(agentText),
		"agent/tools.go gained %d raw SQL call site(s): tools must delegate to application services (handlers-end-state ADR Phase 3)", countSQL(agentText))
	assert.LessOrEqual(t, lineCount(agentText), 1028,
		"agent/tools.go grew to %d lines (cap 1028): split tools per domain and lower the cap", lineCount(agentText))
}
