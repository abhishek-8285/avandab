package handlers

import (
	"encoding/json"
	"net/http"
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
