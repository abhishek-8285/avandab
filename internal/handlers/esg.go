package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/sustainability"
	esgapp "transport-app/internal/sustainability/application"
)

// ESGHandlers exposes REST endpoints for Scope 3 emissions, carbon certificates, and BRSR reports (Spec 20 §3, B11).
type ESGHandlers struct {
	*App
	useCase *esgapp.ESGUsecase
}

// NewESGHandlers creates a new ESGHandlers.
func NewESGHandlers(app *App, useCase *esgapp.ESGUsecase) *ESGHandlers {
	return &ESGHandlers{
		App:     app,
		useCase: useCase,
	}
}

// Routes mounts the /esg web UI routes.
func (h *ESGHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "esg", "read")).Get("/", h.WebESGDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "esg", "write")).Post("/generate", h.WebGenerateSnapshot)
}

// WebESGDashboard renders the ESG carbon reporting dashboard.
func (h *ESGHandlers) WebESGDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)
	snapshots, err := h.useCase.ListSnapshots(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load ESG snapshots", err.Error(), user)
		return
	}

	var totalCO2eKG float64
	var totalDistanceKM float64
	var cleanDistanceKM float64
	var totalCargoTKM float64
	for _, s := range snapshots {
		totalCO2eKG += s.TotalCO2eKG
		totalDistanceKM += s.TotalDistanceKM
		cleanDistanceKM += s.EVDistanceKM + s.BS6DistanceKM
		totalCargoTKM += s.TotalCargoTKM
	}

	var avgIntensity float64
	if totalCargoTKM > 0 {
		avgIntensity = (totalCO2eKG * 1000.0) / totalCargoTKM
	}

	data := PageData{
		Title: "ESG & Carbon Reporting",
		User:  user,
		Extra: map[string]interface{}{
			"Snapshots":       snapshots,
			"TotalCO2eMT":     totalCO2eKG / 1000.0,
			"AvgIntensity":    avgIntensity,
			"CleanDistanceKM": cleanDistanceKM,
			"TotalDistanceKM": totalDistanceKM,
		},
	}
	h.renderPage(w, r, "esg_dashboard.html", data)
}

// WebGenerateSnapshot calculates a new periodic snapshot and redirects to the dashboard.
func (h *ESGHandlers) WebGenerateSnapshot(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	now := time.Now().UTC()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	endOfMonth := now.Format("2006-01-02")

	req := sustainability.GenerateSnapshotRequest{
		PeriodStart: startOfMonth,
		PeriodEnd:   endOfMonth,
	}
	createdBy := "staff"
	if user, ok := h.getUserFromContext(r); ok && user != nil && user.Name != "" {
		createdBy = user.Name
	}
	_, _ = h.useCase.GenerateSnapshot(r.Context(), tenantID, req, createdBy)
	http.Redirect(w, r, "/esg", http.StatusSeeOther)
}

// RegisterAPIRoutes mounts the /api/v1/esg/* REST endpoints.
func (h *ESGHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/esg", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "esg", "read")).Get("/trips/{trip_id}", h.GetTripCarbonCertificate)
		r.With(middleware.RequirePermission(h.AuthSrv, "esg", "read")).Get("/snapshots", h.ListSnapshots)
		r.With(middleware.RequirePermission(h.AuthSrv, "esg", "write")).Post("/snapshots/generate", h.GenerateSnapshot)
		r.With(middleware.RequirePermission(h.AuthSrv, "esg", "read")).Get("/reports/brsr", h.GetBRSRReport)
	})
}

// GetTripCarbonCertificate returns audited carbon metrics for a trip (GET /api/v1/esg/trips/{trip_id}).
func (h *ESGHandlers) GetTripCarbonCertificate(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.MustTenantID(r.Context()))
	tripID := chi.URLParam(r, "trip_id")
	if tripID == "" {
		http.Error(w, `{"error":"trip_id is required"}`, http.StatusBadRequest)
		return
	}

	cert, err := h.useCase.GetTripCarbonCertificate(r.Context(), tenantID, tripID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, `{"error":"trip not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cert)
}

// ListSnapshots returns all historical periodic snapshots (GET /api/v1/esg/snapshots).
func (h *ESGHandlers) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.MustTenantID(r.Context()))

	snapshots, err := h.useCase.ListSnapshots(r.Context(), tenantID)
	if err != nil {
		http.Error(w, `{"error":"failed to load snapshots"}`, http.StatusInternalServerError)
		return
	}

	if snapshots == nil {
		snapshots = []sustainability.ESGEmissionSnapshot{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(snapshots)
}

// GenerateSnapshot generates and stores a new periodic emission snapshot (POST /api/v1/esg/snapshots/generate).
func (h *ESGHandlers) GenerateSnapshot(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.MustTenantID(r.Context()))

	var req sustainability.GenerateSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	createdBy := "system"
	if session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && session != nil && session.UserID != "" {
		createdBy = session.UserID
	}

	snap, err := h.useCase.GenerateSnapshot(r.Context(), tenantID, req, createdBy)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(snap)
}

// GetBRSRReport returns SEBI BRSR Principle 6 carbon report in JSON or CSV format (GET /api/v1/esg/reports/brsr).
func (h *ESGHandlers) GetBRSRReport(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.MustTenantID(r.Context()))
	year := r.URL.Query().Get("year")
	format := strings.ToLower(r.URL.Query().Get("format"))

	report, err := h.useCase.GetBRSRReport(r.Context(), tenantID, year)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", `attachment; filename="brsr_principle6_report.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"Reporting Period", "Scope 1 (Tonnes)", "Scope 3 Cat 4 (Tonnes)", "Total CO2e (Tonnes)", "Intensity kg/km", "Intensity kg/tkm", "Total Distance KM", "Green Fleet %"})
		_ = cw.Write([]string{
			report.ReportingPeriod,
			fmt.Sprintf("%.2f", report.Scope1EmissionsTonne),
			fmt.Sprintf("%.2f", report.Scope3Category4Tonne),
			fmt.Sprintf("%.2f", report.TotalEmissionsTonne),
			fmt.Sprintf("%.2f", report.CarbonIntensityPerKM),
			fmt.Sprintf("%.4f", report.CarbonIntensityTKM),
			fmt.Sprintf("%.2f", report.TotalDistanceKM),
			fmt.Sprintf("%.1f%%", report.GreenFleetSharePct),
		})
		cw.Flush()
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(report)
}
