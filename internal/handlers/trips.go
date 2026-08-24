package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	bookingdomain "transport-app/internal/booking/domain"
	bookingaggregate "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/domain"
	geofencerepo "transport-app/internal/geofence/infrastructure/persistence/sql"
	invoiceApp "transport-app/internal/invoice/application"
	invoiceaggregate "transport-app/internal/invoice/domain/aggregate"
	invoicesql "transport-app/internal/invoice/infrastructure/persistence/sql"
	"transport-app/internal/middleware"
	"transport-app/internal/pnl"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	clock "transport-app/internal/shared/clock"
	id "transport-app/internal/shared/id"
	"transport-app/internal/shared/ports"
	uow "transport-app/internal/shared/uow"
	tripapp "transport-app/internal/trip/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
)

// TripHandlers handles trip management.
type TripHandlers struct {
	*App
	createUC          *tripapp.CreateTripUseCase
	startUC           *tripapp.StartTripUseCase
	reachPickupUC     *tripapp.ReachPickupUseCase
	startTransitUC    *tripapp.StartTransitUseCase
	deliverUC         *tripapp.DeliverUseCase
	completeUC        *tripapp.CompleteTripUseCase
	cancelUC          *tripapp.CancelTripUseCase
	getUC             *tripapp.GetTripUseCase
	listUC            *tripapp.ListTripsUseCase
	scheduleUC        *tripapp.ScheduleTripUseCase
	assignDriverUC    *tripapp.AssignDriverUseCase
	assignVehicleUC   *tripapp.AssignVehicleUseCase
	generateInvoiceUC *invoiceApp.GenerateInvoiceUseCase
	detRepo           *geofencerepo.EventLogRepository
}

func (h *TripHandlers) init() {
	if h.createUC == nil {
		uowImpl := uow.NewSQLUnitOfWork(h.DB)
		clockImpl := clock.NewRealClock()
		idGenImpl := id.NewUUIDGenerator()

		h.createUC = tripapp.NewCreateTripUseCase(uowImpl, idGenImpl, clockImpl)
		h.startUC = tripapp.NewStartTripUseCase(uowImpl, clockImpl)
		h.reachPickupUC = tripapp.NewReachPickupUseCase(uowImpl, clockImpl)
		h.startTransitUC = tripapp.NewStartTransitUseCase(uowImpl, clockImpl)
		h.deliverUC = tripapp.NewDeliverUseCase(uowImpl, clockImpl)
		h.completeUC = tripapp.NewCompleteTripUseCase(uowImpl, clockImpl)
		h.cancelUC = tripapp.NewCancelTripUseCase(uowImpl, clockImpl)
		h.getUC = tripapp.NewGetTripUseCase(uowImpl)
		h.listUC = tripapp.NewListTripsUseCase(uowImpl)
		h.scheduleUC = tripapp.NewScheduleTripUseCase(uowImpl, clockImpl)
		h.assignDriverUC = tripapp.NewAssignDriverUseCase(uowImpl, clockImpl)
		h.assignVehicleUC = tripapp.NewAssignVehicleUseCase(uowImpl, clockImpl)
		h.generateInvoiceUC = invoiceApp.NewGenerateInvoiceUseCase(uowImpl, idGenImpl, clockImpl)
		h.detRepo = geofencerepo.NewEventLogRepository(h.DB)
	}
}

func (h *TripHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Get("/{id}/edit", h.Edit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/edit", h.Update)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "delete")).Post("/{id}/delete", h.Delete)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/schedule", h.Schedule)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "assign")).Post("/{id}/assign-driver", h.AssignDriver)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "assign")).Post("/{id}/assign-vehicle", h.AssignVehicle)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/start", h.StartTrip)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/reach-pickup", h.ReachPickup)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/in-transit", h.StartTransit)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/deliver", h.Deliver)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/complete", h.CompleteTrip)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "cancel")).Post("/{id}/cancel", h.CancelTrip)
	r.With(middleware.ResourcePermission(h.AuthSrv, "shares", "create")).Post("/{id}/share", h.App.Share.CreateShare)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/{id}/compliance", h.TripComplianceFragment)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/send-pod-otp", h.SendPODOTPSMS)
}

