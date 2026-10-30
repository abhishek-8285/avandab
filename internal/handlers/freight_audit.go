package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/freightaudit"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type FreightAuditHandlers struct {
	*App
}

func (h *FreightAuditHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/freight-audit", func(r chi.Router) {
		r.Route("/rates", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/", h.APIListRateCards)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/", h.APICreateRateCard)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/{id}", h.APIGetRateCard)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Put("/{id}", h.APIUpdateRateCard)
		})

		r.Route("/audits", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/", h.APIListAudits)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/", h.APICreateAudit)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/{id}", h.APIGetAudit)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/{id}/approve-deduction", h.APIApproveDeduction)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/{id}/dispute", h.APIDisputeAudit)
			r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/{id}/settle", h.APISettleAudit)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/summary", h.APIGetSummary)
	})
}

// Routes mounts the Freight Audit web UI handlers at /freight-audit.
func (h *FreightAuditHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "read")).Get("/", h.WebFreightAuditDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/rates", h.WebCreateRateCard)
	r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/audits", h.WebCreateAudit)
	r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/audits/{id}/approve-deduction", h.WebApproveDeduction)
	r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/audits/{id}/dispute", h.WebDisputeAudit)
	r.With(middleware.RequirePermission(h.AuthSrv, "freight_audit", "write")).Post("/audits/{id}/settle", h.WebSettleAudit)
}

// WebFreightAuditDashboard renders the overbilling variances, tariff contracts, and debit notes.
func (h *FreightAuditHandlers) WebFreightAuditDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	summary, err := h.service().GetSummary(r.Context(), tenantID)
	if err != nil {
		summary = &freightaudit.AuditSummary{}
	}

	statusFilter := r.URL.Query().Get("status")
	searchFilter := r.URL.Query().Get("search")

	audits, _, err := h.service().ListAudits(r.Context(), tenantID, freightaudit.AuditFilter{
		Status: statusFilter,
		Search: searchFilter,
		Limit:  50,
	})
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load audits", err.Error(), user)
		return
	}

	rateCards, _, _ := h.service().ListRateCards(r.Context(), tenantID, freightaudit.RateCardFilter{
		Limit: 50,
	})

	// Fetch recent trips for audit creation dropdown
	rowsT, err := h.DB.QueryContext(r.Context(), `SELECT id, trip_number FROM trips WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 50`, tenantID)
	var trips []map[string]string
	if err == nil {
		defer func() { _ = rowsT.Close() }()
		for rowsT.Next() {
			var id, tripNum string
			if err := rowsT.Scan(&id, &tripNum); err == nil {
				trips = append(trips, map[string]string{
					"ID":         id,
					"TripNumber": tripNum,
				})
			}
		}
	}

	data := PageData{
		Title: "Freight Audit & Overbilling",
		User:  user,
		Extra: map[string]interface{}{
			"Summary":      summary,
			"Audits":       audits,
			"RateCards":    rateCards,
			"Trips":        trips,
			"StatusFilter": statusFilter,
			"Search":       searchFilter,
		},
	}

	h.renderPage(w, r, "freight_audit_dashboard.html", data)
}

// WebCreateRateCard handles form submission for new tariff contracts.
func (h *FreightAuditHandlers) WebCreateRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	contractNum := strings.TrimSpace(r.FormValue("contract_number"))
	name := strings.TrimSpace(r.FormValue("name"))
	if contractNum == "" || name == "" {
		h.renderError(w, http.StatusBadRequest, "Contract Number and Name required", "contract_number and name cannot be blank", user)
		return
	}

	basis := freightaudit.BillingBasis(r.FormValue("billing_basis"))
	if !basis.IsValid() {
		basis = freightaudit.BillingBasisPerKM
	}

	rate, _ := strconv.ParseFloat(r.FormValue("rate"), 64)
	minCharge, _ := strconv.ParseFloat(r.FormValue("min_charge"), 64)
	freeDetention, _ := strconv.ParseFloat(r.FormValue("free_detention_hours"), 64)
	detentionRate, _ := strconv.ParseFloat(r.FormValue("detention_rate_per_hour"), 64)

	in := freightaudit.CreateRateCardInput{
		ContractNumber:       contractNum,
		Name:                 name,
		VehicleType:          strings.TrimSpace(r.FormValue("vehicle_type")),
		BillingBasis:         basis,
		Rate:                 rate,
		MinCharge:            minCharge,
		FreeDetentionHours:   freeDetention,
		DetentionRatePerHour: detentionRate,
	}

	if _, err := h.service().CreateRateCard(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create rate card", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/freight-audit", http.StatusSeeOther)
}

