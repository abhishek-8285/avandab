package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/ev"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type EVHandlers struct {
	*App
}

func NewEVHandlers(app *App) *EVHandlers {
	return &EVHandlers{App: app}
}

func (h *EVHandlers) service() *ev.Service {
	return ev.NewService(ev.NewSQLRepository(h.DB))
}

func writeEVJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeEVError(w http.ResponseWriter, code int, msg string) {
	writeEVJSON(w, code, map[string]string{"error": msg})
}

// RegisterAPIRoutes mounts the RESTful EV telematics endpoints at /api/v1/ev.
func (h *EVHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/ev", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "read")).Get("/vehicles", h.APIListVehicles)
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "read")).Get("/vehicles/{id}", h.APIGetVehicle)
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "write")).Post("/vehicles/{id}/profile", h.APIConfigureProfile)
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "write")).Post("/telemetry", h.APIIngestTelemetry)
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "read")).Get("/charging-sessions", h.APIListChargingSessions)
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "write")).Post("/energy-summary", h.APIRecordEnergySummary)
		r.With(middleware.RequirePermission(h.AuthSrv, "ev", "read")).Get("/metrics", h.APIGetFleetMetrics)
	})
}

// Routes mounts the EV web UI handlers at /ev.
func (h *EVHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "ev", "read")).Get("/", h.WebEVDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "ev", "write")).Post("/profiles", h.WebConfigureProfile)
}

// APIListVehicles lists all electric vehicles with battery SoC, range, and charging status.
func (h *EVHandlers) APIListVehicles(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	vehicles, err := h.service().ListEVVehicles(r.Context(), tenantID)
	if err != nil {
		writeEVError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeEVJSON(w, http.StatusOK, map[string]interface{}{
		"vehicles": vehicles,
		"total":    len(vehicles),
	})
}

// APIGetVehicle returns an EV's profile and latest real-time battery status.
func (h *EVHandlers) APIGetVehicle(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "id")

	profile, err := h.service().GetProfile(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeEVError(w, http.StatusInternalServerError, err.Error())
		return
	}

	telem, err := h.service().GetLatestTelemetry(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeEVError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeEVJSON(w, http.StatusOK, map[string]interface{}{
		"vehicle_id": vehicleID,
		"profile":    profile,
		"telemetry":  telem,
	})
}

// APIConfigureProfile updates EV battery specs.
func (h *EVHandlers) APIConfigureProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "id")

	var in ev.ConfigureEVProfileInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeEVError(w, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}

	profile, err := h.service().ConfigureProfile(r.Context(), tenantID, vehicleID, in)
	if err != nil {
		writeEVError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeEVJSON(w, http.StatusOK, profile)
}

