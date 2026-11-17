package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	kharchaapp "transport-app/internal/kharcha/application"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// Driver-facing expense API and the session->driver binding helpers,
// split out of kharcha.go to keep the dashboard file under its ratchet cap.

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
	// as DeliverWithPOD): drivers.id or the user's email. A driver account
	// without a linked driver row must not fall back to a user id.
	driverID, dErr := h.actingDriverID(ctx, session.UserID)
	if dErr != nil {
		slog.WarnContext(ctx, "driver resolution from auth user failed", slog.Any("error", dErr))
		writePODJSONError(w, "could not resolve driver profile", http.StatusInternalServerError)
		return
	}
	if driverID == "" {
		if session.Role == "driver" {
			writePODJSONError(w, "forbidden: no driver profile for this account", http.StatusForbidden)
			return
		}
		driverID = session.UserID
	}
	if session.Role == "driver" {
		owned, ownErr := h.driverOwnsTrip(ctx, driverID, tripID)
		if ownErr != nil {
			slog.WarnContext(ctx, "trip ownership check failed", slog.Any("error", ownErr))
			writePODJSONError(w, "could not verify trip assignment", http.StatusInternalServerError)
			return
		}
		if !owned {
			writePODJSONError(w, "forbidden: trip is not assigned to this driver", http.StatusForbidden)
			return
		}
	}

	var receiptURL string
	if _, fh, fErr := r.FormFile("receipt_photo"); fErr == nil {
		fileRec, saveErr := h.Services.Files.UploadFile(ctx, fh, "expense_receipt", tripID)
		if saveErr != nil {
			slog.Warn("kharcha receipt upload failed", "trip_id", tripID, "error", saveErr)
			writePODJSONError(w, "file upload failed", http.StatusBadRequest)
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
		if _, aErr := h.Services.FuelAudit.AuditPendingClaims(ctx); aErr != nil {
			slog.WarnContext(ctx, "fuel audit pass failed after expense create", slog.Any("error", aErr))
		}
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

// actingDriverID resolves the drivers.id row linked to an authenticated user.
// The mobile app creates driver users with the same email as the driver record,
// or with users.id equal to drivers.id (the drivers table has no user_id
// column). Returns "" when no driver profile is linked.
func (h *KharchaHandlers) actingDriverID(ctx context.Context, userID string) (string, error) {
	ref, err := kharchaapp.LookupUserDriver(ctx, h.DB, userID)
	if err != nil || ref == nil {
		return "", err
	}
	return ref.ID, nil
}

// driverOwnsTrip reports whether the trip is assigned to the given driver.
// An empty trip id is allowed: kharcha can be filed off-trip.
func (h *KharchaHandlers) driverOwnsTrip(ctx context.Context, driverID, tripID string) (bool, error) {
	return kharchaapp.DriverOwnsTrip(ctx, h.DB, string(shared.TenantIDFromContext(ctx)), driverID, tripID)
}
