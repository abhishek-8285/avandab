package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	"transport-app/internal/auth"
	"transport-app/internal/events"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	"transport-app/internal/shared/clock"
	"transport-app/internal/shared/uow"
	"transport-app/internal/trip/application"
)

func newCloseTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_trip_close_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	cwd, _ := os.Getwd()
	migrationsDir := "../../../../../db/migrations"
	if filepath.Base(cwd) == "basic" {
		migrationsDir = "db/migrations"
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-1','Test Tenant 1','tenant-1')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newCloseTestHandler(t *testing.T, db *sql.DB) *APITripHandler {
	t.Helper()
	sqlUoW := uow.NewSQLUnitOfWork(db)
	realClock := clock.NewRealClock()
	bus := events.NewInMemoryBus()
	return &APITripHandler{
		completeUC: application.NewCompleteTripUseCase(sqlUoW, realClock),
		opsAlerts:  service.NewOpsAlertServiceForTest(db, bus),
		authSrv:    nil,
	}
}

func seedCloseTrip(t *testing.T, db *sql.DB, tripID string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trips
		(id, trip_number, booking_id, route_id, status, departure_time, arrival_time, tenant_id)
		VALUES (?, ?, 'b-1', 'r-1', 'delivered', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'tenant-1')`,
		tripID, "TRIP-"+tripID)
	require.NoError(t, err)
}

func callComplete(t *testing.T, h *APITripHandler, tripID, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID+"/complete", nil)
	} else {
		req = httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID+"/complete", strings.NewReader(body))
	}
	ctx := shared.ContextWithTenantID(req.Context(), shared.TenantID("tenant-1"))
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "u1", Role: "admin"})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", tripID)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	h.Complete(w, req)
	return w
}

func breakdownAlertCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM ops_alerts WHERE tenant_id = 'tenant-1' AND alert_type = 'vehicle_breakdown' AND status = 'open'`).Scan(&n))
	return n
}

func TestComplete_BreakdownFilesOutstandingAlert(t *testing.T) {
	db := newCloseTestDB(t)
	h := newCloseTestHandler(t, db)
	seedCloseTrip(t, db, "trip-bd-1")

	w := callComplete(t, h, "trip-bd-1", `{"close_odometer":125400.5,"breakdown":true,"breakdown_note":"tyre burst"}`)
	assert.Equal(t, http.StatusOK, w.Code)

	var status string
	var odo sql.NullFloat64
	require.NoError(t, db.QueryRow(
		`SELECT status, close_odometer FROM trips WHERE id = 'trip-bd-1'`).Scan(&status, &odo))
	assert.Equal(t, "completed", status)
	require.True(t, odo.Valid)
	assert.InDelta(t, 125400.5, odo.Float64, 0.001)
	assert.Equal(t, 1, breakdownAlertCount(t, db))
}

func TestComplete_WithoutBreakdownFilesNoAlert(t *testing.T) {
	db := newCloseTestDB(t)
	h := newCloseTestHandler(t, db)
	seedCloseTrip(t, db, "trip-bd-2")

	w := callComplete(t, h, "trip-bd-2", "")
	assert.Equal(t, http.StatusOK, w.Code)

	var status string
	require.NoError(t, db.QueryRow(
		`SELECT status FROM trips WHERE id = 'trip-bd-2'`).Scan(&status))
	assert.Equal(t, "completed", status)
	assert.Equal(t, 0, breakdownAlertCount(t, db))
}
