package db

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxColumns is the append-only schema ratchet: a table may not gain columns
// past its recorded cap. 222 ADD COLUMN statements across 61 migrations grew
// `trips` to 52 columns and `vehicles` to 62 because every feature appended in
// place. When a feature needs more fields, put them in a satellite table
// (the vehicle_ev_profiles / customer_booking_details pattern) instead.
// Raising a cap is allowed only with a comment here saying why the new column
// belongs in that aggregate rather than a table of its own.
var maxColumns = map[string]int{
	"Vehicle":      62,
	"Trip":         52,
	"FreightAudit": 44,
	"Alert":        31,
	"EwayBill":     30,
	// 25 -> 27 (00185): deactivated_at is the KYC-retention clock and
	// kyc_purged_at is the purge-idempotency stamp for KYC columns already on
	// this row (the same worker clears aadhaar/pan in place), so a satellite
	// table would only add a join around one UPDATE.
	"Driver":                27,
	"TripStop":              29,
	"Tyre":                  28,
	"TelemetryPosition":     26,
	"Invoice":               26,
	"DriverExpense":         26,
	"Rfq":                   25,
	"DriverSettlement":      25,
	"CustomerBookingDetail": 24,
	"Customer":              23,
	"VideoSafetyEvent":      22,
	"VehicleLatestPosition": 22,
	"InvoiceLineItem":       22,
	"CompanySetting":        22,
	"TenantCompanyProfile":  21,
	"PlannedStop":           20,
	"FuelIssue":             20,
	"FuelClaimAudit":        20,
	"Facility":              20,
	"CustomerQuote":         20,
}

// maxTableColumns is the hard ceiling no table may cross, applied even to
// tables that are not yet in maxColumns.
const maxTableColumns = 62

var structRE = regexp.MustCompile(`(?s)type (\w+) struct \{\n(.*?)\n\}`)

// TestSchemaColumnRatchet reads the sqlc-generated models (one field per
// column, regenerated from the migrations) and fails when a table grows wider
// than its cap.
func TestSchemaColumnRatchet(t *testing.T) {
	path := filepath.Join("generated", "sqlite", "models.go")
	src, err := os.ReadFile(path)
	require.NoError(t, err, "regenerate with sqlc after a migration")

	seen := map[string]int{}
	for _, m := range structRE.FindAllStringSubmatch(string(src), -1) {
		name := m[1]
		fields := 0
		for _, line := range regexp.MustCompile(`\n`).Split(m[2], -1) {
			if len(line) > 0 {
				fields++
			}
		}
		seen[name] = fields
		if fields > maxTableColumns {
			t.Errorf("%s has %d columns (hard ceiling %d): split the new fields into a satellite table", name, fields, maxTableColumns)
		}
	}
	require.NotEmpty(t, seen, "no models parsed from %s", path)

	for table, cap := range maxColumns {
		n, ok := seen[table]
		if !ok {
			continue // table dropped/renamed; the cap is just stale, not a failure
		}
		assert.LessOrEqual(t, n, cap,
			"%s grew to %d columns (cap %d): put the new fields in a satellite table or raise the cap in maxColumns with a reason",
			table, n, cap)
	}
}
