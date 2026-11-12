package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	customerportal "transport-app/internal/customerportal"
	"transport-app/internal/shared"
)

// CustomerPortalHandlers handles shipper portal (Spec 21 §2.3, §4, §5).
// All queries are scoped via customer_users: SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
type CustomerPortalHandlers struct {
	*App
}

// NewCustomerPortalHandlers creates a new CustomerPortalHandlers.
func NewCustomerPortalHandlers(app *App) *CustomerPortalHandlers {
	return &CustomerPortalHandlers{App: app}
}

type customerInvoiceRow = customerportal.InvoiceRow

// parseTimeFlex parses DATETIME strings from SQLite (multiple formats).
// parseTimeFlex parses DATETIME strings from SQLite (multiple formats).
func parseTimeFlex(s string) time.Time {
	return customerportal.ParseTimeFlex(s)
}

// ListMyBookings handles GET /customer/bookings — scoped to customer_users.
// Query: SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
func (h *CustomerPortalHandlers) ListMyBookings(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	tenantStr := string(shared.MustTenantID(r.Context()))
	pp := parsePaginationParams(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "all" {
		status = ""
	}

	bookings, total, err := customerportal.New(h.DB).Bookings(r.Context(), tenantStr, session.UserID, status, search, pp.Limit, pp.Offset)
	if err != nil {
		slog.Error("customer_portal ListMyBookings query failed", "error", err)
		http.Error(w, "Failed to load bookings", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, total, "/customer/bookings")

	// JSON response for API clients (Spec 21 §2.3: {"bookings":[...],"total":12})
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"bookings": bookings, "total": total})
		return
	}

	// Datastar fragment reuse booking_list_table partial.
	if isDatastarRequest(r) {
		h.renderFragment(w, r, "booking_list_table.html", map[string]interface{}{
			"Bookings":     bookings,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
		})
		return
	}

	h.renderPage(w, r, "customer_bookings.html", PageData{
		Title: "My Shipments",
		User:  session,
		Extra: h.withBrand(r.Context(), map[string]interface{}{"Bookings": bookings, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status}),
	})
}

// ListMyInvoices handles GET /customer/invoices — scoped via customer_users.
// Query: SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
func (h *CustomerPortalHandlers) ListMyInvoices(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	tenantStr := string(shared.MustTenantID(r.Context()))
	pp := parsePaginationParams(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "all" {
		status = ""
	}

	invoices, total, err := customerportal.New(h.DB).Invoices(r.Context(), tenantStr, session.UserID, status, search, pp.Limit, pp.Offset)
	if err != nil {
		slog.Error("customer_portal ListMyInvoices query failed", "error", err)
		http.Error(w, "Failed to load invoices", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, total, "/customer/invoices")

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"invoices": invoices, "total": total})
		return
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, r, "invoice_list_table.html", map[string]interface{}{
			"Invoices":     invoices,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"PortalPay":    true,
		})
		return
	}

	h.renderPage(w, r, "customer_invoices.html", PageData{
		Title: "My Invoices",
		User:  session,
		Extra: h.withBrand(r.Context(), map[string]interface{}{"Invoices": invoices, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "PortalPay": true}),
	})
}