// SendPODOTPSMS texts the trip's active delivery OTP to the consignee phone
// captured on POD. Requires the SMS webhook channel to be configured; the
// operator-relay fallback (reading the code aloud) stays available otherwise.
func (h *TripHandlers) SendPODOTPSMS(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if h.App == nil || h.App.Notify == nil || !h.App.Notify.SMSConfigured() {
		h.flashAndRedirect(w, r, tripID, "SMS delivery is not configured; set SMS_WEBHOOK_URL.", false)
		return
	}
	otp, err := h.Services.Trips.EnsurePODOTP(r.Context(), tripID)
	if err != nil || otp == "" {
		h.flashAndRedirect(w, r, tripID, "No delivery OTP is active for this trip.", false)
		return
	}
	var phone string
	if h.App.DB != nil {
		var p sql.NullString
		if err := h.App.DB.QueryRowContext(r.Context(),
			`SELECT COALESCE(pod_consignee_phone,'') FROM trips WHERE id = ?`, tripID).Scan(&p); err == nil {
			phone = p.String
		}
	}
	if strings.TrimSpace(phone) == "" {
		h.flashAndRedirect(w, r, tripID, "No consignee phone on record — capture it during POD entry first.", false)
		return
	}
	msg := fmt.Sprintf("Delivery OTP for your shipment (trip %s): %s. Share this code with the driver only at delivery.", tripRefForSMS(r.Context(), h.Services.Trips, tripID), otp)
	if err := h.App.Notify.SendSMS(r.Context(), ports.NotificationMessage{
		Recipient: phone,
		Body:      msg,
		Type:      ports.NotificationTypeSMS,
	}); err != nil {
		slog.Error("POD OTP SMS delivery failed", "trip_id", tripID, "error", err)
		h.flashAndRedirect(w, r, tripID, "SMS delivery failed — relay the code manually.", false)
		return
	}
	h.flashAndRedirect(w, r, tripID, "OTP sent by SMS to "+phone+".", true)
}

func tripRefForSMS(ctx context.Context, trips *service.TripService, tripID string) string {
	trip, err := trips.GetTrip(ctx, domain.TripID(tripID))
	if err != nil {
		return tripID
	}
	return trip.TripNumber
}

func (h *TripHandlers) flashAndRedirect(w http.ResponseWriter, r *http.Request, tripID, msg string, success bool) {
	name := "flash_error"
	if success {
		name = "flash_success"
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: url.QueryEscape(msg), Path: "/", HttpOnly: true, MaxAge: 10})
	http.Redirect(w, r, "/trips/"+tripID, http.StatusSeeOther)
}

func (h *TripHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	pp := parsePaginationParams(r)

	res, err := h.listUC.Execute(r.Context(), tripapp.ListTripsQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     pp.Page,
		Limit:    pp.Limit,
		Search:   pp.Query,
		Status:   pp.Status,
	})
	if err != nil {
		fmt.Printf("[Trips Error] Failed to list trips: %v\n", err)
		http.Error(w, "Failed to load trips", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, res.Total, "/trips")

	if isDatastarRequest(r) {
		h.renderFragment(w, "trip_list.html", map[string]interface{}{
			"Trips":        res.Trips,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
		})
		return
	}

	h.renderPage(w, r, "trip_list.html", PageData{
		Title: "Trips",
		User:  session,
		Extra: map[string]interface{}{"Trips": res.Trips, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status},
	})
}

func (h *TripHandlers) New(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	bookingID := r.URL.Query().Get("booking")
	drivers, _, _ := h.Services.Drivers.ListDrivers(r.Context(), "", "available", 1000, 0)
	vehicles, _, _ := h.Services.Vehicles.ListVehicles(r.Context(), "", "available", 1000, 0)
	routes, _, _ := h.Services.Routes.ListRoutes(r.Context(), "", 1000, 0)
	h.renderForm(w, r, "trip_edit.html", PageData{
		Title: "New Trip",
		User:  session,
		Extra: map[string]interface{}{"Drivers": drivers, "Vehicles": vehicles, "Routes": routes, "SelectedBookingID": bookingID},
	})
}

