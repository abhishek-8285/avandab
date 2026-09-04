package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTenantPurge_Flow(t *testing.T) {
	db := newTenantsTestDB(t)
	_, err := db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('tenant-px','Purge X','purge-x')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id) VALUES ('u-px','px@x.in','x','PX',6,'active','tenant-px')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO customers (id, tenant_id, name, phone) VALUES ('c-px','tenant-px','PX Corp','9000000001')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO routes (id, tenant_id, source, destination, distance, estimated_hours, standard_fare) VALUES ('r-px','tenant-px','A','B',10,1,100)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, passengers, price, status, tenant_id, version) VALUES ('b-px','BK-PX','c-px',datetime('now'),'r-px','truck',1,100,'confirmed','tenant-px',1)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO trips (id, trip_number, booking_id, route_id, departure_time, status, tenant_id, version) VALUES ('t-px','TR-PX','b-px','r-px',datetime('now'),'scheduled','tenant-px',1)`)
	require.NoError(t, err)

	app := newTenantsTestApp(t, db, &mockAuthSvc{}, true)
	h := NewTenantsHandlers(app)

	previewReq := func() (*httptest.ResponseRecorder, *http.Request) {
		req := httptest.NewRequest(http.MethodGet, "/tenants/tenant-px/purge", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "tenant-px")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		return httptest.NewRecorder(), req
	}

	// 1. Preview counts, deletes nothing.
	rec, req := previewReq()
	h.PurgePreview(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "bookings")
	var n int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bookings WHERE tenant_id='tenant-px'`).Scan(&n))
	assert.Equal(t, 1, n)

	// 2. Wrong confirm refused, data intact.
	form := url.Values{"confirm": {"nope"}}
	badReq := httptest.NewRequest(http.MethodPost, "/tenants/tenant-px/purge", strings.NewReader(form.Encode()))
	badReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "tenant-px")
	badReq = badReq.WithContext(context.WithValue(badReq.Context(), chi.RouteCtxKey, rctx))
	badRec := httptest.NewRecorder()
	h.Purge(badRec, badReq)
	assert.Equal(t, http.StatusBadRequest, badRec.Code)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM trips WHERE tenant_id='tenant-px'`).Scan(&n))
	assert.Equal(t, 1, n)

	// 3. Bootstrap purge refused.
	bootForm := url.Values{"confirm": {"1"}}
	bootReq := httptest.NewRequest(http.MethodPost, "/tenants/1/purge", strings.NewReader(bootForm.Encode()))
	bootReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bootCtx := chi.NewRouteContext()
	bootCtx.URLParams.Add("id", "1")
	bootReq = bootReq.WithContext(context.WithValue(bootReq.Context(), chi.RouteCtxKey, bootCtx))
	bootRec := httptest.NewRecorder()
	h.Purge(bootRec, bootReq)
	assert.Equal(t, http.StatusBadRequest, bootRec.Code)

	// 4. Correct confirm purges ops, keeps users + tenant.
	okForm := url.Values{"confirm": {"tenant-px"}}
	okReq := httptest.NewRequest(http.MethodPost, "/tenants/tenant-px/purge", strings.NewReader(okForm.Encode()))
	okReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	okCtx := chi.NewRouteContext()
	okCtx.URLParams.Add("id", "tenant-px")
	okReq = okReq.WithContext(context.WithValue(okReq.Context(), chi.RouteCtxKey, okCtx))
	okRec := httptest.NewRecorder()
	h.Purge(okRec, okReq)
	assert.Equal(t, http.StatusOK, okRec.Code)

	for _, tbl := range []string{"bookings", "trips", "customers", "routes"} {
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+tbl+` WHERE tenant_id='tenant-px'`).Scan(&n))
		assert.Equal(t, 0, n, tbl+" must be empty")
	}
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM users WHERE tenant_id='tenant-px'`).Scan(&n))
	assert.Equal(t, 1, n, "users survive purge")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM tenants WHERE id='tenant-px'`).Scan(&n))
	assert.Equal(t, 1, n, "tenant row survives purge")
}