// WebCreateAudit handles form submission to audit a billed invoice against GPS and contract.
func (h *FreightAuditHandlers) WebCreateAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	vendorName := strings.TrimSpace(r.FormValue("vendor_name"))
	vehNum := strings.TrimSpace(r.FormValue("vehicle_number"))
	if vendorName == "" || vehNum == "" {
		h.renderError(w, http.StatusBadRequest, "Vendor and Vehicle required", "Vendor name and vehicle number cannot be blank", user)
		return
	}

	billedDist, _ := strconv.ParseFloat(r.FormValue("billed_distance_km"), 64)
	billedWt, _ := strconv.ParseFloat(r.FormValue("billed_weight_kg"), 64)
	billedRate, _ := strconv.ParseFloat(r.FormValue("billed_rate"), 64)
	billedTotal, _ := strconv.ParseFloat(r.FormValue("billed_total"), 64)

	var gpsDist *float64
	if v, err := strconv.ParseFloat(r.FormValue("gps_distance_km"), 64); err == nil && v > 0 {
		gpsDist = &v
	}
	var actWt *float64
	if v, err := strconv.ParseFloat(r.FormValue("actual_weight_kg"), 64); err == nil && v > 0 {
		actWt = &v
	}
	var audRate *float64
	if v, err := strconv.ParseFloat(r.FormValue("audited_rate"), 64); err == nil && v > 0 {
		audRate = &v
	}

	var tripID *string
	if tid := strings.TrimSpace(r.FormValue("trip_id")); tid != "" {
		tripID = &tid
	}

	in := freightaudit.CreateAuditInput{
		TripID:            tripID,
		VendorName:        vendorName,
		VehicleNumber:     vehNum,
		BilledDistanceKM:  billedDist,
		BilledWeightKG:    billedWt,
		BilledRate:        billedRate,
		BilledBaseFreight: billedDist * billedRate,
		BilledTotal:       billedTotal,
		GPSDistanceKM:     gpsDist,
		ActualWeightKG:    actWt,
		AuditedRate:       audRate,
		Notes:             strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().CreateAudit(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create freight audit", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/freight-audit", http.StatusSeeOther)
}

// WebApproveDeduction approves identified overbilling deductions and generates debit note.
func (h *FreightAuditHandlers) WebApproveDeduction(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	approver := "Audit Manager"
	if user != nil && user.UserID != "" {
		approver = user.UserID
	}

	var dedAmount *float64
	if v, err := strconv.ParseFloat(r.FormValue("deduction_amount"), 64); err == nil && v > 0 {
		dedAmount = &v
	}

	in := freightaudit.ApproveDeductionInput{
		DeductionAmount: dedAmount,
		Notes:           strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().ApproveDeduction(r.Context(), tenantID, id, approver, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to approve deduction", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/freight-audit", http.StatusSeeOther)
}

// WebDisputeAudit marks discrepancy as under carrier dispute.
func (h *FreightAuditHandlers) WebDisputeAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	disputer := "Carrier Liaison"
	if user != nil && user.UserID != "" {
		disputer = user.UserID
	}

	in := freightaudit.DisputeAuditInput{
		Reason: strings.TrimSpace(r.FormValue("reason")),
	}

	if _, err := h.service().DisputeAudit(r.Context(), tenantID, id, disputer, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to dispute audit", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/freight-audit", http.StatusSeeOther)
}

// WebSettleAudit closes and marks the audit invoice as settled.
func (h *FreightAuditHandlers) WebSettleAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	settler := "Finance Officer"
	if user != nil && user.UserID != "" {
		settler = user.UserID
	}

	in := freightaudit.SettleAuditInput{
		Notes: strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().SettleAudit(r.Context(), tenantID, id, settler, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to settle audit", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/freight-audit", http.StatusSeeOther)
}

func writeFreightAuditJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeFreightAuditError(w http.ResponseWriter, code int, msg string) {
	writeFreightAuditJSON(w, code, map[string]string{"error": msg})
}

