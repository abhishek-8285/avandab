package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/domain"
	kharchaapp "transport-app/internal/kharcha/application"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
)

// KharchaHandlers handles the driver expense (kharcha) approval dashboard.
type KharchaHandlers struct {
	*App
}

func (h *KharchaHandlers) Routes(r chi.Router) {
	// kharcha:read, not trips:read: the driver role (00027) holds trips:read,
	// and the dashboard/queue/ledger expose every driver's claims.
	r.With(middleware.ResourcePermission(h.AuthSrv, "kharcha", "read")).Get("/", h.Dashboard)
	r.With(middleware.ResourcePermission(h.AuthSrv, "kharcha", "read")).Get("/pending", h.PendingQueue)
	r.With(middleware.ResourcePermission(h.AuthSrv, "kharcha", "read")).Get("/ledger", h.Ledger)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/create", h.Create)
	// kharcha:approve, not trips:update: the driver role (00027) holds
	// trips:update, and expense approval is a money-authorization step.
	r.With(middleware.ResourcePermission(h.AuthSrv, "kharcha", "approve")).Post("/{id}/approve", h.Approve)
	r.With(middleware.ResourcePermission(h.AuthSrv, "kharcha", "approve")).Post("/{id}/reject", h.Reject)
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
	session, _ := h.getUserFromContext(r)
	h.renderFragment(w, r, "kharcha_queue.html", map[string]interface{}{
		"PendingExpenses": pending,
		"User":            session,
	})
}

// GET /kharcha/ledger?trip_id= — HTMX partial: filtered ledger rows.
func (h *KharchaHandlers) Ledger(w http.ResponseWriter, r *http.Request) {
	tripID := r.URL.Query().Get("trip_id")
	entries, _ := h.Services.Kharcha.ListLedger(r.Context(), tripID)
	h.renderFragment(w, r, "kharcha_ledger_rows.html", map[string]interface{}{
		"LedgerEntries": entries,
	})
}

