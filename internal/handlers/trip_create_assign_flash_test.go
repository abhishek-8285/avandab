package handlers

// Spec 04 §2 / AGENTS.md no-silent-failures: POST /trips/new assignment
// failures (driver/vehicle) must surface via the flash_error cookie instead of
// being swallowed (`_ =`), and must not abort trip creation.

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/config"
	"transport-app/internal/events"
	repoSQLite "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func newTripAssignDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_trip_assign_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}
	_ = goose.SetDialect("sqlite")
	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTripAssignApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	cfg := &config.Config{
		AppEnv:       "testing",
		CookieSecret: "test-secret-32",
		CookieSecure: false,
		UploadDir:    t.TempDir(),
	}
	repo := repoSQLite.NewRepository(db)
	bus := events.NewInMemoryBus()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	services := service.NewServices(repo, cfg, logger, bus)
	return NewApp(services, cfg, nil, db, nil, nil)
}

func tripCreatePostRequest(t *testing.T, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/trips/new", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := chi.NewRouteContext()
	rctx := context.WithValue(r.Context(), chi.RouteCtxKey, ctx)
	rctx = shared.ContextWithTenantID(rctx, shared.DefaultTenant)
	return r.WithContext(rctx)
}

func flashCookieValue(t *testing.T, resp *http.Response, name string) string {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

func TestTripCreate_AssignmentFailure_SetsFlashAndStillCreatesTrip(t *testing.T) {
	db := newTripAssignDB(t)
	app := newTripAssignApp(t, db)
	h := app.Trips

	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r1', 'Delhi', 'Jaipur', 280, 5, 5000, '1')`)
	require.NoError(t, err)

	form := url.Values{}
	form.Set("route_id", "r1")
	form.Set("departure_time", "2026-09-11 10:00")
	form.Set("driver_id", "no-such-driver")
	form.Set("vehicle_id", "no-such-vehicle")
	w := httptest.NewRecorder()
	h.Create(w, tripCreatePostRequest(t, form))

	assert.Equal(t, http.StatusSeeOther, w.Code)

	flash := flashCookieValue(t, w.Result(), "flash_error")
	assert.Contains(t, flash, "driver assignment failed", "driver assign failure must surface")
	assert.Contains(t, flash, "vehicle assignment failed", "vehicle assign failure must surface")

	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM trips`).Scan(&count))
	assert.Equal(t, 1, count, "trip must be created despite failed assignments")

	var driverID, vehicleID any
	require.NoError(t, db.QueryRow(`SELECT driver_id, vehicle_id FROM trips`).Scan(&driverID, &vehicleID))
	assert.Nil(t, driverID, "failed driver assignment must not stamp a driver")
	assert.Nil(t, vehicleID, "failed vehicle assignment must not stamp a vehicle")
}

func TestTripCreate_AssignmentSuccess_NoFlash(t *testing.T) {
	db := newTripAssignDB(t)
	app := newTripAssignApp(t, db)
	h := app.Trips

	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r1', 'Delhi', 'Jaipur', 280, 5, 5000, '1')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('d1', 'DRV-1', 'Sunil', 'Kumar', '9876500001', 'DL-1', date('now','+1 year'), 'available', '1')`)
	require.NoError(t, err)

	form := url.Values{}
	form.Set("route_id", "r1")
	form.Set("departure_time", "2026-09-11 10:00")
	form.Set("driver_id", "d1")
	w := httptest.NewRecorder()
	h.Create(w, tripCreatePostRequest(t, form))

	assert.Equal(t, http.StatusSeeOther, w.Code)
	assert.Empty(t, flashCookieValue(t, w.Result(), "flash_error"),
		"successful assignment must not produce a flash_error cookie")

	var driverID any
	require.NoError(t, db.QueryRow(`SELECT driver_id FROM trips`).Scan(&driverID))
	require.NotNil(t, driverID)
	assert.Equal(t, "d1", driverID, "successful assignment must stamp the driver")
}