func (h *FreightAuditHandlers) service() *freightaudit.Service {
	return freightaudit.NewService(freightaudit.NewRepository(h.DB))
}

// Rate Card Handlers

func (h *FreightAuditHandlers) APIListRateCards(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := freightaudit.RateCardFilter{
		RouteID:     r.URL.Query().Get("route_id"),
		VehicleType: r.URL.Query().Get("vehicle_type"),
		ActiveOnly:  r.URL.Query().Get("active_only") == "true",
		Search:      r.URL.Query().Get("search"),
		Limit:       limit,
		Offset:      offset,
	}

	cards, total, err := h.service().ListRateCards(r.Context(), tenantID, filter)
	if err != nil {
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to query rate cards: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, map[string]interface{}{
		"rate_cards": cards,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}

func (h *FreightAuditHandlers) APICreateRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in freightaudit.CreateRateCardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	card, err := h.service().CreateRateCard(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, freightaudit.ErrContractNumberExists) {
			writeFreightAuditError(w, http.StatusConflict, "contract number already exists")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to create rate card: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusCreated, card)
}

func (h *FreightAuditHandlers) APIGetRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	card, err := h.service().GetRateCard(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "rate card not found")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to fetch rate card: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, card)
}

func (h *FreightAuditHandlers) APIUpdateRateCard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in freightaudit.UpdateRateCardInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	card, err := h.service().UpdateRateCard(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "rate card not found")
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to update rate card: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, card)
}

// Audit Handlers

func (h *FreightAuditHandlers) APIListAudits(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := freightaudit.AuditFilter{
		Status:          r.URL.Query().Get("status"),
		TripID:          r.URL.Query().Get("trip_id"),
		InvoiceID:       r.URL.Query().Get("invoice_id"),
		DiscrepancyOnly: r.URL.Query().Get("discrepancy_only") == "true",
		Search:          r.URL.Query().Get("search"),
		Limit:           limit,
		Offset:          offset,
	}

	audits, total, err := h.service().ListAudits(r.Context(), tenantID, filter)
	if err != nil {
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to query freight audits: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, map[string]interface{}{
		"audits": audits,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *FreightAuditHandlers) APICreateAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in freightaudit.CreateAuditInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	audit, err := h.service().CreateAudit(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to create freight audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusCreated, audit)
}

func (h *FreightAuditHandlers) APIGetAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	audit, err := h.service().GetAudit(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to fetch freight audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func getAuditUser(r *http.Request, defaultVal string) string {
	if sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil && sess.UserID != "" {
		return sess.UserID
	}
	return defaultVal
}

func (h *FreightAuditHandlers) APIApproveDeduction(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	user := getAuditUser(r, "dispatcher")

	var in freightaudit.ApproveDeductionInput
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}

	audit, err := h.service().ApproveDeduction(r.Context(), tenantID, id, user, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidStatus) {
			writeFreightAuditError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidInput) {
			writeFreightAuditError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to approve deduction: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func (h *FreightAuditHandlers) APIDisputeAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	user := getAuditUser(r, "carrier")

	var in freightaudit.DisputeAuditInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeFreightAuditError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		writeFreightAuditError(w, http.StatusBadRequest, "dispute reason is required")
		return
	}

	audit, err := h.service().DisputeAudit(r.Context(), tenantID, id, user, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		if errors.Is(err, freightaudit.ErrInvalidStatus) {
			writeFreightAuditError(w, http.StatusConflict, err.Error())
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to dispute audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func (h *FreightAuditHandlers) APISettleAudit(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	user := getAuditUser(r, "finance")

	var in freightaudit.SettleAuditInput
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}

	audit, err := h.service().SettleAudit(r.Context(), tenantID, id, user, in)
	if err != nil {
		if errors.Is(err, freightaudit.ErrNotFound) {
			writeFreightAuditError(w, http.StatusNotFound, "freight audit not found")
			return
		}
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to settle audit: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, audit)
}

func (h *FreightAuditHandlers) APIGetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	summary, err := h.service().GetSummary(r.Context(), tenantID)
	if err != nil {
		writeFreightAuditError(w, http.StatusInternalServerError, "failed to get freight audit summary: "+err.Error())
		return
	}

	writeFreightAuditJSON(w, http.StatusOK, summary)
}
