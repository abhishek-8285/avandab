package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
)

func seedDueScheduleCard(t *testing.T, db *sql.DB, veh, sched, tenant string) {
	t.Helper()
	ctx := context.Background()
	repo := maintsql.NewMaintenanceRepository(db)
	// Interval schedule exhausted 10 days ago: closing the card stamps
	// last_done markers, which is what lets EvaluateResolution clear due.
	// (Fixed past due_at dates stay due by design — the schedule itself
	// must be edited — so the loop is proven on interval schedules.)
	stale := time.Now().UTC().Add(-40 * 24 * time.Hour)
	interval := 30
	require.NoError(t, repo.SaveSchedule(ctx, domain.Schedule{
		ID: veh + sched, VehicleID: veh, ServiceType: "brake",
		IntervalDays: &interval, LastDoneAt: &stale, Active: true,
	}))
	require.NoError(t, repo.SetMaintenanceDue(ctx, veh, time.Now().UTC()))
	require.NoError(t, repo.CreateWorkOrder(ctx, domain.WorkOrder{
		ID: "wo-" + veh, TenantID: tenant, VehicleID: veh,
		ScheduleID: strPtr(veh + sched), Title: "Brake job",
	}))
}

// API close: done transition writes the service record and clears the due flag.
func TestWorkOrderAPI_CloseWritesRecord(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-close", "REG-CLOSE")
	seedDueScheduleCard(t, db, "veh-close", "-sched", "1")
	r := newWorkOrderAPIRouter(app)

	w := woAPIRequest(t, r, "POST", "/api/v1/work-orders/wo-veh-close/transition", "1",
		`{"status":"done"}`)
	require.Equal(t, http.StatusOK, w.Code)
	var done map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &done))
	assert.Equal(t, "done", done["status"])
	assert.Nil(t, done["record_error"])

	var svc, sched string
	err := db.QueryRow(`SELECT service_type, schedule_id FROM maintenance_records WHERE vehicle_id = 'veh-close'`).Scan(&svc, &sched)
	require.NoError(t, err, "closing must write a service record")
	assert.Equal(t, "brake", svc)
	assert.Equal(t, "veh-close-sched", sched)

	var due sql.NullString
	require.NoError(t, db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-close'").Scan(&due))
	assert.False(t, due.Valid, "due flag clears when the books close")
}

// Web close: same loop through the detail-page form.
func TestWorkOrderWeb_CloseWritesRecord(t *testing.T) {
	db := newMaintHandlerTestDB(t)
	app := newMaintHandlerApp(t, db, maintAllowAuthSvc{})
	insertMaintTestVehicle(t, db, "veh-wclose", "REG-WCLOSE")
	seedDueScheduleCard(t, db, "veh-wclose", "-sched", "1")

	w := woWebRequest(t, app.Maintenance.TransitionWorkOrder, "POST",
		"/maintenance/work-orders/wo-veh-wclose/transition", "1",
		url.Values{"status": {"done"}}.Encode())
	require.Equal(t, http.StatusSeeOther, w.Code)
	var hasFlashErr bool
	for _, c := range w.Result().Cookies() {
		if c.Name == "flash_error" && c.Value != "" {
			hasFlashErr = true
		}
	}
	assert.False(t, hasFlashErr, "close must not flash errors")

	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM maintenance_records WHERE vehicle_id = 'veh-wclose'`).Scan(&n))
	assert.Equal(t, 1, n)

	var due sql.NullString
	require.NoError(t, db.QueryRow("SELECT maintenance_due FROM vehicles WHERE id = 'veh-wclose'").Scan(&due))
	assert.False(t, due.Valid)

	// Detail page still renders the done card.
	d := woWebRequest(t, app.Maintenance.ViewWorkOrder, "GET",
		"/maintenance/work-orders/wo-veh-wclose", "1", "")
	require.Equal(t, http.StatusOK, d.Code)
	assert.Contains(t, d.Body.String(), "Done")
}
