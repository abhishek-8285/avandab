package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/fuel"
	fuelapp "transport-app/internal/fuel/application"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// FuelCardHandlers handles commercial fuel card registration and reconciliation (Spec 20 §2, B10).
type FuelCardHandlers struct {
	*App
	useCase *fuelapp.FuelCardUseCase
}

// NewFuelCardHandlers creates a new FuelCardHandlers.
func NewFuelCardHandlers(app *App, useCase *fuelapp.FuelCardUseCase) *FuelCardHandlers {
	return &FuelCardHandlers{
		App:     app,
		useCase: useCase,
	}
}

// Routes registers web UI routes for fuel cards under /fuel-cards.
func (h *FuelCardHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "read")).Get("/", h.WebListFuelCards)
	r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "write")).Post("/", h.WebRegisterCard)
	r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "write")).Post("/sync", h.WebSyncTransactions)
}

// WebListFuelCards renders the fuel cards management screen.
func (h *FuelCardHandlers) WebListFuelCards(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)
	cards, err := h.useCase.ListCards(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load fuel cards", err.Error(), user)
		return
	}

	activeCount := 0
	var totalSpendToday float64
	for _, c := range cards {
		if strings.EqualFold(string(c.Status), "active") {
			activeCount++
		}
		totalSpendToday += c.SpendToday
	}

	data := PageData{
		Title: "Fuel Cards",
		User:  user,
		Extra: map[string]interface{}{
			"Cards":           cards,
			"ActiveCount":     activeCount,
			"TotalSpendToday": totalSpendToday,
		},
	}
	h.renderPage(w, r, "fuel_cards_list.html", data)
}

// WebRegisterCard handles registration form submissions for new commercial fuel cards.
func (h *FuelCardHandlers) WebRegisterCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	cardNumber := strings.TrimSpace(r.PostFormValue("card_number"))
	provider := strings.TrimSpace(r.PostFormValue("provider"))
	vehicleID := strings.TrimSpace(r.PostFormValue("vehicle_id"))
	driverID := strings.TrimSpace(r.PostFormValue("driver_id"))
	dailyLimitStr := strings.TrimSpace(r.PostFormValue("daily_spend_limit"))

	if cardNumber == "" || provider == "" {
		http.Redirect(w, r, "/fuel-cards?error=missing_fields", http.StatusSeeOther)
		return
	}

	var dailyLimit float64
	if dailyLimitStr != "" {
		if f, err := strconv.ParseFloat(dailyLimitStr, 64); err == nil {
			dailyLimit = f
		}
	}

	var vehPtr *string
	if vehicleID != "" {
		vehPtr = &vehicleID
	}
	var drvPtr *string
	if driverID != "" {
		drvPtr = &driverID
	}

	req := fuel.RegisterFuelCardRequest{
		CardNumber:        cardNumber,
		Provider:          provider,
		AssignedVehicleID: vehPtr,
		AssignedDriverID:  drvPtr,
		DailySpendLimit:   dailyLimit,
	}

	_, err := h.useCase.RegisterCard(r.Context(), tenantID, req)
	if err != nil {
		h.renderError(w, http.StatusBadRequest, "Failed to register fuel card", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/fuel-cards?registered=1", http.StatusSeeOther)
}

// WebSyncTransactions triggers OMC settlement ingest and reconciliation from the web UI.
func (h *FuelCardHandlers) WebSyncTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	_, err := h.useCase.SyncTransactions(r.Context(), tenantID, fuel.SyncFuelTransactionsRequest{})
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Sync Failed", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/fuel-cards?synced=1", http.StatusSeeOther)
}

// RegisterAPIRoutes registers /api/v1/fuel-cards/* REST endpoints.
func (h *FuelCardHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/fuel-cards", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "read")).Get("/", h.ListCards)
		r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "write")).Post("/", h.RegisterCard)
		r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "write")).Post("/transactions/sync", h.SyncTransactions)
		r.With(middleware.RequirePermission(h.AuthSrv, "fuel", "write")).Post("/transactions/{id}/reconcile", h.ReconcileTransaction)
	})
}

// requireTenant resolves the acting org. Fail closed with 401: fuel-card
// routes sit behind RequirePermission which sets tenant; a missing tenant
// means a bad/expired session, never a 500 panic.
func (h *FuelCardHandlers) requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid, err := shared.TenantRequired(r.Context())
	if err != nil {
		http.Error(w, `{"error":"tenant required"}`, http.StatusUnauthorized)
		return "", false
	}
	return string(tid), true
}

// RegisterCard registers a new fuel card and binds it to vehicle/driver (POST /api/v1/fuel-cards).
func (h *FuelCardHandlers) RegisterCard(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	var req fuel.RegisterFuelCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	card, err := h.useCase.RegisterCard(r.Context(), tenantID, req)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(card)
}

// ListCards lists all registered fuel cards with spend metrics (GET /api/v1/fuel-cards).
func (h *FuelCardHandlers) ListCards(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	cards, err := h.useCase.ListCards(r.Context(), tenantID)
	if err != nil {
		http.Error(w, `{"error":"failed to load fuel cards"}`, http.StatusInternalServerError)
		return
	}

	if cards == nil {
		cards = []fuel.FuelCard{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cards)
}

// SyncTransactions ingests OMC statement feeds and runs automated reconciliation (POST /api/v1/fuel-cards/transactions/sync).
func (h *FuelCardHandlers) SyncTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}

	var req fuel.SyncFuelTransactionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	res, err := h.useCase.SyncTransactions(r.Context(), tenantID, req)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// ReconcileTransaction manually matches a transaction against an expense (POST /api/v1/fuel-cards/transactions/{id}/reconcile).
func (h *FuelCardHandlers) ReconcileTransaction(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	txnID := chi.URLParam(r, "id")
	if txnID == "" {
		http.Error(w, `{"error":"transaction id required"}`, http.StatusBadRequest)
		return
	}

	var req fuel.ReconcileTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	txn, err := h.useCase.ReconcileTransaction(r.Context(), tenantID, txnID, req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, `{"error":"transaction not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(txn)
}
