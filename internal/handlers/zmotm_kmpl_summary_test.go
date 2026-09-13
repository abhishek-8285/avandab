package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedKMPLSummaryFixtures(t *testing.T, app *App, month time.Time) {
	t.Helper()
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta')`)

	_, err := app.DB.Exec(`
INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status, fleet_class, standard_kmpl)
VALUES
('veh-norm', 'tenant-alpha', 'MH12KMPL01', 'KMPL-01', 'truck', 10000, 'diesel', 'running', 'CV', 5.0),
('veh-nonorm', 'tenant-alpha', 'MH12KMPL02', 'KMPL-02', 'truck', 10000, 'diesel', 'running', 'CV', NULL),
('veh-single', 'tenant-alpha', 'MH12KMPL03', 'KMPL-03', 'truck', 10000, 'diesel', 'running', 'CV', NULL),
('veh-pump', 'tenant-alpha', 'PUMP-KMPL', 'PUMP-K', 'truck', 50000, 'diesel', 'available', 'FS', NULL);
`)
	require.NoError(t, err)

	// Pre-period opening fill for veh-norm: odo 10000 on the last day of prior month.
	prevMonth := month.AddDate(0, -1, 0)
	openAt := time.Date(prevMonth.Year(), prevMonth.Month(), 28, 10, 0, 0, 0, time.UTC)
	// In-period fills (5th, 15th): total run 10000 -> 10500 = 500 km,
	// fuel after opening = 50 + 60 = 110 L -> KMPL = 4.5454, variance -9.09%.
	d1 := time.Date(month.Year(), month.Month(), 5, 10, 0, 0, 0, time.UTC)
	d2 := time.Date(month.Year(), month.Month(), 15, 10, 0, 0, 0, time.UTC)
	_, err = app.DB.Exec(`
INSERT INTO fuel_issues (
    id, tenant_id, issue_number, fuel_station_id, vehicle_id, litres_issued,
    vehicle_odometer, issued_at
) VALUES
('kmpl-open', 'tenant-alpha', 'FI-OPEN', 'veh-pump', 'veh-norm', 40.0, 10000.0, ?),
('kmpl-1', 'tenant-alpha', 'FI-001', 'veh-pump', 'veh-norm', 50.0, 10200.0, ?),
('kmpl-2', 'tenant-alpha', 'FI-002', 'veh-pump', 'veh-norm', 60.0, 10500.0, ?),
('kmpl-no-1', 'tenant-alpha', 'FI-NO-1', 'veh-pump', 'veh-nonorm', 80.0, 5000.0, ?),
('kmpl-no-2', 'tenant-alpha', 'FI-NO-2', 'veh-pump', 'veh-nonorm', 70.0, 5400.0, ?),
('kmpl-single-1', 'tenant-alpha', 'FI-S-1', 'veh-pump', 'veh-single', 55.0, 9000.0, ?),
('kmpl-beta-1', 'tenant-beta', 'FI-BETA-K', 'veh-pump', 'veh-norm', 10.0, 100.0, ?)
`, openAt, d1, d2, d1, d2, d1, d1)
	require.NoError(t, err)
}