// Tracking handles GET /customer/tracking/{trip_id} — scoped via customer_users.
// Uses tenant scoping: SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
func (h *CustomerPortalHandlers) Tracking(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	tripID := chi.URLParam(r, "trip_id")
	if tripID == "" {
		tripID = chi.URLParam(r, "id")
	}
	if tripID == "" {
		http.Error(w, "trip_id required", http.StatusBadRequest)
		return
	}
	tenantStr := string(shared.MustTenantID(r.Context()))

	trip, err := customerportal.New(h.DB).Tracking(r.Context(), tenantStr, session.UserID, tripID)
	if err != nil {
		slog.Error("customer_portal Tracking query failed", "error", err)
		http.Error(w, "Trip not found", http.StatusNotFound)
		return
	}
	if trip == nil {
		http.Error(w, "Trip not found or access denied", http.StatusNotFound)
		return
	}
	tripNumber, status, vehicleID := trip.TripNumber, trip.Status, trip.VehicleID
	arrivalTimeStr := trip.ArrivalTime
	vehicleReg, vehicleNum := trip.VehicleReg, trip.VehicleNum
	bookingID := sql.NullString{String: trip.BookingID, Valid: trip.BookingID != ""}

	vehicleLabel := vehicleReg
	if vehicleLabel == "" {
		vehicleLabel = vehicleNum
	}
	if vehicleLabel == "" {
		vehicleLabel = "—"
	}

	// Attempt ETA via EtaService if available (reuses ShareData logic but auth-gated).
	var etaMinStr, etaMaxStr *string
	etaMethod := "scheduled"
	if h.App != nil && h.App.Share != nil && h.App.Share.EtaService != nil {
		if res, err := h.App.Share.EtaService.Calculate(r.Context(), tripID); err == nil {
			minT := res.EtaMin.UTC().Format(time.RFC3339)
			maxT := res.EtaMax.UTC().Format(time.RFC3339)
			etaMinStr = &minT
			etaMaxStr = &maxT
			etaMethod = res.Method
		}
	}
	// Fallback to arrival_time ±15 min if EtaService not available or failed.
	if etaMinStr == nil && arrivalTimeStr != "" {
		if arrT := parseTimeFlex(arrivalTimeStr); !arrT.IsZero() {
			minT := arrT.Add(-15 * time.Minute).UTC().Format(time.RFC3339)
			maxT := arrT.Add(15 * time.Minute).UTC().Format(time.RFC3339)
			etaMinStr = &minT
			etaMaxStr = &maxT
		}
	}

	// Last seen telemetry for vehicle.
	var lastSeenStr *string
	var lat, lng *float64
	if vehicleID != "" {
		fixLat, fixLng, fixTs, fixErr := lastSeenFix(r.Context(), h.DB, vehicleID)
		if fixErr != nil {
			// Never swallow: "query failed" must not render as "no data yet".
			slog.ErrorContext(r.Context(), "customer_portal last-seen telemetry failed",
				slog.String("vehicle_id", vehicleID), slog.Any("error", fixErr))
		}
		lat, lng = fixLat, fixLng
		if fixTs != "" {
			lastSeenStr = &fixTs
		}
	}

	type stopSummary struct {
		ID           string `json:"id"`
		StopSequence int    `json:"stop_sequence"`
		StopType     string `json:"stop_type"`
		LocationName string `json:"location_name"`
		Address      string `json:"address"`
		Status       string `json:"status"`
	}
	type customerProgression struct {
		TotalStops        int     `json:"total_stops"`
		CompletedStops    int     `json:"completed_stops"`
		ProgressPercent   float64 `json:"progress_percent"`
		AllStopsCompleted bool    `json:"all_stops_completed"`
	}
	stops := customerportal.New(h.DB).Stops(r.Context(), tripID)
	stopsList := make([]stopSummary, 0, len(stops))
	completedCount := 0
	var currentStop *stopSummary
	for _, st := range stops {
		row := stopSummary{ID: st.ID, StopSequence: st.StopSequence, StopType: st.StopType, LocationName: st.LocationName, Address: st.Address, Status: st.Status}
		if st.Status == "completed" {
			completedCount++
		} else if st.Status != "skipped" && currentStop == nil {
			cp := row
			currentStop = &cp
		}
		stopsList = append(stopsList, row)
	}
	var prog *customerProgression
	if len(stopsList) > 0 {
		p := customerProgression{
			TotalStops:        len(stopsList),
			CompletedStops:    completedCount,
			ProgressPercent:   float64(completedCount) / float64(len(stopsList)) * 100.0,
			AllStopsCompleted: completedCount == len(stopsList),
		}
		if p.AllStopsCompleted {
			p.ProgressPercent = 100.0
		}
		prog = &p
	}

	// JSON response (Spec 21 §2.3: {"trip_number":"TRP-001","status":"in_transit","eta_min":...})
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"trip_number":   tripNumber,
			"status":        status,
			"eta_min":       etaMinStr,
			"eta_max":       etaMaxStr,
			"eta_method":    etaMethod,
			"vehicle_label": vehicleLabel,
			"last_seen":     lastSeenStr,
			"lat":           lat,
			"lng":           lng,
		}
		if len(stopsList) > 0 {
			resp["stops"] = stopsList
			resp["current_stop"] = currentStop
			resp["progression"] = prog
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	}

	// HTML page with PageData + User session
	h.renderPage(w, r, "customer_tracking.html", PageData{
		Title: "Shipment Tracking",
		User:  session,
		Extra: h.withBrand(r.Context(), map[string]interface{}{
			"TripID":       tripID,
			"TripNumber":   tripNumber,
			"Status":       status,
			"VehicleLabel": vehicleLabel,
			"EtaMin":       etaMinStr,
			"EtaMax":       etaMaxStr,
			"EtaMethod":    etaMethod,
			"LastSeen":     lastSeenStr,
			"Lat":          lat,
			"Lng":          lng,
			"BookingID":    bookingID.String,
			"Stops":        stopsList,
			"CurrentStop":  currentStop,
			"Progression":  prog,
		}),
	})
}

