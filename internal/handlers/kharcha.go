package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
)

// KharchaHandlers handles the driver expense (kharcha) approval dashboard.
type KharchaHandlers struct {
	*App
}

func (h *KharchaHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/", h.Dashboard)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/pending", h.PendingQueue)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/ledger", h.Ledger)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/create", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/approve", h.Approve)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/reject", h.Reject)
}

// GET /kharcha — full dashboard (pending queue + ledger).
func (h *KharchaHandlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	ctx := r.Context()

	pending, _ := h.Services.Kharcha.ListPendingExpenses(ctx)
	ledger, _ := h.Services.Kharcha.ListLedger(ctx, "")
	stats, _ := h.Services.Kharcha.GetKharchaStats(ctx)
	trips, _, _ := h.Services.Trips.ListTrips(ctx, "", "in_transit", 100, 0)
	drivers, _, _ := h.Services.Drivers.ListDrivers(ctx, "", "", 1000, 0)

	h.renderPage(w, r, "kharcha_dashboard.html", PageData{
		Title: "Kharcha Ledger",
		User:  session,
		Extra: map[string]interface{}{
			"PendingExpenses": pending,
			"LedgerEntries":   ledger,
			"Stats":           stats,
			"ActiveTrips":     trips,
			"Drivers":         drivers,
		},
	})
}

// GET /kharcha/pending — HTMX partial: live-refresh the queue every 30s.
func (h *KharchaHandlers) PendingQueue(w http.ResponseWriter, r *http.Request) {
	pending, _ := h.Services.Kharcha.ListPendingExpenses(r.Context())
	h.renderFragment(w, "kharcha_queue.html", map[string]interface{}{
		"PendingExpenses": pending,
	})
}

// GET /kharcha/ledger?trip_id= — HTMX partial: filtered ledger rows.
func (h *KharchaHandlers) Ledger(w http.ResponseWriter, r *http.Request) {
	tripID := r.URL.Query().Get("trip_id")
	entries, _ := h.Services.Kharcha.ListLedger(r.Context(), tripID)
	h.renderFragment(w, "kharcha_ledger_rows.html", map[string]interface{}{
		"LedgerEntries": entries,
	})
}

// POST /kharcha/create — create a new driver expense claim (web form).
// Fuel claims capture fuel_litres for the audit flow (Spec 03 §3.2 step 1).
func (h *KharchaHandlers) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, ok := h.getUserFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	tripID := r.FormValue("trip_id")
	driverID := r.FormValue("driver_id")
	category := r.FormValue("category")
	description := r.FormValue("description")
	receiptURL := r.FormValue("receipt_url")

	var amount float64
	_, _ = fmt.Sscanf(r.FormValue("amount"), "%f", &amount)

	var fuelLitres float64
	if category == "fuel" {
		_, _ = fmt.Sscanf(r.FormValue("fuel_litres"), "%f", &fuelLitres)
	}
	idemKey := r.FormValue("idempotency_key")

	expenseID, err := h.Services.Kharcha.CreateExpense(ctx, tripID, driverID, category, amount, description, receiptURL, fuelLitres, idemKey)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 bg-red-50 text-red-600 text-sm font-semibold border-l-4 border-red-500">Error: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}

	// If a fuel claim, queue it for the audit pass immediately.
	if category == "fuel" && h.Services.FuelAudit != nil {
		_, _ = h.Services.FuelAudit.AuditPendingClaims(ctx)
	}

	if isDatastarRequest(r) {
		expense, err := h.Services.Kharcha.GetExpenseByID(ctx, expenseID)
		if err == nil {
			h.renderFragment(w, "kharcha_row_approved.html", expense)
			return
		}
	}

	http.Redirect(w, r, "/kharcha", http.StatusSeeOther)
}

