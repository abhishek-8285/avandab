package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/coldchain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type ColdChainHandlers struct {
	*App
}

func NewColdChainHandlers(app *App) *ColdChainHandlers {
	return &ColdChainHandlers{App: app}
}

func (h *ColdChainHandlers) service() *coldchain.Service {
	return coldchain.NewService(coldchain.NewSQLRepository(h.DB))
}

func writeColdChainJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeColdChainError(w http.ResponseWriter, code int, msg string) {
	writeColdChainJSON(w, code, map[string]string{"error": msg})
}

// RegisterAPIRoutes mounts the RESTful Cold Chain endpoints at /api/v1/cold-chain.
func (h *ColdChainHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/cold-chain", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "read")).Get("/vehicles", h.APIListVehicles)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "read")).Get("/vehicles/{id}", h.APIGetVehicle)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/vehicles/{id}/profile", h.APIConfigureProfile)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/telemetry", h.APIIngestTelemetry)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "read")).Get("/excursions", h.APIListExcursions)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/excursions/{id}/acknowledge", h.APIAcknowledgeExcursion)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/excursions/{id}/resolve", h.APIResolveExcursion)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "read")).Get("/trips/{tripId}/summary", h.APIGetTripSummary)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/trips/{tripId}/certify", h.APICertifyTrip)
		r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "read")).Get("/metrics", h.APIGetFleetMetrics)
	})
}

// Routes mounts the Cold Chain web UI handlers at /cold-chain.
func (h *ColdChainHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "read")).Get("/", h.WebColdChainDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/profiles", h.WebConfigureProfile)
	r.With(middleware.RequirePermission(h.AuthSrv, "coldchain", "write")).Post("/excursions/{id}/resolve", h.WebResolveExcursion)
}

// APIListVehicles lists all reefer-equipped vehicles with real-time zone temperatures.
func (h *ColdChainHandlers) APIListVehicles(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	vehicles, err := h.service().ListReeferVehicles(r.Context(), tenantID)
	if err != nil {
		writeColdChainError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, map[string]interface{}{
		"vehicles": vehicles,
		"total":    len(vehicles),
	})
}

// APIGetVehicle returns reefer configuration and latest telemetry for a vehicle.
func (h *ColdChainHandlers) APIGetVehicle(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "id")

	profile, err := h.service().GetProfile(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeColdChainError(w, http.StatusInternalServerError, err.Error())
		return
	}

	telem, err := h.service().GetLatestTelemetry(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeColdChainError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, map[string]interface{}{
		"vehicle_id": vehicleID,
		"profile":    profile,
		"telemetry":  telem,
	})
}

// APIConfigureProfile sets temperature bands and reefer specifications.
func (h *ColdChainHandlers) APIConfigureProfile(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "id")

	var in coldchain.ConfigureReeferProfileInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeColdChainError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	profile, err := h.service().ConfigureProfile(r.Context(), tenantID, vehicleID, in)
	if err != nil {
		writeColdChainError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, profile)
}

// APIIngestTelemetry ingests probe readings and performs automatic excursion detection.
func (h *ColdChainHandlers) APIIngestTelemetry(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in coldchain.IngestReeferTelemetryInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeColdChainError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	telem, err := h.service().IngestTelemetry(r.Context(), tenantID, in)
	if err != nil {
		writeColdChainError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, telem)
}

