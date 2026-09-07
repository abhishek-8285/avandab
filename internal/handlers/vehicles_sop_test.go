package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

// TestSelectedVehicles_SOPProfile exercises the 00126 fleet-object registry:
// profile create, fleet-class filter, measuring points/documents, and the
// Fleet Object + Measuring sections on the view page.
func TestSelectedVehicles_SOPProfile(t *testing.T) {
	db := newVehiclesSelectedDB(t)
	app := newVehiclesSelectedApp(t, db, &mockAuthSvc{})
	r := chi.NewRouter()
	r.Route("/vehicles", app.Vehicles.Routes)

	futureDate := time.Now().AddDate(1, 0, 0).Format("2006-01-02")
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())), "1", "user-1", "admin")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	get := func(path string) *httptest.ResponseRecorder {
		req := withVehicleTenantSession(httptest.NewRequest(http.MethodGet, path, nil), "1", "user-1", "admin")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	profileForm := url.Values{
		"registration_number": {"KA30P1234"},
		"vehicle_number":      {"V-SOP"},
		"vehicle_type":        {"truck"},
		"capacity":            {"10000"},
		"fuel_type":           {"diesel"},
		"insurance_expiry":    {futureDate},
		"fitness_expiry":      {futureDate},
		"permit_expiry":       {futureDate},
		"fleet_class":         {"CV"},
		"ownership":           {"O"},
		"description":         {"TATA TRUCK"},
		"manufacturer":        {"TATA"},
		"facility_id":         {"MM21000000757"},
		"fleet_number":        {"35"},
		"chassis_no":          {"CHS-SOP-1"},
		"usage_indicator":     {"M"},
	}
	w := post("/vehicles/new", profileForm)
	require.Equal(t, http.StatusSeeOther, w.Code)

	// Fuel-station fleet object for the class filter.
	fsForm := url.Values{
		"registration_number": {"BLR-FST-42"},
		"vehicle_number":      {"FST-42"},
		"vehicle_type":        {"truck"},
		"capacity":            {"0"},
		"fuel_type":           {"diesel"},
		"insurance_expiry":    {futureDate},
		"fitness_expiry":      {futureDate},
		"permit_expiry":       {futureDate},
		"fleet_class":         {"FS"},
		"ownership":           {"F"},
		"facility_id":         {"MM21000000757"},
	}
	require.Equal(t, http.StatusSeeOther, post("/vehicles/new", fsForm).Code)

	// Resolve the CV vehicle's id via filtered search page.
	w = get("/vehicles?fleet_class=CV")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "KA30P1234")
	require.NotContains(t, w.Body.String(), "BLR-FST-42")

	w = get("/vehicles?fleet_class=FS")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "BLR-FST-42")
	require.NotContains(t, w.Body.String(), "KA30P1234")

	// Find the CV vehicle id from the unfiltered list links.
	listBody := get("/vehicles").Body.String()
	id := vehicleIDFromList(t, listBody, "KA30P1234")

	// Edit form renders with the profile (exercises named-type eq in template).
	w = get("/vehicles/" + id + "/edit")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "TATA")

	// View shows the Fleet Object section.
	w = get("/vehicles/" + id)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Fleet Object")
	require.Contains(t, w.Body.String(), "TATA")
	require.Contains(t, w.Body.String(), "MM21000000757")

	// IK01 point + IK11 document round-trip through HTTP.
	w = post("/vehicles/"+id+"/points", url.Values{
		"kind":            {"ODO"},
		"annual_estimate": {"50000"},
		"description":     {"Tata Truck KA30P1234"},
	})
	require.Equal(t, http.StatusSeeOther, w.Code)

	w = get("/vehicles/" + id)
	require.Contains(t, w.Body.String(), "ODO")

	pointID := pointIDFromView(t, w.Body.String())

	w = post("/vehicles/points/"+pointID+"/measurements", url.Values{
		"vehicle_id":      {id},
		"counter_reading": {"1200"},
		"read_by":         {"TCS795488"},
	})
	require.Equal(t, http.StatusSeeOther, w.Code)

	w = get("/vehicles/" + id)
	require.Contains(t, w.Body.String(), "1200")

	// Backwards counter rejected.
	w = post("/vehicles/points/"+pointID+"/measurements", url.Values{
		"vehicle_id":      {id},
		"counter_reading": {"100"},
	})
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid fleet class rejected at the form boundary.
	bad := url.Values{
		"registration_number": {"KA99XX9999"},
		"vehicle_number":      {"V-BAD"},
		"vehicle_type":        {"truck"},
		"capacity":            {"1000"},
		"fuel_type":           {"diesel"},
		"insurance_expiry":    {futureDate},
		"fitness_expiry":      {futureDate},
		"permit_expiry":       {futureDate},
		"fleet_class":         {"XX"},
	}
	require.Equal(t, http.StatusBadRequest, post("/vehicles/new", bad).Code)
}

func vehicleIDFromList(t *testing.T, body, reg string) string {
	t.Helper()
	idx := strings.Index(body, reg)
	require.NotEqual(t, -1, idx, "registration %s not found in list", reg)
	// Walk backwards to the enclosing /vehicles/{id} link.
	link := strings.LastIndex(body[:idx], `/vehicles/`)
	require.NotEqual(t, -1, link)
	rest := body[link+len(`/vehicles/`):]
	end := strings.IndexAny(rest, `"' `)
	require.NotEqual(t, -1, end)
	return rest[:end]
}

func pointIDFromView(t *testing.T, body string) string {
	t.Helper()
	idx := strings.Index(body, "/vehicles/points/")
	require.NotEqual(t, -1, idx, "point form action not found")
	rest := body[idx+len("/vehicles/points/"):]
	end := strings.IndexAny(rest, `"'/ `)
	require.NotEqual(t, -1, end)
	return rest[:end]
}