// Feedback handles POST /customer/feedback — inserts into trip_feedback.
// Body {"trip_id":"...","rating":5,"comment":""} → 201
// Scoped via customer_users: tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
func (h *CustomerPortalHandlers) Feedback(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if session == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	tenantStr := string(shared.MustTenantID(r.Context()))

	var req struct {
		TripID  string `json:"trip_id"`
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	// Support both JSON and form submissions (trip_feedback.html posts form).
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
	} else {
		// Try JSON fallback first if body looks like JSON, else form.
		// Peek body for JSON object start.
		// Simpler: try to parse as form.
		_ = r.ParseForm()
		req.TripID = r.FormValue("trip_id")
		if req.TripID == "" {
			req.TripID = r.FormValue("tripId")
		}
		if req.TripID == "" {
			// Also try decoding JSON if body not empty and form empty
			if r.ContentLength != 0 {
				// Reset body attempt not possible after ParseForm consumed; try using raw
				// but we already have form values. If still empty, try json decode as fallback
				var bodyCopy struct {
					TripID  string `json:"trip_id"`
					Rating  int    `json:"rating"`
					Comment string `json:"comment"`
				}
				// Note: body already consumed for form, so skip.
				_ = bodyCopy
			}
		}
		if v := r.FormValue("rating"); v != "" {
			if iv, err := strconv.Atoi(v); err == nil {
				req.Rating = iv
			}
		}
		req.Comment = r.FormValue("comment")
		// Secondary fallback: try to decode JSON even when not marked json but body contains it
		if req.TripID == "" {
			// Peek r.Body for json if not already consumed (if ParseForm didn't consume for json)
			// We can try to read raw bytes via json decoder with no-op if already consumed it will EOF.
			// To avoid complexity, just attempt second decode if form empty and we have a temp buffer via earlier json attempt
			// but we already lost. For robustness, also accept trip_id from URL query.
			if qv := r.URL.Query().Get("trip_id"); qv != "" {
				req.TripID = qv
			}
		}
		// If still empty and Content-Type was not json but body is json, try a direct json decode using a fresh read of form values already?
		// As fallback, if trip_id still empty, try to decode JSON from request again by re-reading (if not consumed).
		if req.TripID == "" {
			// Attempt to decode JSON body directly (may be empty)
			_ = json.NewDecoder(r.Body).Decode(&req)
		}
	}

	if strings.TrimSpace(req.TripID) == "" {
		http.Error(w, `{"error":"trip_id required"}`, http.StatusBadRequest)
		return
	}
	if req.Rating < 1 || req.Rating > 5 {
		http.Error(w, `{"error":"rating must be between 1 and 5"}`, http.StatusBadRequest)
		return
	}
	if len(req.Comment) > 2000 {
		http.Error(w, `{"error":"comment too long"}`, http.StatusBadRequest)
		return
	}
	tripID := strings.TrimSpace(req.TripID)

	feedbackID, err := customerportal.NewFeedbackService(h.DB).Create(r.Context(), tenantStr, session.UserID, customerportal.FeedbackRequest{
		TripID:  tripID,
		Rating:  req.Rating,
		Comment: req.Comment,
	})
	if err != nil {
		var insErr *customerportal.FeedbackInsertError
		switch {
		case errors.Is(err, customerportal.ErrFeedbackTripNotFound):
			http.Error(w, `{"error":"trip not found"}`, http.StatusNotFound)
		case errors.Is(err, customerportal.ErrFeedbackForbidden):
			http.Error(w, `{"error":"forbidden: trip not in your shipments"}`, http.StatusForbidden)
		case errors.Is(err, customerportal.ErrFeedbackCustomerUnlinked):
			http.Error(w, `{"error":"trip not found or customer not linked"}`, http.StatusNotFound)
		case errors.As(err, &insErr):
			slog.Error("customer_portal Feedback insert failed", "error", insErr.Err)
			http.Error(w, fmt.Sprintf(`{"error":"failed to save feedback: %s"}`, insErr.Err.Error()), http.StatusInternalServerError)
		default:
			slog.Error("customer_portal Feedback customer lookup failed", "error", err)
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		}
		return
	}

	if wantsJSON(r) || strings.Contains(r.Header.Get("Accept"), "application/json") || req.Comment != "" && strings.Contains(ct, "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": feedbackID, "trip_id": tripID, "rating": req.Rating})
		return
	}
	// For form posts, redirect to tracking page or show success
	if strings.Contains(r.Header.Get("Accept"), "text/html") || ct == "application/x-www-form-urlencoded" || r.Method == http.MethodPost {
		// If HTML form, redirect back to tracking with success flash via query param
		http.Redirect(w, r, safeRedirect(r, "/customer/tracking/"+url.PathEscape(tripID)+"?feedback=success", "/customer/tracking"), http.StatusSeeOther) //nolint:gosec // G710: safeRedirect returns only same-origin paths (see redirect_test.go)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": feedbackID})
}

