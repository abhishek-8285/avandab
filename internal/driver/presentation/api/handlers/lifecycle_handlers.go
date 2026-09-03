package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/driver/application"
	"transport-app/internal/shared"
)

type DriverLifecycleAPIHandler struct {
	appService *application.DriverAppService
}

func NewDriverLifecycleAPIHandler(appService *application.DriverAppService) *DriverLifecycleAPIHandler {
	return &DriverLifecycleAPIHandler{appService: appService}
}

func (h *DriverLifecycleAPIHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/drivers", func(r chi.Router) {
		r.Get("/me/onboarding", h.GetOnboarding)
		r.Post("/me/license", h.SubmitLicense)
		r.Post("/me/documents", h.SubmitDocument)
		r.Post("/me/vehicle-claims", h.ClaimVehicle)
		r.Post("/me/payout-account", h.SubmitPayoutAccount)
		r.Post("/me/push-token", h.RegisterPushToken)
		r.Post("/push-token", h.RegisterPushToken)
		r.Get("/me/offers", h.GetPendingOffers)
		r.Post("/me/commands", h.ProcessCommand)

		r.Post("/{id}/verify", h.VerifyDriverLicense)
		r.Post("/{id}/vehicle-claims/{claimId}/verify", h.VerifyVehicleClaim)
		r.Post("/{id}/vehicle-assignments", h.AssignVehicle)
	})

	r.Post("/api/v1/driver/push-token", h.RegisterPushToken)
	r.Post("/api/v1/driver/me/push-token", h.RegisterPushToken)

	r.Route("/api/v1/telemetry", func(r chi.Router) {
		r.Post("/sessions/start", h.StartTelemetrySession)
		r.Post("/sessions/end", h.EndTelemetrySession)
		r.Post("/events", h.IngestTelemetryEvent)
	})

	r.Post("/api/v1/vehicle-assignments/{id}/accept", h.AcceptAssignment)
}

func (h *DriverLifecycleAPIHandler) getContextData(r *http.Request) (string, string, bool) {
	ctx := r.Context()
	tenantID := string(shared.TenantIDFromContext(ctx))
	if tenantID == "" {
		tenantID = string(shared.DefaultTenant)
	}

	session, ok := ctx.Value(auth.ContextUser).(*auth.SessionData)
	if !ok || session == nil || session.UserID == "" {
		return "", "", false
	}
	return tenantID, session.UserID, true
}

func (h *DriverLifecycleAPIHandler) GetOnboarding(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	state, err := h.appService.GetOnboardingState(r.Context(), tenantID, userID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(state)
}

func (h *DriverLifecycleAPIHandler) SubmitLicense(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		LicenseNumber    string   `json:"license_number"`
		IssuingAuthority string   `json:"issuing_authority"`
		IssuedOn         string   `json:"issued_on"`
		ExpiresOn        string   `json:"expires_on"`
		Classes          []string `json:"classes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	exp, err := time.Parse("2006-01-02", req.ExpiresOn)
	if err != nil {
		exp = time.Now().Add(5 * 365 * 24 * time.Hour)
	}

	issued := time.Now().Add(-365 * 24 * time.Hour)
	if req.IssuedOn != "" {
		if t, err := time.Parse("2006-01-02", req.IssuedOn); err == nil {
			issued = t
		}
	}

	if len(req.Classes) == 0 {
		req.Classes = []string{"LMV"}
	}

	err = h.appService.SubmitLicense(r.Context(), tenantID, userID, req.LicenseNumber, req.IssuingAuthority, issued, exp, req.Classes)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "license submitted for verification",
	})
}

func (h *DriverLifecycleAPIHandler) SubmitDocument(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		DocumentType  string `json:"document_type"`
		StorageKey    string `json:"storage_key"`
		MimeType      string `json:"mime_type"`
		FileSizeBytes int64  `json:"file_size_bytes"`
		DocumentHash  string `json:"document_hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	docID, err := h.appService.SubmitDocument(r.Context(), tenantID, userID, req.DocumentType, req.StorageKey, req.MimeType, req.FileSizeBytes, req.DocumentHash)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"document_id": docID,
	})
}

func (h *DriverLifecycleAPIHandler) ClaimVehicle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		RegistrationNumber string  `json:"registration_number"`
		RCDocumentID       *string `json:"rc_document_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	claimID, err := h.appService.ClaimVehicle(r.Context(), tenantID, userID, req.RegistrationNumber, req.RCDocumentID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"claim_id": claimID,
	})
}

func (h *DriverLifecycleAPIHandler) SubmitPayoutAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		AccountHolderName string `json:"account_holder_name"`
		AccountNumber     string `json:"account_number"`
		IFSCCode          string `json:"ifsc_code"`
		BankName          string `json:"bank_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	accID, err := h.appService.SubmitPayoutAccount(r.Context(), tenantID, userID, req.AccountHolderName, req.AccountNumber, req.IFSCCode, req.BankName)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"account_id": accID,
	})
}

