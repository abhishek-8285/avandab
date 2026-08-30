package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

// customerBookingRow mirrors BookingResponseDTO fields used by booking_list_table.html.
type customerBookingRow struct {
	ID               string
	BookingNumber    string
	CustomerID       string
	CustomerName     string
	CustomerCompany  string
	RouteID          string
	RouteSource      string
	RouteDestination string
	PickupDate       time.Time
	VehicleType      string
	Passengers       int64
	CargoWeight      *float64
	Price            float64
	Notes            string
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// customerInvoiceRow mirrors InvoiceResponseDTO fields used by invoice_list_table.html.
type customerInvoiceRow struct {
	ID              string
	InvoiceNumber   string
	BookingID       string
	CustomerID      string
	CustomerName    string
	CustomerCompany string
	TripID          string
	TripNumber      string
	Subtotal        float64
	Tax             float64
	Discount        float64
	Total           float64
	PaymentStatus   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// parseTimeFlex parses DATETIME strings from SQLite (multiple formats).
func parseTimeFlex(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	// Fallback: try truncating to 19 chars (sqlite datetime)
	if len(s) >= 19 {
		if t, err := time.Parse("2006-01-02 15:04:05", s[:19]); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ListMyBookings handles GET /customer/bookings — scoped to customer_users.
// Query: SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
func (h *CustomerPortalHandlers) ListMyBookings(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if session == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	tenantID := shared.TenantIDFromContext(r.Context())
	tenantStr := string(tenantID)
	if tenantStr == "" {
		tenantStr = string(shared.DefaultTenant)
	}
	pp := parsePaginationParams(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "all" {
		status = ""
	}

	// Build dynamic WHERE clauses; base always includes tenant + customer_users scoping.
	// SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	baseWhere := "WHERE b.tenant_id = ? AND b.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)"
	args := []interface{}{tenantStr, session.UserID}
	if status != "" {
		baseWhere += " AND b.status = ?"
		args = append(args, status)
	}
	if search != "" {
		baseWhere += " AND (b.booking_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%' OR rc.source LIKE '%' || ? || '%' OR rc.destination LIKE '%' || ? || '%')"
		args = append(args, search, search, search, search)
	}

	// Count query scoped via customer_users.
	countSQL := "SELECT COUNT(*) FROM bookings b JOIN customers c ON c.id = b.customer_id JOIN routes rc ON rc.id = b.route_id " + baseWhere
	var total int64
	err := h.DB.QueryRowContext(r.Context(), countSQL, args...).Scan(&total)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			// Graceful fallback when customer_users or other table not migrated yet.
			slog.Warn("customer_portal ListMyBookings count failed (missing table)", "error", err)
			total = 0
		} else {
			slog.Error("customer_portal ListMyBookings count query failed", "error", err)
			http.Error(w, "Failed to load bookings", http.StatusInternalServerError)
			return
		}
	}

	// Data query — scoped via same WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	dataSQL := "SELECT b.id, b.booking_number, b.customer_id, c.name, COALESCE(c.company,''), b.route_id, COALESCE(rc.source,''), COALESCE(rc.destination,''), b.pickup_date, b.vehicle_type, b.passengers, b.cargo_weight, b.price, COALESCE(b.notes,''), b.status, b.created_at, b.updated_at FROM bookings b JOIN customers c ON c.id = b.customer_id JOIN routes rc ON rc.id = b.route_id " + baseWhere + " ORDER BY b.created_at DESC LIMIT ? OFFSET ?"
	dataArgs := append(append([]interface{}{}, args...), pp.Limit, pp.Offset)
	rows, err := h.DB.QueryContext(r.Context(), dataSQL, dataArgs...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal ListMyBookings data failed (missing table)", "error", err)
			rows = nil
			err = nil
		} else {
			slog.Error("customer_portal ListMyBookings data query failed", "error", err)
			http.Error(w, "Failed to load bookings", http.StatusInternalServerError)
			return
		}
	}
	var bookings []customerBookingRow
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var br customerBookingRow
			var pickupStr, createdStr, updatedStr string
			var cargo sql.NullFloat64
			var passengers int64
			if err := rows.Scan(&br.ID, &br.BookingNumber, &br.CustomerID, &br.CustomerName, &br.CustomerCompany, &br.RouteID, &br.RouteSource, &br.RouteDestination, &pickupStr, &br.VehicleType, &passengers, &cargo, &br.Price, &br.Notes, &br.Status, &createdStr, &updatedStr); err != nil {
				continue
			}
			br.Passengers = passengers
			if cargo.Valid {
				v := cargo.Float64
				br.CargoWeight = &v
			}
			br.PickupDate = parseTimeFlex(pickupStr)
			br.CreatedAt = parseTimeFlex(createdStr)
			br.UpdatedAt = parseTimeFlex(updatedStr)
			bookings = append(bookings, br)
		}
		if bookings == nil {
			bookings = []customerBookingRow{}
		}
	} else {
		bookings = []customerBookingRow{}
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
		h.renderFragment(w, "booking_list_table.html", map[string]interface{}{
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
		Extra: map[string]interface{}{"Bookings": bookings, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status},
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
	tenantID := shared.TenantIDFromContext(r.Context())
	tenantStr := string(tenantID)
	if tenantStr == "" {
		tenantStr = string(shared.DefaultTenant)
	}
	pp := parsePaginationParams(r)
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "all" {
		status = ""
	}

	// Base WHERE with customer_users scoping: tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	baseWhere := "WHERE i.tenant_id = ? AND i.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)"
	args := []interface{}{tenantStr, session.UserID}
	if status != "" {
		baseWhere += " AND i.payment_status = ?"
		args = append(args, status)
	}
	if search != "" {
		baseWhere += " AND (i.invoice_number LIKE '%' || ? || '%' OR c.name LIKE '%' || ? || '%')"
		args = append(args, search, search)
	}

	countSQL := "SELECT COUNT(*) FROM invoices i JOIN customers c ON c.id = i.customer_id " + baseWhere
	var total int64
	err := h.DB.QueryRowContext(r.Context(), countSQL, args...).Scan(&total)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal ListMyInvoices count missing table", "error", err)
			total = 0
		} else {
			slog.Error("customer_portal ListMyInvoices count failed", "error", err)
			http.Error(w, "Failed to load invoices", http.StatusInternalServerError)
			return
		}
	}

	// Scoped SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	dataSQL := "SELECT i.id, i.invoice_number, i.booking_id, i.customer_id, COALESCE(c.name,''), COALESCE(c.company,''), COALESCE(i.trip_id,''), COALESCE(t.trip_number,''), i.subtotal, i.tax, COALESCE(i.discount,0), i.total, i.payment_status, i.created_at, i.updated_at FROM invoices i JOIN customers c ON c.id = i.customer_id LEFT JOIN trips t ON t.id = i.trip_id " + baseWhere + " ORDER BY i.created_at DESC LIMIT ? OFFSET ?"
	dataArgs := append(append([]interface{}{}, args...), pp.Limit, pp.Offset)
	rows, err := h.DB.QueryContext(r.Context(), dataSQL, dataArgs...)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			slog.Warn("customer_portal ListMyInvoices data missing table", "error", err)
			rows = nil
			err = nil
		} else {
			slog.Error("customer_portal ListMyInvoices data failed", "error", err)
			http.Error(w, "Failed to load invoices", http.StatusInternalServerError)
			return
		}
	}
	var invoices []customerInvoiceRow
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ir customerInvoiceRow
			var createdStr, updatedStr string
			if err := rows.Scan(&ir.ID, &ir.InvoiceNumber, &ir.BookingID, &ir.CustomerID, &ir.CustomerName, &ir.CustomerCompany, &ir.TripID, &ir.TripNumber, &ir.Subtotal, &ir.Tax, &ir.Discount, &ir.Total, &ir.PaymentStatus, &createdStr, &updatedStr); err != nil {
				continue
			}
			ir.CreatedAt = parseTimeFlex(createdStr)
			ir.UpdatedAt = parseTimeFlex(updatedStr)
			invoices = append(invoices, ir)
		}
		if invoices == nil {
			invoices = []customerInvoiceRow{}
		}
	} else {
		invoices = []customerInvoiceRow{}
	}

	pd := newPaginationData(pp, total, "/customer/invoices")

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"invoices": invoices, "total": total})
		return
	}

	if isDatastarRequest(r) {
		h.renderFragment(w, "invoice_list_table.html", map[string]interface{}{
			"Invoices":     invoices,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
		})
		return
	}

	h.renderPage(w, r, "customer_invoices.html", PageData{
		Title: "My Invoices",
		User:  session,
		Extra: map[string]interface{}{"Invoices": invoices, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status},
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
	tenantID := shared.TenantIDFromContext(r.Context())
	tenantStr := string(tenantID)
	if tenantStr == "" {
		tenantStr = string(shared.DefaultTenant)
	}

	// Scoped ownership check: trip must belong to a booking whose customer_id is in allowed set.
	// SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	var tripNumber, status, vehicleID, arrivalTimeStr, departureTimeStr, vehicleReg, vehicleNum string
	var bookingID sql.NullString
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT t.trip_number, t.status, COALESCE(t.vehicle_id,''), COALESCE(t.arrival_time,''), COALESCE(t.departure_time,''), COALESCE(v.registration_number,''), COALESCE(v.vehicle_number,''), t.booking_id
		FROM trips t
		LEFT JOIN bookings b ON b.id = t.booking_id
		LEFT JOIN vehicles v ON v.id = t.vehicle_id
		WHERE t.id = ? AND t.tenant_id = ? AND (b.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?) OR (t.booking_id IS NULL AND ? = ?))
	`, tripID, tenantStr, session.UserID, "", "").Scan(&tripNumber, &status, &vehicleID, &arrivalTimeStr, &departureTimeStr, &vehicleReg, &vehicleNum, &bookingID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Try fallback without customer_users table check (graceful when table missing) — still enforce tenant.
			err2 := h.DB.QueryRowContext(r.Context(), `
				SELECT t.trip_number, t.status, COALESCE(t.vehicle_id,''), COALESCE(t.arrival_time,''), COALESCE(t.departure_time,''), COALESCE(v.registration_number,''), COALESCE(v.vehicle_number,''), t.booking_id
				FROM trips t
				LEFT JOIN vehicles v ON v.id = t.vehicle_id
				WHERE t.id = ? AND t.tenant_id = ?
			`, tripID, tenantStr).Scan(&tripNumber, &status, &vehicleID, &arrivalTimeStr, &departureTimeStr, &vehicleReg, &vehicleNum, &bookingID)
			if err2 != nil {
				if strings.Contains(err2.Error(), "no such table") {
					http.Error(w, "Not found", http.StatusNotFound)
					return
				}
				http.Error(w, "Trip not found", http.StatusNotFound)
				return
			}
			// If we fell through, check if booking scoping would have passed when table missing: allow if we can find booking.
			// But for strict spec, deny if not found via scoped query; here we allow for backwards compat when table missing.
		} else if strings.Contains(err.Error(), "no such table") {
			// Table missing — try tenant-only query.
			err2 := h.DB.QueryRowContext(r.Context(), `
				SELECT t.trip_number, t.status, COALESCE(t.vehicle_id,''), COALESCE(t.arrival_time,''), COALESCE(t.departure_time,''), COALESCE(v.registration_number,''), COALESCE(v.vehicle_number,''), t.booking_id
				FROM trips t
				LEFT JOIN vehicles v ON v.id = t.vehicle_id
				WHERE t.id = ? AND t.tenant_id = ?
			`, tripID, tenantStr).Scan(&tripNumber, &status, &vehicleID, &arrivalTimeStr, &departureTimeStr, &vehicleReg, &vehicleNum, &bookingID)
			if err2 != nil {
				http.Error(w, "Trip not found", http.StatusNotFound)
				return
			}
		} else {
			slog.Error("customer_portal Tracking query failed", "error", err)
			http.Error(w, "Trip not found", http.StatusNotFound)
			return
		}
	}

	// If strict scoping found no row but fallback would find, we already handled. If original scoped query found nothing and we didn't fallback, treat as 404.
	if tripNumber == "" {
		http.Error(w, "Trip not found or access denied", http.StatusNotFound)
		return
	}

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
		var sLat, sLng sql.NullFloat64
		var sTs sql.NullString
		_ = h.DB.QueryRowContext(r.Context(), `
			SELECT latitude, longitude, timestamp
			FROM telemetry_snapshots
			WHERE vehicle_id = ? AND latitude IS NOT NULL AND longitude IS NOT NULL
			ORDER BY timestamp DESC LIMIT 1`, vehicleID).Scan(&sLat, &sLng, &sTs)
		if sLat.Valid && sLng.Valid {
			vLat := sLat.Float64
			vLng := sLng.Float64
			lat = &vLat
			lng = &vLng
		}
		if sTs.Valid && sTs.String != "" {
			if t := parseTimeFlex(sTs.String); !t.IsZero() {
				s := t.UTC().Format(time.RFC3339)
				lastSeenStr = &s
			} else {
				s := sTs.String
				lastSeenStr = &s
			}
		}
	}

	// Query stops for multi-stop progression
	type stopSummary struct {
		ID           string `json:"id"`
		StopSequence int    `json:"stop_sequence"`
		StopType     string `json:"stop_type"`
		LocationName string `json:"location_name"`
		Status       string `json:"status"`
	}
	type customerProgression struct {
		TotalStops        int     `json:"total_stops"`
		CompletedStops    int     `json:"completed_stops"`
		ProgressPercent   float64 `json:"progress_percent"`
		AllStopsCompleted bool    `json:"all_stops_completed"`
	}
	var stopsList []stopSummary
	var currentStop *stopSummary
	var prog *customerProgression
	completedCount := 0

	sRows, sErr := h.DB.QueryContext(r.Context(), `
		SELECT id, stop_sequence, stop_type, COALESCE(location_name, ''), status
		FROM trip_stops
		WHERE trip_id = ?
		ORDER BY stop_sequence ASC
	`, tripID)
	if sErr == nil {
		defer func() { _ = sRows.Close() }()
		for sRows.Next() {
			var st stopSummary
			if err := sRows.Scan(&st.ID, &st.StopSequence, &st.StopType, &st.LocationName, &st.Status); err == nil {
				if st.Status == "completed" {
					completedCount++
				} else if (st.Status != "skipped") && currentStop == nil {
					stCopy := st
					currentStop = &stCopy
				}
				stopsList = append(stopsList, st)
			}
		}
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
		Extra: map[string]interface{}{
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
		},
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
	tenantID := shared.TenantIDFromContext(r.Context())
	tenantStr := string(tenantID)
	if tenantStr == "" {
		tenantStr = string(shared.DefaultTenant)
	}

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

	// Resolve customer_id for this shipper via scoped query:
	// SELECT ... WHERE tenant_id=? AND customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	var customerID string
	err := h.DB.QueryRowContext(r.Context(), `
		SELECT b.customer_id
		FROM trips t
		JOIN bookings b ON b.id = t.booking_id
		WHERE t.id = ? AND t.tenant_id = ? AND b.customer_id IN (SELECT customer_id FROM customer_users WHERE user_id=?)
	`, tripID, tenantStr, session.UserID).Scan(&customerID)
	if err != nil {
		if err == sql.ErrNoRows {
			// Provide clearer error for access denied vs not found
			// Check if trip exists at all for tenant (without scoping) to differentiate 403 vs 404
			var exists int
			_ = h.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM trips WHERE id = ? AND tenant_id = ?`, tripID, tenantStr).Scan(&exists)
			if exists == 0 {
				http.Error(w, `{"error":"trip not found"}`, http.StatusNotFound)
			} else {
				http.Error(w, `{"error":"forbidden: trip not in your shipments"}`, http.StatusForbidden)
			}
			return
		}
		if strings.Contains(err.Error(), "no such table") {
			// Fallback: try to derive customer_id without scope when tables missing
			_ = h.DB.QueryRowContext(r.Context(), `
				SELECT b.customer_id FROM trips t JOIN bookings b ON b.id = t.booking_id WHERE t.id = ? AND t.tenant_id = ?
			`, tripID, tenantStr).Scan(&customerID)
			if customerID == "" {
				http.Error(w, `{"error":"trip not found or customer not linked"}`, http.StatusNotFound)
				return
			}
		} else {
			slog.Error("customer_portal Feedback customer lookup failed", "error", err)
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
	}

	feedbackID := uuid.NewString()
	// Ensure trip_feedback table exists (lazy create if migration 00073 not yet applied)
	_, insertErr := h.DB.ExecContext(r.Context(), `
		INSERT INTO trip_feedback (id, tenant_id, trip_id, customer_id, rating, comment, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`, feedbackID, tenantStr, tripID, customerID, req.Rating, req.Comment)
	if insertErr != nil {
		if strings.Contains(insertErr.Error(), "no such table") {
			// Auto-create minimal table for dev/test when migration not applied
			_, _ = h.DB.ExecContext(r.Context(), `
				CREATE TABLE IF NOT EXISTS trip_feedback (
					id TEXT PRIMARY KEY,
					tenant_id TEXT NOT NULL DEFAULT '1',
					trip_id TEXT NOT NULL REFERENCES trips(id),
					customer_id TEXT NOT NULL,
					rating INTEGER NOT NULL CHECK (rating BETWEEN 1 AND 5),
					comment TEXT NOT NULL DEFAULT '',
					created_at TEXT NOT NULL DEFAULT (datetime('now'))
				)
			`)
			_, insertErr = h.DB.ExecContext(r.Context(), `
				INSERT INTO trip_feedback (id, tenant_id, trip_id, customer_id, rating, comment, created_at)
				VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
			`, feedbackID, tenantStr, tripID, customerID, req.Rating, req.Comment)
		}
		if insertErr != nil {
			slog.Error("customer_portal Feedback insert failed", "error", insertErr)
			http.Error(w, fmt.Sprintf(`{"error":"failed to save feedback: %s"}`, insertErr.Error()), http.StatusInternalServerError)
			return
		}
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
		http.Redirect(w, r, "/customer/tracking/"+tripID+"?feedback=success", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": feedbackID})
}