// lastSeenFix returns the newest telemetry fix recorded for a vehicle.
//
// Ordering uses ts_unix (migration 00134), not the `timestamp` text: that
// column mixes RFC3339 ("2026-06-01T10:00:00Z") and Go's time.String() layout
// ("2026-06-01 15:00:00 +0000 UTC") — 'T' > ' ' — so ORDER BY timestamp DESC
// sorts bytes and hands back a different row than the newest fix.
//
// Returns (nil, nil, "", nil) when the vehicle has never reported. A real
// query failure is returned to the caller so it can be logged: a dead table
// and an unreported vehicle must not look identical to the shipper.
// lastSeenFix returns the newest telemetry fix recorded for a vehicle.
//
// Ordering uses ts_unix (migration 00134), not the `timestamp` text: that
// column mixes RFC3339 and Go's time.String() layout — 'T' > ' ' — so
// ORDER BY timestamp DESC sorts bytes and hands back an older fix. The SQL
// lives in internal/customerportal (handlers-end-state ADR, Phase 2); this
// thin wrapper keeps the characterization-test signature.
func lastSeenFix(ctx context.Context, db *sql.DB, vehicleID string) (lat, lng *float64, ts string, err error) {
	return customerportal.LastSeenFix(ctx, db, vehicleID)
}

// brandView is the customer-facing identity for white-labelled shipper pages.
type brandView = customerportal.Brand

// tenantBrand resolves the transporter's own brand: tenant profile first,
// platform company settings second, product default last. White-label means
// the shipper must see the transporter, never the platform vendor. The SQL
// lives in internal/customerportal (handlers-end-state ADR, Phase 2).
func (a *App) tenantBrand(ctx context.Context) brandView {
	if a == nil {
		return brandView{Name: "Avandab"}
	}
	return customerportal.TenantBrand(ctx, a.DB)
}

// withBrand injects the brand into an existing template Extra map.
func (a *App) withBrand(ctx context.Context, extra map[string]interface{}) map[string]interface{} {
	if extra == nil {
		extra = map[string]interface{}{}
	}
	b := a.tenantBrand(ctx)
	extra["BrandName"] = b.Name
	extra["BrandLogo"] = b.Logo
	return extra
}