func (h *TripHandlers) Create(w http.ResponseWriter, r *http.Request) {
	h.init()
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var bookingID *string
	if b := r.PostFormValue("booking_id"); b != "" {
		bookingID = &b
	}

	departureTime, err := time.Parse("2006-01-02T15:04", r.PostFormValue("departure_time"))
	if err != nil {
		departureTime, err = time.Parse("2006-01-02 15:04", r.PostFormValue("departure_time"))
		if err != nil {
			departureTime = time.Now()
		}
	}

	id, err := h.createUC.Execute(r.Context(), tripapp.CreateTripCommand{
		TenantID:      shared.TenantIDFromContext(r.Context()),
		BookingID:     bookingID,
		RouteID:       r.PostFormValue("route_id"),
		DepartureTime: departureTime,
		Remarks:       r.PostFormValue("remarks"),
	})
	if err != nil {
		session, _ := h.getUserFromContext(r)
		h.renderForm(w, r, "trip_edit.html", PageData{Title: "New Trip", User: session, FlashError: err.Error()})
		return
	}

	// Assign driver/vehicle if supplied in form post
	if dID := r.PostFormValue("driver_id"); dID != "" {
		_ = h.assignDriverUC.Execute(r.Context(), tripapp.AssignDriverCommand{
			TripID:   id,
			DriverID: dID,
			TenantID: shared.TenantIDFromContext(r.Context()),
		})
	}
	if vID := r.PostFormValue("vehicle_id"); vID != "" {
		_ = h.assignVehicleUC.Execute(r.Context(), tripapp.AssignVehicleCommand{
			TripID:    id,
			VehicleID: vID,
			TenantID:  shared.TenantIDFromContext(r.Context()),
		})
	}

	http.Redirect(w, r, "/trips", http.StatusSeeOther)
}

