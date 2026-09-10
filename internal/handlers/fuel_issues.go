package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/fuel"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// RegisterFuelIssueAPIRoutes mounts the /api/v1/fuel-issues REST endpoints.
func (h *FuelAuditHandlers) RegisterFuelIssueAPIRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "read")).Get("/api/v1/fuel-issues", h.APIListFuelIssues)
	r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "read")).Get("/api/v1/fuel-issues/{id}", h.APIGetFuelIssue)
	r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "create")).Post("/api/v1/fuel-issues", h.APIRecordFuelIssue)
}

func writeFuelJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFuelError(w http.ResponseWriter, code int, msg string) {
	writeFuelJSON(w, code, map[string]string{"error": msg})
}

// APIListFuelIssues returns a list of fuel issue logs for the tenant.
func (h *FuelAuditHandlers) APIListFuelIssues(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	repo := fuel.NewSQLFuelIssueRepository(h.DB)

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := fuel.FuelIssueFilter{
		VehicleID:     r.URL.Query().Get("vehicle_id"),
		FuelStationID: r.URL.Query().Get("fuel_station_id"),
		Limit:         limit,
		Offset:        offset,
	}

	issues, err := repo.ListFuelIssues(r.Context(), tenantID, filter)
	if err != nil {
		writeFuelError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeFuelJSON(w, http.StatusOK, map[string]interface{}{
		"fuel_issues": issues,
		"count":       len(issues),
	})
}

// APIGetFuelIssue returns one fuel issue record.
func (h *FuelAuditHandlers) APIGetFuelIssue(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	repo := fuel.NewSQLFuelIssueRepository(h.DB)

	issue, err := repo.GetFuelIssue(r.Context(), tenantID, id)
	if err != nil {
		writeFuelError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if issue == nil {
		writeFuelError(w, http.StatusNotFound, "fuel issue not found")
		return
	}
	writeFuelJSON(w, http.StatusOK, issue)
}

// APIRecordFuelIssue handles posting a new fuel issue entry.
func (h *FuelAuditHandlers) APIRecordFuelIssue(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	actorID := "system"
	if user, ok := h.getUserFromContext(r); ok && user != nil {
		actorID = user.UserID
	}

	var req fuel.RecordFuelIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeFuelError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	repo := fuel.NewSQLFuelIssueRepository(h.DB)
	created, err := repo.RecordFuelIssue(r.Context(), tenantID, actorID, req)
	if err != nil {
		writeFuelError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeFuelJSON(w, http.StatusCreated, created)
}
