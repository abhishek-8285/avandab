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

var sqlTokens = []string{"QueryContext(", "ExecContext(", "QueryRowContext("}

// handlerSQLCaps is the per-file raw-SQL call-site baseline.
var handlerSQLCaps = map[string]int{
	"assignment.go":           0,
	"backhaul.go":             1,
	"bookings.go":             1,
	"compliance.go":           4,
	"console.go":              0,
	"contact.go":              4,
	"customer_portal.go":      9,
	"customers.go":            1,
	"driver_money.go":         2,
	"drivers.go":              0,
	"ewaybill.go":             0,
	"failpage.go":             1,
	"fuel_issues.go":          1,
	"invoices.go":             0,
	"invoices_consolidate.go": 0,
	"kharcha.go":              3,
	"kharcha_verify.go":       1,
	"kpis.go":                 0,
	"maintenance.go":          3,
	"maintenance_plans.go":    2,
	"mqtt_credentials.go":     3,
	"otp.go":                  1,
	"outbox_batch.go":         1,
	"public_payment.go":       0,
	"routes.go":               0,
	"scorecard.go":            1,
	"search.go":               2,
	"settings.go":             1,
	"share.go":                0,
	"sos.go":                  0,
	"telemetry_devices.go":    2,
	"tenant_purge.go":         0,
	"tenants.go":              0,
	"trips.go":                0,
	"vehicles.go":             0,
	"work_orders_web.go":      1,
	"zmotm_reports.go":        0,
}

// handlerLineCaps pins the current god-files; they may only shrink.
var handlerLineCaps = map[string]int{
	"trips.go":                1931,
	"app.go":                  1765,
	"invoices_consolidate.go": 920,
	"sto.go":                  1287,
	"invoices.go":             952,
	"zmotm_reports.go":        476,
	"reports.go":              1146,
	"maintenance.go":          1103,
	"auth.go":                 1011,
	"share.go":                826,
	"vehicles.go":             770,
	"maintenance_plans.go":    910,
	"drivers.go":              575,
	"customer_portal.go":      588,
	"assignment.go":           78,
	"tenants.go":              465,
	"public_payment.go":       305,
	"tenant_purge.go":         56,
	"routes.go":               335,
	"sos.go":                  165,
	"ewaybill.go":             371,
	"console.go":              321,
}

func countSQL(text string) int {
	total := 0
	for _, token := range sqlTokens {
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
		if capSQL, ok := handlerSQLCaps[name]; ok {
			assert.LessOrEqual(t, sql, capSQL,
				"%s raw SQL grew to %d (cap %d): move SQL into the owning domain repository, then lower the cap (handlers-end-state ADR)", name, sql, capSQL)
		} else {
			assert.Zero(t, sql, "%s must not add raw SQL: handlers/ is an HTTP layer (handlers-end-state ADR)", name)
		}
		if capLines, ok := handlerLineCaps[name]; ok {
			assert.LessOrEqual(t, lineCount(text), capLines,
				"%s grew to %d lines (cap %d): split at the adapter boundary and lower the cap (handlers-end-state ADR)", name, lineCount(text), capLines)
		}
	}
	assert.LessOrEqual(t, totalSQL, 45,
		"handlers/ raw SQL total grew to %d (cap 45): new SQL belongs in a domain repository", totalSQL)

	agentPath := filepath.Join("..", "agent", "tools.go")
	agentData, err := os.ReadFile(agentPath)
	require.NoError(t, err)
	agentText := string(agentData)
	assert.LessOrEqual(t, countSQL(agentText), 1,
		"agent/tools.go raw SQL grew to %d (cap 1): tools must delegate to application services", countSQL(agentText))
	assert.LessOrEqual(t, lineCount(agentText), 1186,
		"agent/tools.go grew to %d lines (cap 1186): split tools per domain and lower the cap", lineCount(agentText))
}
