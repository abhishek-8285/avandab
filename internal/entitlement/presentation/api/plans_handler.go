package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/entitlement/application"
	"transport-app/internal/entitlement/domain"
)

// PlansHandler serves the commercial plan catalog.
type PlansHandler struct {
	svc application.Service
}

// NewPlansHandler builds a PlansHandler.
func NewPlansHandler(svc application.Service) *PlansHandler {
	return &PlansHandler{svc: svc}
}

// ListPlans handles GET /api/v1/plans — public catalog for checkout/comparison.
func (h *PlansHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.svc.ListPlans(r.Context())
	if err != nil {
		http.Error(w, `{"error":"failed to list plans"}`, http.StatusInternalServerError)
		return
	}
	if plans == nil {
		plans = []domain.Plan{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"plans": plans})
}

// UpdatePlanPrice handles PUT /api/v1/plans/{id} — live pricing without a
// migration. Admin-gated at the route (users:manage).
func (h *PlansHandler) UpdatePlanPrice(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		MonthlyPriceINR *float64 `json:"monthly_price_inr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.MonthlyPriceINR == nil {
		http.Error(w, `{"error":"monthly_price_inr is required"}`, http.StatusBadRequest)
		return
	}
	if *req.MonthlyPriceINR < 0 {
		http.Error(w, `{"error":"monthly_price_inr cannot be negative"}`, http.StatusBadRequest)
		return
	}
	if err := h.svc.UpdatePlanPrice(r.Context(), domain.PlanID(id), *req.MonthlyPriceINR); err != nil {
		if err == domain.ErrPlanNotFound {
			http.Error(w, `{"error":"plan not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"failed to update price"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"plan_id": id, "monthly_price_inr": *req.MonthlyPriceINR})
}
