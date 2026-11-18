package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/ewaybill"
	ewaybillapp "transport-app/internal/ewaybill/application"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// EWayBillHandlers handles E-Way Bill lifecycle HTTP requests.
type EWayBillHandlers struct {
	*App
	svc     *ewaybill.EWayBillService
	authSrv auth.AuthorizationService
}

// NewEWayBillHandlers creates a new EWayBillHandlers.
func NewEWayBillHandlers(app *App, svc *ewaybill.EWayBillService, authSrv auth.AuthorizationService) *EWayBillHandlers {
	return &EWayBillHandlers{
		App:     app,
		svc:     svc,
		authSrv: authSrv,
	}
}

// Mount mounts ewaybill routes on the router.
func (h *EWayBillHandlers) Mount(r chi.Router) {
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Post("/trips/{id}/ewaybill", h.GenerateForTrip)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Post("/trips/{id}/ewaybill/generate", h.GenerateForTrip)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/trips/{id}/ewaybill", h.GetForTrip)

	// Standalone EWB management endpoints (Spec 07 §4.2)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/ewaybill", h.List)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/ewaybill/table", h.TableFragment)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Get("/ewaybill/new", h.NewForm)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "create")).Post("/ewaybill/new", h.Create)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "read")).Get("/ewaybill/{ewb}", h.Detail)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "update")).Post("/ewaybill/{ewb}/part-b", h.AttachPartB)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "update")).Post("/ewaybill/{ewb}/extend", h.Extend)
	r.With(middleware.ResourcePermission(h.authSrv, "ewaybill", "update")).Post("/ewaybill/{ewb}/cancel", h.Cancel)
}

func (h *EWayBillHandlers) requestTenant(r *http.Request) string {
	return string(shared.TenantIDFromContext(r.Context()))
}

// tripOwned reports whether the trip belongs to the caller's tenant. Missing
// tenant or a failed lookup is treated as not owned (fail closed).
func (h *EWayBillHandlers) tripOwned(ctx context.Context, tripID, tenantID string) (bool, error) {
	return ewaybillapp.NewTenantOwnershipQueries(h.DB).TripOwned(ctx, tripID, tenantID)
}

// ewbOwned resolves ownership through the bill's trip; eway_bills itself has
// no tenant column.
func (h *EWayBillHandlers) ewbOwned(ctx context.Context, ewbNumber, tenantID string) (bool, error) {
	return ewaybillapp.NewTenantOwnershipQueries(h.DB).EWBOwned(ctx, ewbNumber, tenantID)
}

func (h *EWayBillHandlers) requireTrip(w http.ResponseWriter, r *http.Request, tripID string) bool {
	ok, err := h.tripOwned(r.Context(), tripID, h.requestTenant(r))
	if err != nil {
		http.Error(w, "could not verify trip", http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Error(w, "trip not found", http.StatusNotFound)
		return false
	}
	return true
}

func (h *EWayBillHandlers) requireEWB(w http.ResponseWriter, r *http.Request, ewbNumber string) bool {
	ok, err := h.ewbOwned(r.Context(), ewbNumber, h.requestTenant(r))
	if err != nil {
		http.Error(w, "could not verify e-way bill", http.StatusInternalServerError)
		return false
	}
	if !ok {
		http.Error(w, "E-Way Bill not found", http.StatusNotFound)
		return false
	}
	return true
}

func wantsJSON(r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		return true
	}
	accept := r.Header.Get("Accept")
	contentType := r.Header.Get("Content-Type")
	return strings.Contains(accept, "application/json") || strings.Contains(contentType, "application/json")
}

// EWB page view models live in internal/ewaybill (handlers-end-state ADR,
// Phase 2); the aliases keep template and render-test references unchanged.
type (
	EWBStats       = ewaybill.EWBStats
	EWBListItem    = ewaybill.EWBListItem
	TripOption     = ewaybill.TripOption
	EWBEventRecord = ewaybill.EWBEventRecord
)

