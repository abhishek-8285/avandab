package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/apperr"
	"transport-app/internal/domain"
	"transport-app/internal/httpx"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// ComplianceHandlers manages compliance checks, status inspection, document exemptions, and compliance dashboard (Spec 05 §5, Spec 08 §2.3).
type ComplianceHandlers struct {
	*App
	compliance *service.ComplianceService
	radar      *service.ComplianceRadarService
}

// NewComplianceHandlers creates a new ComplianceHandlers instance.
func NewComplianceHandlers(app *App, compliance *service.ComplianceService) *ComplianceHandlers {
	return &ComplianceHandlers{
		App:        app,
		compliance: compliance,
	}
}

// Routes mounts compliance endpoints.
func (h *ComplianceHandlers) Routes(r chi.Router) {
	// Creating an exemption mutates compliance state — requires update,
	// not just the read gate wrapping this mount.
	r.With(middleware.ResourcePermission(h.AuthSrv, "compliance", "update")).Post("/exemptions", h.CreateExemption)
	r.Get("/exemptions", h.ListExemptions)
	r.Get("/status", h.Status)
}

// Mount registers top-level compliance dashboard routes.
func (h *ComplianceHandlers) Mount(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "compliance", "read")).Get("/compliance/dashboard", h.DashboardPage)
	r.With(middleware.RequirePermission(h.AuthSrv, "compliance", "read")).Get("/api/compliance/dashboard", h.DashboardJSON)
	r.With(middleware.RequirePermission(h.AuthSrv, "compliance", "read")).Get("/api/v1/compliance/dashboard", h.DashboardJSON)
	// Spec 22 §2.8 — compliance radar (attached post-construction; nil
	// service degrades to 503 so the rest of compliance keeps working).
	r.With(middleware.RequirePermission(h.AuthSrv, "compliance", "read")).Get("/api/compliance/radar", h.Radar)
}

// AttachRadar wires the radar service (built in main.go where the alert
// pipeline engine lives).
func (h *ComplianceHandlers) AttachRadar(radar *service.ComplianceRadarService) {
	h.radar = radar
}

// Radar handles GET /api/compliance/radar (Spec 22 §2.8).
func (h *ComplianceHandlers) Radar(w http.ResponseWriter, r *http.Request) {
	if h.radar == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeNotImplemented))
		return
	}
	out, err := h.radar.Radar(r.Context(), string(shared.TenantIDFromContext(r.Context())))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// BlockedEntity represents a blocked driver or vehicle.
type BlockedEntity struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// ComplianceEntityMetrics represents aggregate statistics for drivers or vehicles.
type ComplianceEntityMetrics struct {
	Total        int `json:"total"`
	Blocked      int `json:"blocked"`
	ExpiringSoon int `json:"expiring_soon"`
}

// ComplianceDashboardData represents overview metrics across drivers, vehicles, and documents.
type ComplianceDashboardData struct {
	Drivers          ComplianceEntityMetrics `json:"drivers"`
	Vehicles         ComplianceEntityMetrics `json:"vehicles"`
	BlockedDrivers   []BlockedEntity         `json:"blocked_drivers"`
	BlockedVehicles  []BlockedEntity         `json:"blocked_vehicles"`
	DocumentsPending int                     `json:"documents_pending"`
}

