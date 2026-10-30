package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/tyre"
)

type TyreHandlers struct {
	*App
}

func (h *TyreHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/tyres", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/", h.APIListTyres)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/", h.APICreateTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/summary", h.APIGetFleetSummary)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/rotate", h.APIRotateTyres)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}", h.APIGetTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Put("/{id}", h.APIUpdateTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/mount", h.APIMountTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/dismount", h.APIDismountTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/inspect", h.APIRecordInspection)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/retread", h.APIRetreadTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/scrap", h.APIScrapTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}/activities", h.APIListActivities)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}/inspections", h.APIListInspections)
	})

	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/api/v1/vehicles/{id}/tyres", h.APIGetVehicleTyres)
}

func writeTyreJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTyreError(w http.ResponseWriter, code int, msg string) {
	writeTyreJSON(w, code, map[string]string{"error": msg})
}

func (h *TyreHandlers) service() *tyre.Service {
	return tyre.NewService(tyre.NewSQLRepository(h.DB))
}

func (h *TyreHandlers) APIListTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := tyre.TyreFilter{
		TenantID:  tenantID,
		VehicleID: r.URL.Query().Get("vehicle_id"),
		Status:    strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
		Brand:     r.URL.Query().Get("brand"),
		Search:    r.URL.Query().Get("q"),
		Limit:     limit,
		Offset:    offset,
	}

	tyres, total, err := h.service().ListTyres(r.Context(), filter)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyres":  tyres,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *TyreHandlers) APICreateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in tyre.CreateTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	created, err := h.service().CreateTyre(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, tyre.ErrDuplicateSerial) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusCreated, map[string]interface{}{
		"tyre": created,
	})
}

func (h *TyreHandlers) APIGetTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	t, err := h.service().GetTyre(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": t,
	})
}

func (h *TyreHandlers) APIUpdateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.UpdateTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().UpdateTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": updated,
	})
}

func (h *TyreHandlers) APIMountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.MountTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	mounted, err := h.service().MountTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrAxleOccupied) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, tyre.ErrInvalidStatus) {
			writeTyreError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": mounted,
	})
}

func (h *TyreHandlers) APIDismountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.DismountTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	dismounted, err := h.service().DismountTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrInvalidStatus) {
			writeTyreError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": dismounted,
	})
}

func (h *TyreHandlers) APIRotateTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in tyre.RotateTyresInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.service().RotateTyres(r.Context(), tenantID, in); err != nil {
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "success",
		"message": "tyres rotated successfully",
	})
}

func (h *TyreHandlers) APIRecordInspection(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.RecordInspectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	insp, err := h.service().RecordInspection(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusCreated, map[string]interface{}{
		"inspection": insp,
	})
}

func (h *TyreHandlers) APIRetreadTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.RetreadTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().RetreadTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrMaxRetreadsReached) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": updated,
	})
}

func (h *TyreHandlers) APIScrapTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.ScrapTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	scrapped, err := h.service().ScrapTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": scrapped,
	})
}

func (h *TyreHandlers) APIGetVehicleTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehID := chi.URLParam(r, "id")

	tyres, err := h.service().GetVehicleTyres(r.Context(), tenantID, vehID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"vehicle_id": vehID,
		"tyres":      tyres,
	})
}

func (h *TyreHandlers) APIGetFleetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	summary, err := h.service().GetFleetSummary(r.Context(), tenantID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"summary": summary,
	})
}

func (h *TyreHandlers) APIListActivities(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")

	activities, err := h.service().ListActivities(r.Context(), tenantID, tyreID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"activities": activities,
	})
}

func (h *TyreHandlers) APIListInspections(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")

	inspections, err := h.service().ListInspections(r.Context(), tenantID, tyreID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"inspections": inspections,
	})
}