// APIListExcursions lists thermal excursion incidents.
func (h *ColdChainHandlers) APIListExcursions(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := r.URL.Query().Get("vehicle_id")
	status := r.URL.Query().Get("status")

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	excursions, total, err := h.service().ListExcursions(r.Context(), tenantID, vehicleID, status, limit, offset)
	if err != nil {
		writeColdChainError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, map[string]interface{}{
		"excursions": excursions,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

// APIAcknowledgeExcursion flags an excursion as acknowledged.
func (h *ColdChainHandlers) APIAcknowledgeExcursion(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	excursionID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	userName := "Operator"
	if user != nil && user.UserID != "" {
		userName = user.UserID
	}

	exc, err := h.service().AcknowledgeExcursion(r.Context(), tenantID, excursionID, userName)
	if err != nil {
		writeColdChainError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, exc)
}

// APIResolveExcursion closes out an excursion with resolution commentary.
func (h *ColdChainHandlers) APIResolveExcursion(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	excursionID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	var in coldchain.ResolveExcursionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeColdChainError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if in.ResolvedBy == "" && user != nil {
		in.ResolvedBy = user.UserID
	}

	exc, err := h.service().ResolveExcursion(r.Context(), tenantID, excursionID, in)
	if err != nil {
		writeColdChainError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, exc)
}

// APIGetTripSummary returns the HACCP / GDP summary for a trip.
func (h *ColdChainHandlers) APIGetTripSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tripID := chi.URLParam(r, "tripId")

	repo := coldchain.NewSQLRepository(h.DB)
	summary, err := repo.GetTripSummary(r.Context(), tenantID, tripID)
	if err != nil {
		writeColdChainError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if summary == nil {
		writeColdChainError(w, http.StatusNotFound, "trip summary not found")
		return
	}

	writeColdChainJSON(w, http.StatusOK, summary)
}

// APICertifyTrip certifies trip cold chain integrity.
func (h *ColdChainHandlers) APICertifyTrip(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tripID := chi.URLParam(r, "tripId")
	user, _ := h.getUserFromContext(r)

	var body struct {
		VehicleID                 string    `json:"vehicle_id"`
		CertifiedBy               string    `json:"certified_by"`
		Readings                  []float64 `json:"readings"`
		TotalExcursionDurationSec int       `json:"total_excursion_duration_sec"`
		DoorOpenCount             int       `json:"door_open_count"`
		StartTime                 time.Time `json:"start_time"`
		EndTime                   time.Time `json:"end_time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeColdChainError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if body.CertifiedBy == "" && user != nil {
		body.CertifiedBy = user.UserID
	}
	if body.StartTime.IsZero() {
		body.StartTime = time.Now().Add(-4 * time.Hour)
	}
	if body.EndTime.IsZero() {
		body.EndTime = time.Now()
	}

	in := coldchain.CertifyTripComplianceInput{
		TripID:      tripID,
		VehicleID:   body.VehicleID,
		CertifiedBy: body.CertifiedBy,
	}

	summary, err := h.service().CertifyTripCompliance(r.Context(), tenantID, in, body.Readings, body.TotalExcursionDurationSec, body.DoorOpenCount, body.StartTime, body.EndTime)
	if err != nil {
		writeColdChainError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusCreated, summary)
}

// APIGetFleetMetrics returns cold chain fleet health metrics.
func (h *ColdChainHandlers) APIGetFleetMetrics(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	metrics, err := h.service().GetFleetMetrics(r.Context(), tenantID)
	if err != nil {
		writeColdChainError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeColdChainJSON(w, http.StatusOK, metrics)
}

// WebColdChainDashboard renders the cold chain operational cockpit.
func (h *ColdChainHandlers) WebColdChainDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	metrics, err := h.service().GetFleetMetrics(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load Cold Chain metrics", err.Error(), user)
		return
	}

	vehicles, err := h.service().ListReeferVehicles(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load Reefer fleet", err.Error(), user)
		return
	}

	excursions, _, err := h.service().ListExcursions(r.Context(), tenantID, "", "", 20, 0)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load temperature excursions", err.Error(), user)
		return
	}

	summaries, _, err := h.service().ListTripSummaries(r.Context(), tenantID, 10, 0)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load trip summaries", err.Error(), user)
		return
	}

	// Fetch all fleet vehicles for the configuration modal dropdown
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, vehicle_number, registration_number
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	var allVehicles []map[string]string
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, vNum, regNum string
			if err := rows.Scan(&id, &vNum, &regNum); err == nil {
				allVehicles = append(allVehicles, map[string]string{
					"ID":                 id,
					"VehicleNumber":      vNum,
					"RegistrationNumber": regNum,
				})
			}
		}
	}

	data := PageData{
		Title: "Cold Chain Telematics",
		User:  user,
		Extra: map[string]interface{}{
			"Metrics":     metrics,
			"Vehicles":    vehicles,
			"Excursions":  excursions,
			"Summaries":   summaries,
			"AllVehicles": allVehicles,
		},
	}

	h.renderPage(w, r, "cold_chain_dashboard.html", data)
}

// WebConfigureProfile handles form submission for configuring reefer thermal specs.
func (h *ColdChainHandlers) WebConfigureProfile(w http.ResponseWriter, r *http.Request) {
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

	// Make/model is required — no vendor default (validate honestly via service).
	model := strings.TrimSpace(r.FormValue("reefer_make_model"))
	if model == "" {
		h.renderError(w, http.StatusBadRequest, "Reefer make/model is required", "reefer_make_model cannot be blank", user)
		return
	}

	numZones := 1
	if nz, err := strconv.Atoi(r.FormValue("num_zones")); err == nil && nz >= 1 && nz <= 3 {
		numZones = nz
	}

	z1Type := strings.TrimSpace(r.FormValue("zone1_type"))
	if z1Type == "" {
		z1Type = coldchain.ZoneTypeChilled
	}

	targetTemp, _ := strconv.ParseFloat(r.FormValue("zone1_target_temp_c"), 64)
	minTemp, _ := strconv.ParseFloat(r.FormValue("zone1_min_temp_c"), 64)
	maxTemp, _ := strconv.ParseFloat(r.FormValue("zone1_max_temp_c"), 64)

	humidityMon := r.FormValue("humidity_monitoring") == "1" || r.FormValue("humidity_monitoring") == "true"
	defrostMode := strings.TrimSpace(r.FormValue("defrost_mode"))
	if defrostMode == "" {
		defrostMode = "AUTO"
	}

	in := coldchain.ConfigureReeferProfileInput{
		ReeferMakeModel:    model,
		NumZones:           numZones,
		Zone1Type:          z1Type,
		Zone1TargetTempC:   targetTemp,
		Zone1MinTempC:      minTemp,
		Zone1MaxTempC:      maxTemp,
		HumidityMonitoring: humidityMon,
		DefrostMode:        defrostMode,
	}

	if _, err := h.service().ConfigureProfile(r.Context(), tenantID, vehicleID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to configure reefer profile", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/cold-chain", http.StatusSeeOther)
}

// WebResolveExcursion handles form submission to close an excursion with corrective note.
func (h *ColdChainHandlers) WebResolveExcursion(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	excursionID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	note := strings.TrimSpace(r.FormValue("resolution_note"))
	if note == "" {
		h.renderError(w, http.StatusBadRequest, "Resolution note required", "Please describe corrective actions taken", user)
		return
	}

	resolvedBy := "Operator"
	if user != nil && user.UserID != "" {
		resolvedBy = user.UserID
	}

	in := coldchain.ResolveExcursionInput{
		ResolutionNote: note,
		ResolvedBy:     resolvedBy,
	}

	if _, err := h.service().ResolveExcursion(r.Context(), tenantID, excursionID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to resolve excursion", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/cold-chain", http.StatusSeeOther)
}
