package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/facility"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type FacilityHandlers struct {
	*App
}

func (h *FacilityHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/facilities", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "facilities", "read")).Get("/", h.APIListFacilities)
		r.With(middleware.RequirePermission(h.AuthSrv, "facilities", "write")).Post("/", h.APICreateFacility)
		r.With(middleware.RequirePermission(h.AuthSrv, "facilities", "read")).Get("/{id}", h.APIGetFacility)
		r.With(middleware.RequirePermission(h.AuthSrv, "facilities", "write")).Put("/{id}", h.APIUpdateFacility)
		r.With(middleware.RequirePermission(h.AuthSrv, "facilities", "write")).Delete("/{id}", h.APIDeleteFacility)
	})
}

func writeFacilityJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFacilityError(w http.ResponseWriter, code int, msg string) {
	writeFacilityJSON(w, code, map[string]string{"error": msg})
}

func (h *FacilityHandlers) APIListFacilities(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	repo := facility.NewSQLRepository(h.DB)

	q := r.URL.Query().Get("q")
	if q == "" {
		q = r.URL.Query().Get("search")
	}
	facType := r.URL.Query().Get("type")
	if facType == "" {
		facType = r.URL.Query().Get("facility_type")
	}

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	var activeOnly *bool
	if a := r.URL.Query().Get("active_only"); a != "" {
		val := a == "true" || a == "1"
		activeOnly = &val
	}

	filter := facility.FacilityFilter{
		Search:       q,
		FacilityType: facType,
		ActiveOnly:   activeOnly,
		Limit:        limit,
		Offset:       offset,
	}

	list, total, err := repo.List(r.Context(), tenantID, filter)
	if err != nil {
		writeFacilityError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeFacilityJSON(w, http.StatusOK, map[string]interface{}{
		"facilities": list,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

func (h *FacilityHandlers) APICreateFacility(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	repo := facility.NewSQLRepository(h.DB)

	var input facility.CreateFacilityInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeFacilityError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	created, err := repo.Create(r.Context(), tenantID, input)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
			writeFacilityError(w, http.StatusConflict, "facility_code already exists for this organization")
			return
		}
		writeFacilityError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeFacilityJSON(w, http.StatusCreated, map[string]interface{}{
		"facility": created,
	})
}

func (h *FacilityHandlers) APIGetFacility(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	repo := facility.NewSQLRepository(h.DB)

	fac, err := repo.GetByID(r.Context(), tenantID, id)
	if err != nil {
		writeFacilityError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fac == nil {
		// Fallback: try by facility_code
		fac, err = repo.GetByCode(r.Context(), tenantID, id)
		if err != nil {
			writeFacilityError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if fac == nil {
		writeFacilityError(w, http.StatusNotFound, "facility not found")
		return
	}

	writeFacilityJSON(w, http.StatusOK, map[string]interface{}{
		"facility": fac,
	})
}

func (h *FacilityHandlers) APIUpdateFacility(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	repo := facility.NewSQLRepository(h.DB)

	var input facility.UpdateFacilityInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeFacilityError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Resolve internal ID if facility_code was supplied in route
	targetID := id
	existing, err := repo.GetByID(r.Context(), tenantID, id)
	if err != nil {
		writeFacilityError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		byCode, err := repo.GetByCode(r.Context(), tenantID, id)
		if err != nil {
			writeFacilityError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if byCode != nil {
			targetID = byCode.ID
		} else {
			writeFacilityError(w, http.StatusNotFound, "facility not found")
			return
		}
	}

	updated, err := repo.Update(r.Context(), tenantID, targetID, input)
	if err != nil {
		writeFacilityError(w, http.StatusBadRequest, err.Error())
		return
	}
	if updated == nil {
		writeFacilityError(w, http.StatusNotFound, "facility not found")
		return
	}

	writeFacilityJSON(w, http.StatusOK, map[string]interface{}{
		"facility": updated,
	})
}

func (h *FacilityHandlers) APIDeleteFacility(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	repo := facility.NewSQLRepository(h.DB)

	targetID := id
	existing, err := repo.GetByID(r.Context(), tenantID, id)
	if err != nil {
		writeFacilityError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing == nil {
		byCode, err := repo.GetByCode(r.Context(), tenantID, id)
		if err != nil {
			writeFacilityError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if byCode != nil {
			targetID = byCode.ID
		} else {
			writeFacilityError(w, http.StatusNotFound, "facility not found")
			return
		}
	}

	if err := repo.Delete(r.Context(), tenantID, targetID); err != nil {
		writeFacilityError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeFacilityJSON(w, http.StatusOK, map[string]interface{}{
		"deleted": true,
	})
}