func (h *TripHandlers) View(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	id := chi.URLParam(r, "id")
	trip, err := h.getUC.Execute(r.Context(), tripapp.GetTripQuery{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Trip Not Found", fmt.Sprintf("No trip found with ID %q.", id), session)
		return
	}

	var availableDrivers, availableVehicles interface{}
	if trip.DriverID == nil || trip.VehicleID == nil {
		availableDrivers, _, _ = h.Services.Drivers.ListDrivers(r.Context(), "", "available", 1000, 0)
		availableVehicles, _, _ = h.Services.Vehicles.ListVehicles(r.Context(), "", "available", 1000, 0)
	}
	tripPnL, _ := pnl.NewService(h.DB).Calculate(r.Context(), id)

	var ewbRecord interface{}
	if h.App != nil && h.App.EWayBill != nil && h.App.EWayBill.svc != nil {
		if rec, err := h.App.EWayBill.svc.GetByTrip(r.Context(), id); err == nil {
			ewbRecord = rec
		}
	}

	// Delivery OTP: shown to the operator, relayed to the consignee by phone
	// until SMS delivery is configured (48h validity, regenerates on expiry).
	podOTP, _ := h.Services.Trips.EnsurePODOTP(r.Context(), id)
	flashMsg, flashOK := "", false
	if c, err := r.Cookie("flash_success"); err == nil && c.Value != "" {
		flashMsg, _ = url.QueryUnescape(c.Value)
		flashOK = true
		http.SetCookie(w, &http.Cookie{Name: "flash_success", Value: "", Path: "/", MaxAge: -1})
	} else if c, err := r.Cookie("flash_error"); err == nil && c.Value != "" {
		flashMsg, _ = url.QueryUnescape(c.Value)
		http.SetCookie(w, &http.Cookie{Name: "flash_error", Value: "", Path: "/", MaxAge: -1})
	}

	// Related context for the detail page: booking (customer + fare),
	// kharcha ledger and the invoice generated from this trip.
	var bookingInfo interface{}
	if trip.BookingID != nil && *trip.BookingID != "" {
		if b, err := h.Services.Bookings.GetBooking(r.Context(), domain.BookingID(*trip.BookingID)); err == nil {
			bookingInfo = b
		}
	}

	kharchaRows, _ := h.Services.Kharcha.ListLedger(r.Context(), id)
	var kharchaTotal float64
	for _, e := range kharchaRows {
		if e.Status != "rejected" {
			kharchaTotal += e.Amount
		}
	}

	var tripInvoice interface{}
	if h.DB != nil {
		if agg, err := invoicesql.NewInvoiceRepository(h.DB).FindByTripID(r.Context(), id, shared.TenantIDFromContext(r.Context())); err == nil && agg != nil {
			tripInvoice = agg
		}
	}

	duration := ""
	if trip.ArrivalTime != nil {
		if d := trip.ArrivalTime.Sub(trip.DepartureTime); d > 0 {
			duration = fmt.Sprintf("%.1f h", d.Hours())
		}
	}

	h.renderPage(w, r, "trip_view.html", PageData{
		Title: "View Trip",
		User:  session,
		Extra: map[string]interface{}{
			"Trip":              trip,
			"AvailableDrivers":  availableDrivers,
			"AvailableVehicles": availableVehicles,
			"PnL":               tripPnL,
			"EWayBill":          ewbRecord,
			"PODOTP":            podOTP,
			"SMSEnabled":        h.App != nil && h.App.Notify != nil && h.App.Notify.SMSConfigured(),
			"FlashMsg":          flashMsg,
			"FlashOK":           flashOK,
			"Booking":           bookingInfo,
			"Kharcha":           kharchaRows,
			"KharchaTotal":      kharchaTotal,
			"Invoice":           tripInvoice,
			"Duration":          duration,
		},
	})
}

func (h *TripHandlers) Edit(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	session, _ := h.getUserFromContext(r)
	trip, err := h.getUC.Execute(r.Context(), tripapp.GetTripQuery{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Trip not found", http.StatusNotFound)
		return
	}
	drivers, _, _ := h.Services.Drivers.ListDrivers(r.Context(), "", "", 1000, 0)
	vehicles, _, _ := h.Services.Vehicles.ListVehicles(r.Context(), "", "", 1000, 0)
	routes, _, _ := h.Services.Routes.ListRoutes(r.Context(), "", 1000, 0)

	var selDriverID, selVehicleID string
	if trip.DriverID != nil {
		selDriverID = *trip.DriverID
	}
	if trip.VehicleID != nil {
		selVehicleID = *trip.VehicleID
	}

	h.renderForm(w, r, "trip_edit.html", PageData{
		Title: "Edit Trip",
		User:  session,
		Extra: map[string]interface{}{
			"Trip":              trip,
			"Drivers":           drivers,
			"Vehicles":          vehicles,
			"Routes":            routes,
			"SelectedDriverID":  selDriverID,
			"SelectedVehicleID": selVehicleID,
		},
	})
}

func (h *TripHandlers) Update(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id := domain.TripID(chi.URLParam(r, "id"))
	var driverID *domain.DriverID
	if d := r.PostFormValue("driver_id"); d != "" {
		did := domain.DriverID(d)
		driverID = &did
	}
	var vehicleID *domain.VehicleID
	if v := r.PostFormValue("vehicle_id"); v != "" {
		vid := domain.VehicleID(v)
		vehicleID = &vid
	}

	_, err := h.Services.Trips.UpdateTrip(r.Context(), id, service.CreateTripRequest{
		RouteID:       domain.RouteID(r.PostFormValue("route_id")),
		DriverID:      driverID,
		VehicleID:     vehicleID,
		DepartureTime: r.PostFormValue("departure_time"),
		ArrivalTime:   r.PostFormValue("arrival_time"),
		Remarks:       r.PostFormValue("remarks"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id.String(), http.StatusSeeOther)
}

func (h *TripHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := domain.TripID(chi.URLParam(r, "id"))
	if err := h.Services.Trips.DeleteTrip(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips", http.StatusSeeOther)
}

func (h *TripHandlers) Schedule(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	err := h.scheduleUC.Execute(r.Context(), tripapp.ScheduleTripCommand{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}

func (h *TripHandlers) extractAssignParams(r *http.Request, key string) (string, bool, string) {
	idVal := r.FormValue(key)
	overrideStr := r.FormValue("override_maintenance")
	if overrideStr == "" {
		overrideStr = r.FormValue("override")
	}
	reason := r.FormValue("override_reason")
	if reason == "" {
		reason = r.FormValue("reason")
	}
	override := overrideStr == "1" || strings.EqualFold(overrideStr, "true") || strings.EqualFold(overrideStr, "on")

	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		// Need to read body without losing it for later; we decode into map and reuse values
		bodyBytes, err := io.ReadAll(r.Body)
		if err == nil && len(bodyBytes) > 0 {
			// restore body for downstream if needed (not needed currently)
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			var data map[string]interface{}
			if json.Unmarshal(bodyBytes, &data) == nil {
				if v, ok := data[key].(string); ok && v != "" {
					idVal = v
				}
				if v, ok := data["driver_id"].(string); ok && key == "driver_id" && v != "" {
					idVal = v
				}
				if v, ok := data["vehicle_id"].(string); ok && key == "vehicle_id" && v != "" {
					idVal = v
				}
				if v, ok := data["override"].(bool); ok {
					override = v
				}
				if v, ok := data["override_maintenance"].(bool); ok {
					override = override || v
				}
				if v, ok := data["override"].(string); ok {
					override = override || v == "1" || strings.EqualFold(v, "true")
				}
				if v, ok := data["reason"].(string); ok && v != "" {
					reason = v
				}
				if v, ok := data["override_reason"].(string); ok && v != "" {
					reason = v
				}
			}
		}
	}
	return idVal, override, reason
}

func (h *TripHandlers) handleComplianceBlock(w http.ResponseWriter, r *http.Request, tripID, driverID, vehicleID string, override bool, reason string, blockErr error) bool {
	blockedBy := blockErr.Error()
	lower := strings.ToLower(blockedBy)
	var code string
	switch {
	case strings.Contains(lower, "license"):
		code = "license_expiry"
	case strings.Contains(lower, "insurance"):
		code = "insurance_expiry"
	case strings.Contains(lower, "fitness"):
		code = "fitness_expiry"
	case strings.Contains(lower, "permit"), strings.Contains(lower, "rc"):
		code = "rc_expiry"
	case strings.Contains(lower, "puc"):
		code = "puc_expiry"
	case strings.Contains(lower, "blocked"):
		code = "blocked"
	default:
		code = "compliance"
	}
	if !override {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "dispatch_blocked", "blocked_by": code, "detail": blockedBy})
		return true
	}
	user, _ := h.getUserFromContext(r)
	if user == nil {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return true
	}
	hasPerm := false
	if h.AuthSrv != nil && h.AuthSrv.Can(user.UserID, "users", "update") {
		hasPerm = true
	}
	if user.Role == "admin" {
		hasPerm = true
	}
	if !hasPerm {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden", "detail": "dispatch override requires users:update permission"})
		return true
	}
	if len(strings.TrimSpace(reason)) < 10 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "reason is required (≥10 chars)"})
		return true
	}
	tenant := shared.TenantIDFromContext(r.Context())
	if tenant == "" {
		tenant = shared.DefaultTenant
	}
	if h.DB != nil {
		_, _ = h.DB.ExecContext(r.Context(), `CREATE TABLE IF NOT EXISTS dispatch_overrides (
            id TEXT PRIMARY KEY,
            tenant_id TEXT NOT NULL DEFAULT '1',
            trip_id TEXT NOT NULL,
            vehicle_id TEXT,
            driver_id TEXT,
            blocked_by TEXT NOT NULL,
            reason TEXT NOT NULL,
            overridden_by TEXT NOT NULL,
            created_at TEXT NOT NULL DEFAULT (datetime('now'))
        )`)
		id := uuid.NewString()
		_, _ = h.DB.ExecContext(r.Context(), `INSERT INTO dispatch_overrides (id, tenant_id, trip_id, vehicle_id, driver_id, blocked_by, reason, overridden_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
			id, string(tenant), tripID, vehicleID, driverID, code, reason, user.UserID)
	}
	if h.Services != nil && h.Services.Audit != nil {
		uid := domain.UserID(user.UserID)
		rc := reason
		_ = h.Services.Audit.LogAction(r.Context(), &uid, "dispatch_override", "trips", tripID, nil, &rc)
	} else if h.DB != nil {
		auditID := uuid.NewString()
		_, _ = h.DB.ExecContext(r.Context(), `INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, created_at) VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
			auditID, user.UserID, "dispatch_override", "trips", tripID, reason)
	}
	return false
}