func (h *DriverLifecycleAPIHandler) SubmitForVerification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	err := h.appService.SubmitForVerification(r.Context(), tenantID, userID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "onboarding application submitted for operational verification",
	})
}

func (h *DriverLifecycleAPIHandler) VerifyDriverLicense(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, reviewerID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		LicenseID string `json:"license_id"`
		Approve   bool   `json:"approve"`
		Reason    string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	err := h.appService.ReviewDriverLicense(r.Context(), tenantID, req.LicenseID, reviewerID, req.Approve, req.Reason)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (h *DriverLifecycleAPIHandler) VerifyVehicleClaim(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, reviewerID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	claimID := chi.URLParam(r, "claimId")
	var req struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	err := h.appService.ReviewVehicleClaim(r.Context(), tenantID, claimID, reviewerID, req.Approve, req.Reason)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (h *DriverLifecycleAPIHandler) AssignVehicle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, assignerID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	driverID := chi.URLParam(r, "id")
	var req struct {
		VehicleID      string `json:"vehicle_id"`
		AssignmentType string `json:"assignment_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	if req.AssignmentType == "" {
		req.AssignmentType = "company_assigned"
	}

	asgID, err := h.appService.AssignVehicleToDriver(r.Context(), tenantID, driverID, req.VehicleID, assignerID, req.AssignmentType)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"assignment_id": asgID,
	})
}

func (h *DriverLifecycleAPIHandler) AcceptAssignment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = chi.URLParam(r, "id")

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "assignment accepted",
	})
}

func (h *DriverLifecycleAPIHandler) StartTelemetrySession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, driverID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		InstallationID string `json:"installation_id"`
		AppVersion     string `json:"app_version"`
		OSVersion      string `json:"os_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	sess, err := h.appService.StartTelemetrySession(r.Context(), tenantID, driverID, req.InstallationID, req.AppVersion, req.OSVersion)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(sess)
}

func (h *DriverLifecycleAPIHandler) EndTelemetrySession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, driverID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	err := h.appService.EndTelemetrySession(r.Context(), tenantID, driverID, req.SessionID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func (h *DriverLifecycleAPIHandler) IngestTelemetryEvent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, driverID, ok := h.getContextData(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(application.TelemetryIngestResponse{
			Status:  application.StatusUnauthorized,
			Message: "Authentication required",
		})
		return
	}

	var req application.TelemetryIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(application.TelemetryIngestResponse{
			Status:  application.StatusInvalidCoordinates,
			Message: "Invalid JSON body",
		})
		return
	}

	resp, err := h.appService.IngestTelemetryEvent(r.Context(), tenantID, driverID, req)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *DriverLifecycleAPIHandler) GetPendingOffers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, driverID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	offers, err := h.appService.GetPendingOffersForDriver(r.Context(), tenantID, driverID)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	if offers == nil {
		offers = []application.DispatchOfferDTO{}
	}

	_ = json.NewEncoder(w).Encode(offers)
}

func (h *DriverLifecycleAPIHandler) ProcessCommand(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, driverID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req application.DriverCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	resp, err := h.appService.ProcessDriverCommand(r.Context(), tenantID, driverID, req)
	if err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *DriverLifecycleAPIHandler) RegisterPushToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	tenantID, userID, ok := h.getContextData(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req struct {
		DriverID      string `json:"driver_id"`
		DeviceID      string `json:"device_id"`
		PushToken     string `json:"push_token"`
		ExpoPushToken string `json:"expo_push_token"`
		Token         string `json:"token"`
		Platform      string `json:"platform"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	pushToken := req.PushToken
	if pushToken == "" {
		pushToken = req.ExpoPushToken
	}
	if pushToken == "" {
		pushToken = req.Token
	}

	if pushToken == "" {
		http.Error(w, `{"error":"push_token is required"}`, http.StatusBadRequest)
		return
	}

	platform := req.Platform
	if platform == "" {
		platform = "android"
	}

	deviceID := req.DeviceID
	if deviceID == "" {
		deviceID = platform + "-" + userID
	}

	driverID := req.DriverID
	if driverID == "" {
		driverID = userID
	}

	err := h.appService.RegisterPushToken(r.Context(), tenantID, driverID, userID, deviceID, pushToken, platform)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "push token registered",
	})
}