// CreateExpenseAPI handles driver expense claims from the mobile app
// (Spec 13): POST /api/v1/kharcha/expense with Bearer auth.
// Multipart fields mirror ExpenseScreen.tsx: trip_id, type/expense_type,
// amount, notes, receipt_photo (file), latitude/longitude (accepted but not
// persisted — driver_expenses has no geo columns yet).
func (h *KharchaHandlers) CreateExpenseAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		writePODJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writePODJSONError(w, "form parse error", http.StatusBadRequest)
		return
	}

	tripID := r.FormValue("trip_id")
	category := r.FormValue("expense_type")
	if category == "" {
		category = r.FormValue("type")
	}
	description := r.FormValue("notes")

	amount, err := strconv.ParseFloat(r.FormValue("amount"), 64)
	if err != nil {
		writePODJSONError(w, "invalid amount", http.StatusBadRequest)
		return
	}

	// Resolve the acting driver from the authenticated user (same mapping
	// as DeliverWithPOD): drivers.id or drivers.driver_id via user email.
	driverID := session.UserID
	if h.DB != nil {
		var dID string
		_ = h.DB.QueryRowContext(ctx, `
			SELECT id FROM drivers
			WHERE id = ? OR email = (SELECT email FROM users WHERE id = ?)
			LIMIT 1
		`, session.UserID, session.UserID).Scan(&dID)
		if dID != "" {
			driverID = dID
		}
	}

	var receiptURL string
	if _, fh, fErr := r.FormFile("receipt_photo"); fErr == nil {
		fileRec, saveErr := h.Services.Files.UploadFile(ctx, fh, "expense_receipt", tripID)
		if saveErr != nil {
			writePODJSONError(w, "file upload failed: "+saveErr.Error(), http.StatusBadRequest)
			return
		}
		receiptURL = "/files/" + string(fileRec.ID)
	}

	opts := service.CreateExpenseOpts{
		TripID:         tripID,
		DriverID:       driverID,
		Category:       category,
		Amount:         amount,
		Description:    description,
		ReceiptURL:     receiptURL,
		IdempotencyKey: r.FormValue("idempotency_key"),
	}
	if v, pErr := strconv.ParseFloat(r.FormValue("latitude"), 64); pErr == nil {
		opts.Latitude = &v
	}
	if v, pErr := strconv.ParseFloat(r.FormValue("longitude"), 64); pErr == nil {
		opts.Longitude = &v
	}

	expenseID, err := h.Services.Kharcha.CreateExpenseWithOpts(ctx, opts)
	if err != nil {
		writePODJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Fuel claims enter the audit queue immediately (parity with web create).
	if category == "fuel" && h.Services.FuelAudit != nil {
		_, _ = h.Services.FuelAudit.AuditPendingClaims(ctx)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "created",
		"id":         expenseID,
		"expense_id": expenseID,
	})
}

// POST /kharcha/{id}/approve — HTMX inline swap: approve and replace the row.
func (h *KharchaHandlers) Approve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	expenseID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.Services.Kharcha.ApproveExpense(ctx, expenseID, session.UserID); err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 bg-red-50 text-red-600 text-sm font-semibold border-l-4 border-red-500">Error: %s</div>`, template.HTMLEscapeString(err.Error()))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 bg-red-50 text-red-600 text-sm font-semibold border-l-4 border-red-500">Error: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}

	expense, err := h.Services.Kharcha.GetExpenseByID(ctx, expenseID)
	if err != nil {
		// Silently replace row with approved confirmation
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 flex items-center gap-3 bg-emerald-50/60"><span class="w-2 h-2 rounded-full bg-emerald-500"></span><span class="text-sm font-semibold text-emerald-700">Expense approved successfully.</span></div>`)
		return
	}

	// htmx 4 hx-partial: row morph + badge/KPI morph + toast trigger in one response
	if isDatastarRequest(r) {
		stats, _ := h.Services.Kharcha.GetKharchaStats(ctx)
		pending, _ := h.Services.Kharcha.ListPendingExpenses(ctx)
		w.Header().Set("HX-Trigger", `{"showToast":{"tone":"success","msg":"Expense approved"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// row
		_ = h.Templates.ExecuteTemplate(w, "kharcha_row_approved.html", expense)
		// OOB partials via htmx 4 <template hx type="partial">
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kharcha-queue-count" hx-swap="innerMorph">%d waiting</template>`, len(pending))
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kpi-pending-count" hx-swap="innerMorph">%d</template>`, stats.PendingCount)
		return
	}

	h.renderFragment(w, "kharcha_row_approved.html", expense)
}

// POST /kharcha/{id}/reject — form post with reason field.
func (h *KharchaHandlers) Reject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	expenseID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	reason := r.FormValue("reason")

	if err := h.Services.Kharcha.RejectExpense(ctx, expenseID, session.UserID, reason); err != nil {
		if isDatastarRequest(r) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 bg-red-50 text-red-600 text-sm font-semibold border-l-4 border-red-500">Error: %s</div>`, template.HTMLEscapeString(err.Error()))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 bg-red-50 text-red-600 text-sm font-semibold border-l-4 border-red-500">Error: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}

	expense, err := h.Services.Kharcha.GetExpenseByID(ctx, expenseID)
	if err != nil {
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 flex items-center gap-3 bg-rose-50/60"><span class="w-2 h-2 rounded-full bg-rose-500"></span><span class="text-sm font-semibold text-rose-700">Expense rejected.</span></div>`)
		return
	}

	if isDatastarRequest(r) {
		stats, _ := h.Services.Kharcha.GetKharchaStats(ctx)
		pending, _ := h.Services.Kharcha.ListPendingExpenses(ctx)
		w.Header().Set("HX-Trigger", `{"showToast":{"tone":"success","msg":"Expense rejected"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = h.Templates.ExecuteTemplate(w, "kharcha_row_rejected.html", expense)
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kharcha-queue-count" hx-swap="innerMorph">%d waiting</template>`, len(pending))
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kpi-pending-count" hx-swap="innerMorph">%d</template>`, stats.PendingCount)
		return
	}

	h.renderFragment(w, "kharcha_row_rejected.html", expense)
}