// APIIngestTelemetry ingests CAN/OEM battery telemetry and manages charging session transitions.
func (h *EVHandlers) APIIngestTelemetry(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in ev.IngestEVTelemetryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeEVError(w, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}

	telem, err := h.service().IngestTelemetry(r.Context(), tenantID, in)
	if err != nil {
		writeEVError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeEVJSON(w, http.StatusOK, telem)
}

// APIListChargingSessions returns past and active charging sessions.
func (h *EVHandlers) APIListChargingSessions(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := r.URL.Query().Get("vehicle_id")

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	sessions, total, err := h.service().ListChargingSessions(r.Context(), tenantID, vehicleID, limit, offset)
	if err != nil {
		writeEVError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeEVJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// APIRecordEnergySummary records trip distance, green energy consumption, and carbon offset.
func (h *EVHandlers) APIRecordEnergySummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in ev.RecordEnergySummaryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeEVError(w, http.StatusBadRequest, "invalid request payload: "+err.Error())
		return
	}

	summary, err := h.service().RecordEnergySummary(r.Context(), tenantID, in)
	if err != nil {
		writeEVError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeEVJSON(w, http.StatusCreated, summary)
}

// APIGetFleetMetrics returns top-level EV fleet battery metrics and carbon savings.
func (h *EVHandlers) APIGetFleetMetrics(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	metrics, err := h.service().GetFleetMetrics(r.Context(), tenantID)
	if err != nil {
		writeEVError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeEVJSON(w, http.StatusOK, metrics)
}

// WebEVDashboard renders the EV fleet cockpit and live battery tracking dashboard.
func (h *EVHandlers) WebEVDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	metrics, err := h.service().GetFleetMetrics(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load EV metrics", err.Error(), user)
		return
	}

	vehicles, err := h.service().ListEVVehicles(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load EV fleet", err.Error(), user)
		return
	}

	sessions, _, err := h.service().ListChargingSessions(r.Context(), tenantID, "", 10, 0)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load charging sessions", err.Error(), user)
		return
	}

	summaries, err := h.service().ListRecentEnergySummaries(r.Context(), tenantID, 10)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load energy summaries", err.Error(), user)
		return
	}

	// Fetch all fleet vehicles for the configuration modal dropdown
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, vehicle_number, registration_number, fuel_type
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	var allVehicles []map[string]string
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, vNum, regNum, fuel string
			if err := rows.Scan(&id, &vNum, &regNum, &fuel); err == nil {
				allVehicles = append(allVehicles, map[string]string{
					"ID":                 id,
					"VehicleNumber":      vNum,
					"RegistrationNumber": regNum,
					"FuelType":           fuel,
				})
			}
		}
	}

	data := PageData{
		Title: "EV Telematics",
		User:  user,
		Extra: map[string]interface{}{
			"Metrics":     metrics,
			"Vehicles":    vehicles,
			"Sessions":    sessions,
			"Summaries":   summaries,
			"AllVehicles": allVehicles,
		},
	}

	h.renderPage(w, r, "ev_dashboard.html", data)
}

// WebConfigureProfile handles form submission for configuring EV battery specs.
func (h *EVHandlers) WebConfigureProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	vehicleID := strings.TrimSpace(r.FormValue("vehicle_id"))
	if vehicleID == "" {
		h.renderError(w, http.StatusBadRequest, "Vehicle is required", "vehicle_id cannot be blank", user)
		return
	}

	capKWh, err := strconv.ParseFloat(r.FormValue("battery_capacity_kwh"), 64)
	if err != nil || capKWh <= 0 {
		h.renderError(w, http.StatusBadRequest, "Invalid battery capacity", "Must be positive kWh", user)
		return
	}

	maxRange, err := strconv.ParseFloat(r.FormValue("max_range_km"), 64)
	if err != nil || maxRange <= 0 {
		h.renderError(w, http.StatusBadRequest, "Invalid max range", "Must be positive km", user)
		return
	}

	standard := strings.TrimSpace(r.FormValue("charging_standard"))
	if standard == "" {
		standard = ev.ChargingStandardCCS2
	}

	minSafeSoC := 15.0
	if val := strings.TrimSpace(r.FormValue("min_safe_soc")); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed >= 0 && parsed <= 100 {
			minSafeSoC = parsed
		}
	}

	efficiency := 140.0
	if val := strings.TrimSpace(r.FormValue("nominal_efficiency_wh_km")); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			efficiency = parsed
		}
	}

	in := ev.ConfigureEVProfileInput{
		BatteryCapacityKWh:    capKWh,
		MaxRangeKM:            maxRange,
		ChargingStandard:      standard,
		MinSafeSoC:            minSafeSoC,
		NominalEfficiencyWhKM: efficiency,
	}

	if _, err := h.service().ConfigureProfile(r.Context(), tenantID, vehicleID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to configure EV profile", err.Error(), user)
		return
	}

	// Update fuel_type in vehicles table to 'electric' if not already
	_, _ = h.DB.ExecContext(r.Context(), `UPDATE vehicles SET fuel_type = 'electric' WHERE tenant_id = $1 AND id = $2`, tenantID, vehicleID)

	http.Redirect(w, r, "/ev", http.StatusSeeOther)
}