func (h *TripHandlers) AssignDriver(w http.ResponseWriter, r *http.Request) {
	h.init()
	tripID := chi.URLParam(r, "id")
	driverID, override, reason := h.extractAssignParams(r, "driver_id")

	// Hard dispatch blocker via EnforceDispatchCompliance (Spec 21 §5)
	if h.Services != nil && h.Services.Compliance != nil {
		ctx := r.Context()
		var vehicleID string
		if h.DB != nil {
			var v sql.NullString
			_ = h.DB.QueryRowContext(ctx, `SELECT vehicle_id FROM trips WHERE id = ?`, tripID).Scan(&v)
			if v.Valid {
				vehicleID = v.String
			}
		}
		var dPtr *domain.DriverID
		var vPtr *domain.VehicleID
		if driverID != "" {
			did := domain.DriverID(driverID)
			dPtr = &did
		}
		if vehicleID != "" {
			vid := domain.VehicleID(vehicleID)
			vPtr = &vid
		}
		if err := h.Services.Compliance.EnforceDispatchCompliance(ctx, dPtr, vPtr); err != nil {
			if blocked := h.handleComplianceBlock(w, r, tripID, driverID, vehicleID, override, reason, err); blocked {
				return
			}
		}
	}

	// Maintenance override still requires maintenance:update unless compliance override already handled users:update
	user, _ := h.getUserFromContext(r)
	if override && user != nil && h.AuthSrv != nil {
		// For maintenance override path, require maintenance:update; for compliance we already checked users:update above.
		// Allow either permission to proceed when override is requested.
		if !h.AuthSrv.Can(user.UserID, "maintenance", "update") && !h.AuthSrv.Can(user.UserID, "users", "update") && user.Role != "admin" {
			http.Error(w, "override requires maintenance:update or users:update permission", http.StatusForbidden)
			return
		}
	}

	err := h.assignDriverUC.Execute(r.Context(), tripapp.AssignDriverCommand{
		TripID:              tripagg.TripID(tripID),
		DriverID:            driverID,
		TenantID:            shared.TenantIDFromContext(r.Context()),
		OverrideMaintenance: override,
		OverrideReason:      reason,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "dispatch blocked") || strings.Contains(strings.ToLower(err.Error()), "compliance") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dispatch_blocked", "blocked_by": err.Error()})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "assigned", "trip_id": tripID, "driver_id": driverID})
		return
	}
	http.Redirect(w, r, "/trips/"+tripID, http.StatusSeeOther)
}