// POST /kharcha/create — create a new driver expense claim (web form).
// Fuel claims capture fuel_litres for the audit flow (Spec 03 §3.2 step 1).
func (h *KharchaHandlers) Create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := h.getUserFromContext(r)
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
	if session.Role == "driver" {
		ownID, ownErr := h.actingDriverID(ctx, session.UserID)
		if ownErr != nil {
			slog.WarnContext(ctx, "driver resolution for kharcha create failed", slog.Any("error", ownErr))
			http.Error(w, "could not resolve driver profile", http.StatusInternalServerError)
			return
		}
		if ownID == "" {
			http.Error(w, "forbidden: no driver profile for this account", http.StatusForbidden)
			return
		}
		if driverID != "" && driverID != ownID {
			http.Error(w, "forbidden: cannot file an expense for another driver", http.StatusForbidden)
			return
		}
		owned, ownErr := h.driverOwnsTrip(ctx, ownID, tripID)
		if ownErr != nil {
			slog.WarnContext(ctx, "trip ownership check failed", slog.Any("error", ownErr))
			http.Error(w, "could not verify trip assignment", http.StatusInternalServerError)
			return
		}
		if !owned {
			http.Error(w, "forbidden: trip is not assigned to this driver", http.StatusForbidden)
			return
		}
		driverID = ownID
	}
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
		if _, aErr := h.Services.FuelAudit.AuditPendingClaims(ctx); aErr != nil {
			slog.WarnContext(ctx, "fuel audit pass failed after expense create", slog.Any("error", aErr))
		}
	}

	if isDatastarRequest(r) {
		expense, err := h.Services.Kharcha.GetExpenseByID(ctx, expenseID)
		if err == nil {
			h.renderFragment(w, r, "kharcha_row_approved.html", expense)
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
func (h *KharchaHandlers) Approve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	expenseID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.Services.Kharcha.ApproveExpense(ctx, expenseID, session.UserID); err != nil {
		if errors.Is(err, service.ErrSelfApproval) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		// Chip tokens for wash + text so this banner survives dark mode. This is
		// the one Go site where light mode shifts slightly: the wash moves from
		// emerald-50/60 to emerald-500/10 (the shared chip wash), so it reads a
		// touch more saturated. The solid `bg-emerald-500` dot is left alone —
		// an opaque fill is legible on both light and dark surfaces.
		_, _ = fmt.Fprintf(w, `<div class="px-6 py-4 flex items-center gap-3 bg-emerald-soft"><span class="w-2 h-2 rounded-full bg-emerald-500"></span><span class="text-sm font-semibold text-emerald-strong">Expense approved successfully.</span></div>`)
		return
	}

	// htmx 4 hx-partial: row morph + badge/KPI morph + toast trigger in one response
	if isDatastarRequest(r) {
		stats, _ := h.Services.Kharcha.GetKharchaStats(ctx)
		pending, _ := h.Services.Kharcha.ListPendingExpenses(ctx)
		w.Header().Set("HX-Trigger", `{"showToast":{"tone":"success","msg":"Expense approved"}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// row
		if tErr := h.Templates.ExecuteTemplate(w, "kharcha_row_approved.html", expense); tErr != nil {
			slog.WarnContext(ctx, "kharcha approved-row partial render failed", slog.Any("error", tErr))
		}
		// OOB partials via htmx 4 <template hx type="partial">
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kharcha-queue-count" hx-swap="innerMorph">%d waiting</template>`, len(pending))
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kpi-pending-count" hx-swap="innerMorph">%d</template>`, stats.PendingCount)
		return
	}

	h.renderFragment(w, r, "kharcha_row_approved.html", expense)
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
		if errors.Is(err, service.ErrSelfApproval) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
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
		if tErr := h.Templates.ExecuteTemplate(w, "kharcha_row_rejected.html", expense); tErr != nil {
			slog.WarnContext(ctx, "kharcha rejected-row partial render failed", slog.Any("error", tErr))
		}
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kharcha-queue-count" hx-swap="innerMorph">%d waiting</template>`, len(pending))
		_, _ = fmt.Fprintf(w, `<template hx type="partial" hx-target="#kpi-pending-count" hx-swap="innerMorph">%d</template>`, stats.PendingCount)
		return
	}

	h.renderFragment(w, r, "kharcha_row_rejected.html", expense)
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

	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBodyBytes)
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
		if ref, qErr := kharchaapp.LookupUserDriver(ctx, h.DB, session.UserID); qErr != nil {
			// No driver row is a valid state (falls through to the 403); real
			// DB errors must surface in logs, not vanish.
			slog.WarnContext(ctx, "driver resolution from auth user failed", slog.Any("error", qErr))
		} else if ref != nil {
			if (ref.ID != "" && assignedDriverID == ref.ID) || (ref.DriverCode != "" && assignedDriverID == ref.DriverCode) {
				driverMatches = true
			}
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
			slog.Warn("kharcha POD upload failed", "trip_id", tripID, "error", saveErr)
			writePODJSONError(w, "file upload failed", http.StatusBadRequest)
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
		_ = kharchaapp.UpdateTripPODArtifacts(ctx, h.DB, tripID, kharchaapp.PODArtifacts{
			SignatureData:  signatureData,
			QuantityShort:  quantityShort,
			DamageQty:      damageQty,
			RefusalReason:  refusalReason,
			ConsigneeName:  consigneeName,
			ConsigneePhone: consigneePhone,
			ScanValue:      scanValue,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"trip_number": tripNum,
		"status":      "delivered",
		"pod_url":     h.PODSigner.SignURLOrRaw(podPhotoURL),
	})
}

// PendingAPI handles GET /api/v1/kharcha/pending (P3.7) for mobile manager approvals.
func (h *KharchaHandlers) PendingAPI(w http.ResponseWriter, r *http.Request) {
	pending, err := h.Services.Kharcha.ListPendingExpenses(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pending == nil {
		pending = []service.KharchaExpense{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pending)
}

// ApproveAPI handles POST /api/v1/kharcha/{id}/approve with JSON response.
func (h *KharchaHandlers) ApproveAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	expenseID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.Services.Kharcha.ApproveExpense(ctx, expenseID, session.UserID); err != nil {
		if errors.Is(err, service.ErrSelfApproval) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "approved",
		"expense_id": expenseID,
	})
}

// RejectAPI handles POST /api/v1/kharcha/{id}/reject with JSON response.
func (h *KharchaHandlers) RejectAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	expenseID := chi.URLParam(r, "id")
	session, ok := h.getUserFromContext(r)
	if !ok || session == nil || session.UserID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Reason == "" {
		body.Reason = r.FormValue("reason")
	}

	if err := h.Services.Kharcha.RejectExpense(ctx, expenseID, session.UserID, body.Reason); err != nil {
		if errors.Is(err, service.ErrSelfApproval) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "rejected",
		"expense_id": expenseID,
		"reason":     body.Reason,
	})
}