func writePODJSONError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// DeliverWithPOD handles e-POD submission from driver mobile (multipart form).
// Spec 13 §2.4: POST /api/v1/trips/{id}/deliver-pod and POST /trips/{id}/deliver-pod
func (h *KharchaHandlers) DeliverWithPOD(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tripID := chi.URLParam(r, "id")

	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		writePODJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writePODJSONError(w, "form parse error", http.StatusBadRequest)
		return
	}

	trip, err := h.Services.Trips.GetTrip(ctx, domain.TripID(tripID))
	if err != nil {
		writePODJSONError(w, "trip not found", http.StatusNotFound)
		return
	}

	if trip.DriverID == nil {
		writePODJSONError(w, "forbidden: trip is not assigned to this driver", http.StatusForbidden)
		return
	}

	assignedDriverID := string(*trip.DriverID)
	driverMatches := assignedDriverID == session.UserID
	if !driverMatches && h.DB != nil {
		var dID, dCode string
		_ = h.DB.QueryRowContext(ctx, `
			SELECT id, driver_id FROM drivers
			WHERE id = ? OR email = (SELECT email FROM users WHERE id = ?)
			LIMIT 1
		`, session.UserID, session.UserID).Scan(&dID, &dCode)
		if (dID != "" && assignedDriverID == dID) || (dCode != "" && assignedDriverID == dCode) {
			driverMatches = true
		}
	}
	if !driverMatches && session.Role != "admin" && session.Role != "dispatcher" {
		writePODJSONError(w, "forbidden: trip is not assigned to this driver", http.StatusForbidden)
		return
	}

	consigneeName := r.FormValue("consignee_name")
	consigneePhone := r.FormValue("consignee_phone")
	notes := r.FormValue("notes")
	signatureData := r.FormValue("pod_signature_data")
	if signatureData == "" {
		signatureData = r.FormValue("signature_dataurl")
	}
	quantityShort, _ := strconv.ParseFloat(r.FormValue("quantity_short"), 64)
	damageQty, _ := strconv.ParseFloat(r.FormValue("damage_qty"), 64)
	refusalReason := r.FormValue("refusal_reason")

	// Upload POD photo using existing UploadFile if provided
	var podPhotoURL string
	if _, fh, err := r.FormFile("pod_photo"); err == nil {
		if fileRec, saveErr := h.Services.Files.UploadFile(ctx, fh, "trip_pod", tripID); saveErr == nil {
			podPhotoURL = "/files/" + string(fileRec.ID)
		} else {
			writePODJSONError(w, "file upload failed: "+saveErr.Error(), http.StatusBadRequest)
			return
		}
	} else if existingURL := r.FormValue("pod_url"); existingURL != "" {
		podPhotoURL = existingURL
	}

	req := service.DeliverWithPODRequest{
		ConsigneeName:  consigneeName,
		ConsigneePhone: consigneePhone,
		Notes:          notes,
		PODPhotoURL:    podPhotoURL,
		SignatureURL:   signatureData,
		OTPCode:        r.FormValue("otp"),
	}

	tripNum, err := h.Services.Trips.DeliverWithPOD(ctx, tripID, req)
	if err != nil {
		writePODJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Persist ePOD signature + delivery discrepancies (Spec 21 §3 00073) — best-effort, never fails delivery
	scanValue := strings.TrimSpace(r.FormValue("pod_scan_value"))
	if h.DB != nil && (signatureData != "" || quantityShort != 0 || damageQty != 0 || refusalReason != "" || scanValue != "") {
		_, _ = h.DB.ExecContext(ctx,
			`UPDATE trips SET pod_signature_data = COALESCE(NULLIF(?,''), pod_signature_data),
			 pod_quantity_short = CASE WHEN ? != 0 THEN ? ELSE pod_quantity_short END,
			 pod_damage_qty = CASE WHEN ? != 0 THEN ? ELSE pod_damage_qty END,
			 pod_refusal_reason = COALESCE(NULLIF(?,''), pod_refusal_reason),
			 pod_consignee_name = COALESCE(NULLIF(?,''), pod_consignee_name),
			 pod_consignee_phone = COALESCE(NULLIF(?,''), pod_consignee_phone),
			 pod_scan_value = CASE WHEN ? != '' THEN ? ELSE pod_scan_value END
			 WHERE id = ?`,
			signatureData, quantityShort, quantityShort, damageQty, damageQty, refusalReason, consigneeName, consigneePhone, scanValue, scanValue, tripID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"trip_number": tripNum,
		"status":      "delivered",
		"pod_url":     podPhotoURL,
	})
}