// DashboardJSON returns JSON compliance overview metrics.
func (h *ComplianceHandlers) DashboardJSON(w http.ResponseWriter, r *http.Request) {
	data, err := h.getDashboardData(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

// DashboardPage renders the HTML compliance overview dashboard.
func (h *ComplianceHandlers) DashboardPage(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	data, err := h.getDashboardData(r)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load compliance data", err.Error(), session)
		return
	}

	h.renderForm(w, r, "compliance_dashboard.html", PageData{
		Title: "Compliance Dashboard",
		User:  session,
		Extra: map[string]interface{}{
			"Data": data,
		},
	})
}

func (h *ComplianceHandlers) getDashboardData(r *http.Request) (ComplianceDashboardData, error) {
	db := h.DB
	if db == nil {
		return ComplianceDashboardData{}, fmt.Errorf("database unavailable")
	}
	ctx := r.Context()
	now := time.Now().Truncate(24 * time.Hour)
	soon := now.Add(7 * 24 * time.Hour)

	var data ComplianceDashboardData
	data.BlockedDrivers = []BlockedEntity{}
	data.BlockedVehicles = []BlockedEntity{}

	// Drivers metrics
	rows, err := db.QueryContext(ctx, `SELECT id, license_expiry, status, blocked, COALESCE(blocked_reason,'') FROM drivers`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, status, bReason string
			var lExp sql.NullString
			var blocked int
			if err := rows.Scan(&id, &lExp, &status, &blocked, &bReason); err == nil {
				data.Drivers.Total++
				isBlocked := blocked == 1 || status == "blocked"
				reason := bReason
				if reason == "" {
					reason = "driver blocked"
				}

				if lExp.Valid && lExp.String != "" {
					if t, err := time.Parse("2006-01-02", lExp.String); err == nil {
						if t.Before(now) {
							isBlocked = true
							reason = fmt.Sprintf("License expired %s", lExp.String)
						} else if t.Before(soon) {
							data.Drivers.ExpiringSoon++
						}
					}
				}

				if isBlocked {
					data.Drivers.Blocked++
					data.BlockedDrivers = append(data.BlockedDrivers, BlockedEntity{ID: id, Reason: reason})
				}
			}
		}
	}

	// Vehicles metrics
	vRows, err := db.QueryContext(ctx, `SELECT id, insurance_expiry, fitness_expiry, permit_expiry, COALESCE(puc_expiry,''), status, blocked, COALESCE(blocked_reason,'') FROM vehicles`)
	if err == nil {
		defer vRows.Close()
		for vRows.Next() {
			var id, status, bReason string
			var insExp, fitExp, perExp, pucExp sql.NullString
			var blocked int
			if err := vRows.Scan(&id, &insExp, &fitExp, &perExp, &pucExp, &status, &blocked, &bReason); err == nil {
				data.Vehicles.Total++
				isBlocked := blocked == 1 || status == "blocked"
				reason := bReason
				if reason == "" {
					reason = "vehicle blocked"
				}

				for _, item := range []struct {
					name string
					exp  sql.NullString
				}{
					{"Insurance", insExp},
					{"Fitness", fitExp},
					{"Permit", perExp},
					{"PUC", pucExp},
				} {
					if item.exp.Valid && item.exp.String != "" {
						if t, err := time.Parse("2006-01-02", item.exp.String); err == nil {
							if t.Before(now) {
								isBlocked = true
								reason = fmt.Sprintf("%s expired %s", item.name, item.exp.String)
								break
							} else if t.Before(soon) {
								data.Vehicles.ExpiringSoon++
							}
						}
					}
				}

				if isBlocked {
					data.Vehicles.Blocked++
					data.BlockedVehicles = append(data.BlockedVehicles, BlockedEntity{ID: id, Reason: reason})
				}
			}
		}
	}

	// Pending documents count
	var dPending, vPending int
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM driver_documents WHERE status = 'pending_review'`).Scan(&dPending)
	_ = db.QueryRowContext(ctx, `SELECT count(*) FROM vehicle_documents WHERE status = 'pending_review'`).Scan(&vPending)
	data.DocumentsPending = dPending + vPending

	return data, nil
}

// CreateExemption creates a temporary exemption for a compliance document.
func (h *ComplianceHandlers) CreateExemption(w http.ResponseWriter, r *http.Request) {
	user, _ := h.getUserFromContext(r)
	userID := ""
	if user != nil {
		userID = user.UserID
	}

	entityType := r.FormValue("entity_type")
	entityID := r.FormValue("entity_id")
	docType := r.FormValue("doc_type")
	reason := r.FormValue("reason")
	exemptUntilStr := r.FormValue("exempt_until")

	if entityType == "" || entityID == "" || docType == "" || reason == "" || exemptUntilStr == "" {
		http.Error(w, "missing required fields (entity_type, entity_id, doc_type, reason, exempt_until)", http.StatusBadRequest)
		return
	}

	exemptUntil, err := time.Parse("2006-01-02", exemptUntilStr)
	if err != nil {
		exemptUntil, err = time.Parse(time.RFC3339, exemptUntilStr)
		if err != nil {
			http.Error(w, "invalid exempt_until format (expected YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
	}

	// Set expiry to end of that day (23:59:59)
	exemptUntil = time.Date(exemptUntil.Year(), exemptUntil.Month(), exemptUntil.Day(), 23, 59, 59, 0, time.UTC)

	ex := service.ComplianceExemption{
		EntityType:  entityType,
		EntityID:    entityID,
		DocType:     docType,
		Reason:      reason,
		ExemptUntil: exemptUntil,
		CreatedBy:   userID,
	}

	if err := h.compliance.CreateExemption(r.Context(), ex); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create exemption", err.Error(), user)
		return
	}

	if isDatastarRequest(r) || r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "message": "Exemption granted"})
		return
	}

	referer := r.Header.Get("Referer")
	if referer != "" {
		http.Redirect(w, r, referer, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/trips", http.StatusSeeOther)
}

// ListExemptions returns exemptions for an entity.
func (h *ComplianceHandlers) ListExemptions(w http.ResponseWriter, r *http.Request) {
	entityType := r.URL.Query().Get("entity_type")
	entityID := r.URL.Query().Get("entity_id")

	exemptions, err := h.compliance.ListExemptions(r.Context(), entityType, entityID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(exemptions)
}

// Status returns compliance check details for a driver and/or vehicle.
func (h *ComplianceHandlers) Status(w http.ResponseWriter, r *http.Request) {
	driverID := r.URL.Query().Get("driver_id")
	vehicleID := r.URL.Query().Get("vehicle_id")

	res, err := h.compliance.CheckDispatchCompliance(r.Context(), driverID, vehicleID)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":   false,
			"blocked": true,
			"reason":  err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

// TripComplianceFragment renders a Datastar HTML fragment showing compliance status on a trip page.
func (h *TripHandlers) TripComplianceFragment(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	trip, err := h.Services.Trips.GetTrip(r.Context(), domain.TripID(tripID))
	if err != nil {
		http.Error(w, "trip not found", http.StatusNotFound)
		return
	}

	driverID := ""
	if trip.DriverID != nil {
		driverID = string(*trip.DriverID)
	}
	vehicleID := ""
	if trip.VehicleID != nil {
		vehicleID = string(*trip.VehicleID)
	}

	res, compErr := h.Services.Compliance.CheckDispatchCompliance(r.Context(), driverID, vehicleID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf(`<div id="trip-compliance-status" class="p-4 rounded-xl border %s mb-4">`, func() string {
		if compErr != nil || res.Blocked {
			return "bg-red-50 dark:bg-red-950/30 border-red-200 dark:border-red-800 text-red-900 dark:text-red-200"
		}
		if len(res.Alerts) > 0 {
			return "bg-amber-50 dark:bg-amber-950/30 border-amber-200 dark:border-amber-800 text-amber-900 dark:text-amber-200"
		}
		return "bg-emerald-50 dark:bg-emerald-950/30 border-emerald-200 dark:border-emerald-800 text-emerald-900 dark:text-emerald-200"
	}()))

	buf.WriteString(`<div class="flex items-center gap-2 font-semibold text-sm">`)
	if compErr != nil || res.Blocked {
		buf.WriteString(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4 shrink-0 text-red-600"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="m9 9 6 6"/><path d="m15 9-6 6"/></svg>`)
		buf.WriteString(fmt.Sprintf(`<span>Dispatch Blocked: %s</span>`, func() string {
			if compErr != nil {
				return compErr.Error()
			}
			return res.Reason
		}()))
	} else if len(res.Alerts) > 0 {
		buf.WriteString(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4 shrink-0 text-amber-600"><path d="m21.73 18-8-14a2 2 0 0 0-3.46 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3"/><line x1="12" x2="12" y1="9" y2="13"/><line x1="12" x2="12.01" y1="17" y2="17"/></svg>`)
		buf.WriteString(`<span>Compliance Warnings Active</span>`)
	} else {
		buf.WriteString(`<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="w-4 h-4 shrink-0 text-emerald-600"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10"/><path d="m9 12 2 2 4-4"/></svg>`)
		buf.WriteString(`<span>All 5 Dispatch Compliance Documents Verified</span>`)
	}
	buf.WriteString(`</div>`)

	if len(res.Alerts) > 0 {
		buf.WriteString(`<ul class="mt-2 text-xs list-disc list-inside space-y-1">`)
		for _, a := range res.Alerts {
			buf.WriteString(fmt.Sprintf(`<li>%s</li>`, a))
		}
		buf.WriteString(`</ul>`)
	}

	buf.WriteString(`</div>`)
	_, _ = w.Write([]byte(buf.String()))
}