func (h *EWayBillHandlers) List(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	// Presentation reads are tenant-scoped via the owning trip (security fix).
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	reads := ewaybill.NewEWayBillReadQueries(h.DB)
	stats, sErr := reads.Stats(r.Context(), tenantID)
	if sErr != nil && sErr != sql.ErrNoRows {
		// Zeroed stats on a failed query is a silent failure - surface it in logs.
		slog.WarnContext(r.Context(), "EWB stats query failed", slog.Any("error", sErr))
	}

	// 2. Fetch list items
	items := reads.Items(r.Context(), tenantID)

	// 3. Fetch candidate trips for new modal
	trips := reads.CandidateTrips(r.Context(), tenantID)

	h.renderPage(w, r, "ewaybill_index.html", PageData{
		Title: "E-Way Bills",
		User:  session,
		Extra: map[string]interface{}{
			"ActiveNav": "ewaybill",
			"Stats":     stats,
			"EWayBills": items,
			"Trips":     trips,
		},
	})
}

func (h *EWayBillHandlers) TableFragment(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	items := ewaybill.NewEWayBillReadQueries(h.DB).Items(r.Context(), tenantID)
	h.renderFragment(w, r, "ewaybill_row.html", map[string]interface{}{"EWayBills": items})
}

func (h *EWayBillHandlers) NewForm(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	trips := ewaybill.NewEWayBillReadQueries(h.DB).CandidateTrips(r.Context(), string(shared.TenantIDFromContext(r.Context())))

	h.renderPage(w, r, "ewaybill_index.html", PageData{
		Title: "Generate E-Way Bill",
		User:  session,
		Extra: map[string]interface{}{
			"ActiveNav": "ewaybill",
			"Trips":     trips,
		},
	})
}

func (h *EWayBillHandlers) Create(w http.ResponseWriter, r *http.Request) {
	tripID := strings.TrimSpace(r.FormValue("trip_id"))
	if tripID == "" {
		http.Error(w, "trip_id required", http.StatusBadRequest)
		return
	}

	var goodsVal float64
	if gv := r.FormValue("goods_value"); gv != "" {
		goodsVal, _ = strconv.ParseFloat(gv, 64)
	}
	force := r.FormValue("force") == "true"

	if !h.requireTrip(w, r, tripID) {
		return
	}
	rec, err := h.svc.GeneratePartA(r.Context(), ewaybill.GeneratePartARequest{
		TripID:     tripID,
		GoodsValue: goodsVal,
		GenMode:    "MANUAL",
		Force:      force,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate EWB: %v", err), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/ewaybill/"+rec.EwbNumber, http.StatusSeeOther)
}

func (h *EWayBillHandlers) Detail(w http.ResponseWriter, r *http.Request) {
	ewbNumber := chi.URLParam(r, "ewb")
	if ewbNumber == "" {
		http.Error(w, "ewb number required", http.StatusBadRequest)
		return
	}

	session, _ := h.getUserFromContext(r)
	record, err := h.svc.GetByNumberForTenant(r.Context(), ewbNumber, string(shared.TenantIDFromContext(r.Context())))
	if err != nil {
		if err == ewaybill.ErrEWBNotFound {
			http.Error(w, "E-Way Bill not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Caller-tenant event history only (fail-closed for foreign EWBs).
	events := ewaybill.NewEWayBillReadQueries(h.DB).Events(r.Context(), ewbNumber, string(shared.TenantIDFromContext(r.Context())))

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ewaybill": record,
			"events":   events,
		})
		return
	}

	h.renderPage(w, r, "ewaybill_detail.html", PageData{
		Title: fmt.Sprintf("EWB %s", record.EwbNumber),
		User:  session,
		Extra: map[string]interface{}{
			"ActiveNav": "ewaybill",
			"EWayBill":  record,
			"Events":    events,
		},
	})
}