func (h *TripHandlers) AssignVehicle(w http.ResponseWriter, r *http.Request) {
	h.init()
	tripID := chi.URLParam(r, "id")
	vehicleID, override, reason := h.extractAssignParams(r, "vehicle_id")

	// Hard dispatch blocker via EnforceDispatchCompliance (Spec 21 §5)
	if h.Services != nil && h.Services.Compliance != nil {
		ctx := r.Context()
		var driverID string
		if h.DB != nil {
			var d sql.NullString
			_ = h.DB.QueryRowContext(ctx, `SELECT driver_id FROM trips WHERE id = ?`, tripID).Scan(&d)
			if d.Valid {
				driverID = d.String
			}
		}
		var dPtr *domain.DriverID
		var vPtr *domain.VehicleID
		if driverID != "" {
			did := domain.DriverID(driverID)
			dPtr = &did
		}
		if vehicleID != "" {
			vid := domain.VehicleID(vehicleID)
			vPtr = &vid
		}
		if err := h.Services.Compliance.EnforceDispatchCompliance(ctx, dPtr, vPtr); err != nil {
			if blocked := h.handleComplianceBlock(w, r, tripID, driverID, vehicleID, override, reason, err); blocked {
				return
			}
		}
	}

	user, _ := h.getUserFromContext(r)
	if override && user != nil && h.AuthSrv != nil {
		if !h.AuthSrv.Can(user.UserID, "maintenance", "update") && !h.AuthSrv.Can(user.UserID, "users", "update") && user.Role != "admin" {
			http.Error(w, "override requires maintenance:update or users:update permission", http.StatusForbidden)
			return
		}
	}

	err := h.assignVehicleUC.Execute(r.Context(), tripapp.AssignVehicleCommand{
		TripID:              tripagg.TripID(tripID),
		VehicleID:           vehicleID,
		TenantID:            shared.TenantIDFromContext(r.Context()),
		OverrideMaintenance: override,
		OverrideReason:      reason,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "dispatch blocked") || strings.Contains(strings.ToLower(err.Error()), "compliance") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "dispatch_blocked", "blocked_by": err.Error()})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.Contains(r.Header.Get("Accept"), "application/json") || strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "assigned", "trip_id": tripID, "vehicle_id": vehicleID})
		return
	}
	http.Redirect(w, r, "/trips/"+tripID, http.StatusSeeOther)
}

