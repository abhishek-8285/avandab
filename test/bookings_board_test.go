package test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bookingapp "transport-app/internal/booking/application"
	bookingagg "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	uow "transport-app/internal/shared/uow"
)

func seedBoardFixtures(t *testing.T, db *sql.DB) {
	t.Helper()
	day := "2026-08-25 10:00:00"
	must := func(q string, args ...any) {
		_, err := db.Exec(q, args...)
		require.NoError(t, err, q)
	}
	for _, b := range []struct{ id, status string }{
		{"bk-p1", "pending"}, {"bk-c1", "confirmed"}, {"bk-d1", "completed"}, {"bk-x1", "cancelled"},
	} {
		must(`INSERT INTO bookings (id, booking_number, customer_id, route_id, vehicle_type, pickup_date, price, status, tenant_id, created_at, updated_at)
		      VALUES (?, ?, 'cust-1', 'r-1', 'truck', ?, 24000, ?, 'tenant-a', ?, ?)`,
			b.id, "BN-"+b.id, day, b.status, day, day)
	}
	// Other tenant — must not leak.
	must(`INSERT INTO bookings (id, booking_number, customer_id, route_id, vehicle_type, pickup_date, price, status, tenant_id, created_at, updated_at)
	      VALUES ('bk-t2','BN-T2','cust-1','r-1','truck',?,1000,'pending','tenant-b',?,?)`, day, day, day)
}

// TestSpec22_BookingsBoard_MirrorsStatuses — Spec 22 §7 S5: board JSON
// reflects the real booking statuses per lane and stays tenant-scoped.
func TestSpec22_BookingsBoard_MirrorsStatuses(t *testing.T) {
	db := NewTestDB(t)
	seedBoardFixtures(t, db)

	h := &handlers.BookingHandlers{App: &handlers.App{DB: db}}
	r := chi.NewRouter()
	r.With(tenantMW("tenant-a")).Get("/bookings/board", h.Board)
	r.With(tenantMW("tenant-b")).Get("/b/board", h.Board)

	req := httptest.NewRequest(http.MethodGet, "/bookings/board?format=json", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Columns []struct {
			Status string `json:"status"`
			Cards  []struct {
				ID      string  `json:"id"`
				Freight float64 `json:"freight"`
			} `json:"cards"`
		} `json:"columns"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	byStatus := map[string][]string{}
	for _, col := range resp.Columns {
		for _, c := range col.Cards {
			byStatus[col.Status] = append(byStatus[col.Status], c.ID)
		}
	}
	assert.Equal(t, []string{"bk-p1"}, byStatus["pending"])
	assert.Equal(t, []string{"bk-c1"}, byStatus["confirmed"])
	assert.Equal(t, []string{"bk-d1"}, byStatus["completed"])
	assert.Equal(t, []string{"bk-x1"}, byStatus["cancelled"])

	// Tenant isolation.
	req2 := httptest.NewRequest(http.MethodGet, "/b/board?format=json", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp2 struct {
		Columns []struct {
			Cards []struct{ ID string } `json:"cards"`
		} `json:"columns"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	total := 0
	for _, col := range resp2.Columns {
		total += len(col.Cards)
	}
	assert.Equal(t, 1, total)
}

// TestSpec22_BackwardsDragRejectedByServer — Spec 22 §9 edge case 9: even
// if a client forces a backwards transition, the server rejects it. The
// board's confirm target only accepts pending bookings (domain machine).
func TestSpec22_BackwardsDragRejectedByServer(t *testing.T) {
	db := NewTestDB(t)
	seedBoardFixtures(t, db)

	uowImpl := uow.NewSQLUnitOfWork(db)
	confirmUC := bookingapp.NewConfirmBookingUseCase(uowImpl, clock.NewRealClock())

	err := confirmUC.Execute(shared.ContextWithTenantID(context.Background(), "1"), bookingapp.ConfirmBookingCommand{
		BookingID: bookingagg.BookingID("bk-d1"), // completed
		TenantID:  shared.TenantID("tenant-a"),
	})
	assert.Error(t, err, "completed booking must not be confirmable")

	err = confirmUC.Execute(shared.ContextWithTenantID(context.Background(), "1"), bookingapp.ConfirmBookingCommand{
		BookingID: bookingagg.BookingID("bk-c1"), // already confirmed
		TenantID:  shared.TenantID("tenant-a"),
	})
	assert.Error(t, err, "confirmed booking must not be re-confirmable")
}
