package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/apperr"
	"transport-app/internal/auth"
	"transport-app/internal/fastag"
	"transport-app/internal/httpx"
	intFastag "transport-app/internal/integration/fastag"
	"transport-app/internal/middleware"
)

// FASTagHandlers manages FASTag wallet queries, transactions, and reconciliations.
type FASTagHandlers struct {
	*App
	svc     *fastag.FASTagService
	authSrv auth.AuthorizationService
}

// NewFASTagHandlers constructs a new FASTagHandlers instance.
func NewFASTagHandlers(app *App, svc *fastag.FASTagService, authSrv auth.AuthorizationService) *FASTagHandlers {
	return &FASTagHandlers{
		App:     app,
		svc:     svc,
		authSrv: authSrv,
	}
}

// Mount mounts FASTag routes.
func (h *FASTagHandlers) Mount(r chi.Router) {
	r.With(middleware.ResourcePermission(h.authSrv, "fastag", "read")).Get("/fastag", h.Index)
	r.With(middleware.ResourcePermission(h.authSrv, "fastag", "read")).Get("/fastag/balance", h.GetBalance)
	r.With(middleware.ResourcePermission(h.authSrv, "fastag", "read")).Get("/fastag/transactions", h.ListTransactions)
	r.With(middleware.ResourcePermission(h.authSrv, "fastag", "update")).Post("/fastag/reconcile", h.Reconcile)
	r.With(middleware.ResourcePermission(h.authSrv, "fastag", "update")).Post("/fastag/deduct", h.Deduct)
}

// Index renders the FASTag management dashboard.
func (h *FASTagHandlers) Index(w http.ResponseWriter, r *http.Request) {
	tags, err := h.svc.ListTags(r.Context())
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "FASTag Error", err.Error(), nil)
		return
	}

	txs, err := h.svc.ListTransactions(r.Context(), "", 30)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "FASTag Error", err.Error(), nil)
		return
	}

	var totalBalance float64
	for _, t := range tags {
		totalBalance += t.Balance
	}

	pendingReconcile := 0
	for _, tx := range txs {
		if !tx.Reconciled {
			pendingReconcile++
		}
	}

	user, _ := h.getUserFromContext(r)
	data := PageData{
		Title: "FASTag Management",
		User:  user,
		Extra: map[string]interface{}{
			"Tags":             tags,
			"Transactions":     txs,
			"TotalTags":        len(tags),
			"TotalBalance":     totalBalance,
			"PendingReconcile": pendingReconcile,
		},
	}
	h.renderPage(w, r, "fastag_index.html", data)
}

// GetBalance returns wallet balance for a vehicle or tag.
func (h *FASTagHandlers) GetBalance(w http.ResponseWriter, r *http.Request) {
	vehicleNumber := r.URL.Query().Get("vehicle_number")
	if vehicleNumber == "" {
		vehicleNumber = r.URL.Query().Get("vehicle")
	}

	bal, err := h.svc.GetBalance(r.Context(), vehicleNumber)
	if err != nil {
		httpx.Error(w, r, apperr.Wrap(apperr.CodeNotFound, err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bal)
}

// ListTransactions returns recent toll deductions.
func (h *FASTagHandlers) ListTransactions(w http.ResponseWriter, r *http.Request) {
	vehicleNumber := r.URL.Query().Get("vehicle_number")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	txs, err := h.svc.ListTransactions(r.Context(), vehicleNumber, limit)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(txs)
}

// Reconcile executes greedy trip matching and auto-kharcha creation.
func (h *FASTagHandlers) Reconcile(w http.ResponseWriter, r *http.Request) {
	vehicleNumber := r.FormValue("vehicle_number")
	fromDate := r.FormValue("from_date")
	toDate := r.FormValue("to_date")

	if vehicleNumber == "" {
		var req struct {
			VehicleNumber string `json:"vehicle_number"`
			FromDate      string `json:"from_date"`
			ToDate        string `json:"to_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			vehicleNumber = req.VehicleNumber
			fromDate = req.FromDate
			toDate = req.ToDate
		}
	}

	res, err := h.svc.Reconcile(r.Context(), vehicleNumber, fromDate, toDate)
	if err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<div class="px-3 py-2 text-xs font-semibold text-status-alert bg-status-alert/10 rounded">` + template.HTMLEscapeString(err.Error()) + `</div>`))
			return
		}
		httpx.Error(w, r, err)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(res)
		return
	}

	if isDatastarRequest(r) {
		w.Header().Set("HX-Trigger", `{"showToast": {"tone":"success","msg":"Reconciliation complete"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		matched := 0
		if res != nil {
			matched = res.Matched
		}
		_, _ = w.Write([]byte(`<div class="px-3 py-2 text-xs font-semibold text-status-success bg-status-success/10 rounded border border-status-success/20">Reconciled ` + template.HTMLEscapeString(vehicleNumber) + ` — ` + strconv.Itoa(matched) + ` events matched</div>`))
		return
	}

	http.Redirect(w, r, "/fastag", http.StatusSeeOther)
}

// Deduct records a toll deduction.
func (h *FASTagHandlers) Deduct(w http.ResponseWriter, r *http.Request) {
	var req intFastag.DeductTollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.VehicleNumber = r.FormValue("vehicle_number")
		req.TagID = r.FormValue("tag_id")
		req.PlazaID = r.FormValue("plaza_id")
		req.PlazaName = r.FormValue("plaza_name")
		amt, _ := strconv.ParseFloat(r.FormValue("amount"), 64)
		req.Amount = amt
		req.TripID = r.FormValue("trip_id")
	}

	if req.VehicleNumber == "" && req.TagID == "" {
		httpx.Error(w, r, apperr.New(apperr.CodeMissingField).
			WithDetail("vehicle_number or tag_id is required"))
		return
	}

	txn, err := h.svc.DeductToll(r.Context(), req)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(txn)
}