func TestKMPLSummary_MathVarianceAndIsolation(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	seedKMPLSummaryFixtures(t, app, month)

	req := httptest.NewRequest("GET", "/api/v1/reports/kmpl-summary?month=2026-09", nil)
	req.Header.Set("X-Test-User", "authorized-user")
	req.Header.Set("X-Test-Tenant", "tenant-alpha")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Records []KMPLSummaryDTO `json:"records"`
		Total   int64            `json:"total"`
		Month   string           `json:"month"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "2026-09", resp.Month)
	assert.Equal(t, int64(3), resp.Total, "norm + nonorm + single vehicles")
	require.Len(t, resp.Records, 3)

	byNo := map[string]KMPLSummaryDTO{}
	for _, rec := range resp.Records {
		byNo[rec.VehicleNo] = rec
	}

	// Norm vehicle: 2 in-period fills, opening excluded from fuel.
	norm := byNo["MH12KMPL01"]
	assert.Equal(t, 2, norm.Fills)
	assert.InDelta(t, 110.0, norm.Litres, 0.001)
	require.NotNil(t, norm.DistanceKM)
	assert.InDelta(t, 500.0, *norm.DistanceKM, 0.001)
	require.NotNil(t, norm.KMPL)
	assert.InDelta(t, 500.0/110.0, *norm.KMPL, 0.0001)
	require.NotNil(t, norm.StandardKmpl)
	assert.InDelta(t, 5.0, *norm.StandardKmpl, 0.001)
	require.NotNil(t, norm.VariancePct)
	assert.InDelta(t, (500.0/110.0-5.0)/5.0*100, *norm.VariancePct, 0.01)
	assert.Equal(t, "BELOW_NORM", norm.Flag)

	// No-norm vehicle: first in-period fill opens (400 km / 70 L).
	nonorm := byNo["MH12KMPL02"]
	assert.Equal(t, 2, nonorm.Fills)
	require.NotNil(t, nonorm.KMPL)
	assert.InDelta(t, 400.0/70.0, *nonorm.KMPL, 0.0001)
	assert.Nil(t, nonorm.StandardKmpl)
	assert.Nil(t, nonorm.VariancePct)
	assert.Equal(t, "", nonorm.Flag)

	// Single fill, no history: listed but no KMPL.
	single := byNo["MH12KMPL03"]
	assert.Equal(t, 1, single.Fills)
	assert.Nil(t, single.KMPL)
	assert.Nil(t, single.DistanceKM)

	// Tenant isolation: beta fill invisible.
	for _, rec := range resp.Records {
		assert.NotEqual(t, "FI-BETA-K", rec.VehicleNo)
	}
	assert.NotContains(t, w.Body.String(), "BETA")

	// vehicle_id filter.
	reqF := httptest.NewRequest("GET", "/api/v1/reports/kmpl-summary?month=2026-09&vehicle_id=veh-norm", nil)
	reqF.Header.Set("X-Test-User", "authorized-user")
	reqF.Header.Set("X-Test-Tenant", "tenant-alpha")
	wF := httptest.NewRecorder()
	r.ServeHTTP(wF, reqF)
	require.Equal(t, http.StatusOK, wF.Code)
	var respF struct {
		Records []KMPLSummaryDTO `json:"records"`
		Total   int64            `json:"total"`
	}
	require.NoError(t, json.NewDecoder(wF.Body).Decode(&respF))
	assert.Equal(t, int64(1), respF.Total)
	require.Len(t, respF.Records, 1)
	assert.Equal(t, "MH12KMPL01", respF.Records[0].VehicleNo)

	// Bad month rejected.
	reqB := httptest.NewRequest("GET", "/api/v1/reports/kmpl-summary?month=sept", nil)
	reqB.Header.Set("X-Test-User", "authorized-user")
	reqB.Header.Set("X-Test-Tenant", "tenant-alpha")
	wB := httptest.NewRecorder()
	r.ServeHTTP(wB, reqB)
	assert.Equal(t, http.StatusBadRequest, wB.Code)
}

func TestKMPLSummary_CSV(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	seedKMPLSummaryFixtures(t, app, month)

	req := httptest.NewRequest("GET", "/reports/kmpl-summary.csv?month=2026-09", nil)
	req.Header.Set("X-Test-User", "authorized-user")
	req.Header.Set("X-Test-Tenant", "tenant-alpha")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	content := string(w.Body.Bytes()[3:])
	reader := csv.NewReader(strings.NewReader(content))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4, "1 header + 3 vehicle rows")
	assert.Equal(t, []string{
		"Vehicle No", "Month", "Fills", "Litres", "Distance KM", "KMPL",
		"Standard KMPL", "Variance %", "Flag",
	}, records[0])
	assert.Equal(t, "MH12KMPL01", records[1][0])
	assert.Equal(t, "4.55", records[1][5])
	assert.Equal(t, "5.00", records[1][6])
	assert.Equal(t, "BELOW_NORM", records[1][8])
	assert.True(t, strings.HasPrefix(records[1][7], "-"), "negative variance, got %q", records[1][7])
}

func TestFuelKMPL_PaginationCarryover(t *testing.T) {
	app, r := setupZMOTMReportsTestApp(t)
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	seedKMPLSummaryFixtures(t, app, month)

	// Page through all alpha fills (limit=1) and find FI-001.
	found := map[string]*float64{}
	for page := 1; page <= 6; page++ {
		rq := httptest.NewRequest(
			"GET", fmt.Sprintf("/api/v1/reports/fuel-kmpl?limit=1&page=%d", page), nil,
		)
		rq.Header.Set("X-Test-User", "authorized-user")
		rq.Header.Set("X-Test-Tenant", "tenant-alpha")
		wq := httptest.NewRecorder()
		r.ServeHTTP(wq, rq)
		require.Equal(t, http.StatusOK, wq.Code)
		var pr struct {
			Records []FuelKMPLDTO `json:"records"`
		}
		require.NoError(t, json.NewDecoder(wq.Body).Decode(&pr))
		require.Len(t, pr.Records, 1)
		found[pr.Records[0].IssueNo] = pr.Records[0].KMPL
	}
	// FI-001 is veh-norm's 2nd fill (200 km on 50 L = 4.0) even when it lands
	// on a page alone.
	require.Contains(t, found, "FI-001")
	require.NotNil(t, found["FI-001"], "page-2+ row must carry over prev odometer")
	assert.InDelta(t, 4.0, *found["FI-001"], 0.01)
}

func TestMigration00152_StandardKmplColumn(t *testing.T) {
	app, _ := setupZMOTMReportsTestApp(t)

	var name string
	err := app.DB.QueryRow(
		`SELECT name FROM pragma_table_info('vehicles') WHERE name = 'standard_kmpl'`,
	).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "standard_kmpl", name)

	// CHECK backstop: non-positive norms rejected at the DB layer.
	_, _ = app.DB.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha')`)
	_, err = app.DB.Exec(`
INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status, fleet_class, standard_kmpl)
VALUES ('veh-neg-norm', 'tenant-alpha', 'MH12NEG01', 'NEG-01', 'truck', 10000, 'diesel', 'available', 'CV', -2.0)`)
	require.Error(t, err, "CHECK must reject negative norm")
	_, err = app.DB.Exec(`
INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, vehicle_type, capacity, fuel_type, status, fleet_class, standard_kmpl)
VALUES ('veh-zero-norm', 'tenant-alpha', 'MH12ZERO01', 'ZERO-01', 'truck', 10000, 'diesel', 'available', 'CV', 0.0)`)
	require.Error(t, err, "CHECK must reject zero norm")

	// Down drops the column; up restores it (prove-it: migrations roll back).
	// NOTE: setupZMOTMReportsTestApp chdirs to the repo root.
	migrationsDir := "../../db/migrations"
	if _, serr := os.Stat(migrationsDir); os.IsNotExist(serr) {
		migrationsDir = "db/migrations"
	}
	require.NoError(t, goose.Down(app.DB, migrationsDir))
	var afterDown string
	err = app.DB.QueryRow(
		`SELECT name FROM pragma_table_info('vehicles') WHERE name = 'standard_kmpl'`,
	).Scan(&afterDown)
	require.Error(t, err, "column must be gone after down")
	require.NoError(t, goose.Up(app.DB, migrationsDir))
	err = app.DB.QueryRow(
		`SELECT name FROM pragma_table_info('vehicles') WHERE name = 'standard_kmpl'`,
	).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "standard_kmpl", name)
}