func (h *TripHandlers) StartTrip(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	err := h.startUC.Execute(r.Context(), tripapp.StartTripCommand{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "dispatch blocked") || strings.Contains(strings.ToLower(err.Error()), "compliance") {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}

func (h *TripHandlers) ReachPickup(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	err := h.reachPickupUC.Execute(r.Context(), tripapp.ReachPickupCommand{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}

func (h *TripHandlers) StartTransit(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	err := h.startTransitUC.Execute(r.Context(), tripapp.StartTransitCommand{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}

func (h *TripHandlers) Deliver(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	err := h.deliverUC.Execute(r.Context(), tripapp.DeliverCommand{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}

func (h *TripHandlers) CompleteTrip(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	tenant := shared.TenantIDFromContext(r.Context())
	tripID := tripagg.TripID(id)

	// The trip completion, closed-detention query, invoice generation and
	// detention status flip run in ONE UnitOfWork transaction so no torn
	// state can separate a completed trip from its invoice lines (Spec 02 §6).
	err := h.completeUC.Execute(r.Context(), tripapp.CompleteTripCommand{
		TripID:   tripID,
		TenantID: tenant,
		OnCompleted: func(txCtx ports.TxContext, trip *tripagg.TripAggregate) error {
			if trip.BookingID == nil {
				return nil
			}
			detentions, err := h.detRepo.ListClosedForTrip(txCtx, string(tenant), id)
			if err != nil {
				return err
			}
			if len(detentions) == 0 {
				return nil
			}

			lines := make([]invoiceApp.InvoiceLineItemInput, 0, len(detentions))
			for _, d := range detentions {
				tripIDStr := d.TripID
				description := "Detention"
				if d.ZoneName != "" {
					description = "Detention at " + d.ZoneName
				}
				lines = append(lines, invoiceApp.InvoiceLineItemInput{
					TripID:      &tripIDStr,
					LineType:    invoiceaggregate.LineTypeDetention,
					Description: description,
					Quantity:    float64(d.BillableSeconds) / 3600.0,
					UnitPrice:   d.RatePerHour,
					RefID:       &d.ID,
				})
			}

			// Booking pricing is read through the SAME transaction (no
			// second connection — shared-cache lock trap), then passed into
			// GenerateInTx which re-resolves company GST settings.
			bookingID := *trip.BookingID
			bookingRepo, ok := txCtx.Repositories().Bookings().(bookingdomain.BookingRepository)
			if !ok {
				return errors.New("failed to retrieve booking repository")
			}
			booking, err := bookingRepo.GetReadModel(txCtx, bookingaggregate.BookingID(bookingID), tenant)
			if err != nil {
				return err
			}
			subtotal := booking.Price
			tax := subtotal * 0.18 // 18% GST standard rate (re-resolved by company settings)
			_, attached, err := h.generateInvoiceUC.GenerateInTx(txCtx, invoiceApp.GenerateInvoiceCommand{
				TenantID:   tenant,
				BookingID:  bookingID,
				CustomerID: booking.CustomerID,
				TripID:     &id,
				Subtotal:   subtotal,
				Tax:        tax,
				Discount:   0,
				Total:      subtotal + tax,
				LineItems:  lines,
			})
			if err != nil {
				return err
			}
			// Paid/partially-paid invoice → nothing attached, detentions
			// stay closed so they can be handled manually (no data loss).
			if !attached {
				return nil
			}
			for _, d := range detentions {
				if err := h.detRepo.MarkAttached(txCtx, d.ID); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}

func (h *TripHandlers) CancelTrip(w http.ResponseWriter, r *http.Request) {
	h.init()
	id := chi.URLParam(r, "id")
	err := h.cancelUC.Execute(r.Context(), tripapp.CancelTripCommand{
		TripID:   tripagg.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/trips/"+id, http.StatusSeeOther)
}
