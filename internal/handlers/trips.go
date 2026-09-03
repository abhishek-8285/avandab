package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	bookingdomain "transport-app/internal/booking/domain"
	bookingaggregate "transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/config"
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
	reachStopUC       *tripapp.ReachStopUseCase
	submitStopPODUC   *tripapp.SubmitStopPODUseCase
	completeStopUC    *tripapp.CompleteStopUseCase
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
		h.reachStopUC = tripapp.NewReachStopUseCase(uowImpl, clockImpl)
		h.submitStopPODUC = tripapp.NewSubmitStopPODUseCase(uowImpl, clockImpl)
		h.completeStopUC = tripapp.NewCompleteStopUseCase(uowImpl, clockImpl)
		h.detRepo = geofencerepo.NewEventLogRepository(h.DB)
	}
}

func (h *TripHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "create")).Get("/new", h.New)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "create")).Post("/new", h.Create)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/{id}", h.View)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/{id}/playback", h.Playback)
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
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/stops/{stopId}/reach", h.ReachStop)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/stops/{stopId}/pod", h.SubmitStopPOD)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/stops/{stopId}/complete", h.CompleteStop)
	r.Get("/{id}/epod", h.PublicEPODCertificate)
	r.Get("/{id}/stops/{stopId}/epod", h.PublicEPODCertificate)
	r.With(middleware.ResourcePermission(h.AuthSrv, "shares", "create")).Post("/{id}/share", h.App.Share.CreateShare)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "read")).Get("/{id}/compliance", h.TripComplianceFragment)
	r.With(middleware.ResourcePermission(h.AuthSrv, "trips", "update")).Post("/{id}/send-pod-otp", h.SendPODOTPSMS)
}

// Playback renders the trip playback page (GET /trips/{id}/playback) —
// Samsara-style animated replay of where the vehicle went.
func (h *TripHandlers) Playback(w http.ResponseWriter, r *http.Request) {
	h.init()
	session, _ := h.getUserFromContext(r)
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		tripID = r.URL.Query().Get("id")
	}
	trip, err := h.getUC.Execute(r.Context(), tripapp.GetTripQuery{
		TripID:   tripagg.TripID(tripID),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		h.renderError(w, http.StatusNotFound, "Trip Not Found", "No trip found with ID "+tripID+".", session)
		return
	}
	cfg := h.Config
	if cfg == nil {
		cfg = &config.Config{LiveMap: config.LiveMapConfig{
			MapTileProvider: "auto",
			MapGoogleStyle:  "m",
			MapGL:           "IN",
			MapOSMURL:       "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
			MapPollSec:      10,
		}}
	}
	vehicleID := ""
	if trip.VehicleID != nil {
		vehicleID = *trip.VehicleID
	}
	h.renderPage(w, r, "trip_playback.html", PageData{
		Title: "Trip Playback",
		User:  session,
		Extra: map[string]interface{}{
			"MapAssets": true,
			"MapConfig": map[string]interface{}{
				"Provider":    cfg.LiveMap.MapTileProvider,
				"GoogleStyle": cfg.LiveMap.MapGoogleStyle,
				"GL":          cfg.LiveMap.MapGL,
				"OSMUrl":      cfg.LiveMap.MapOSMURL,
				"PollSec":     cfg.LiveMap.MapPollSec,
			},
			"Trip": trip,
			"PlaybackConfig": map[string]interface{}{
				"TripID":      tripID,
				"VehicleID":   vehicleID,
				"HistoryAPI":  "/api/v1/telemetry/history",
				"PlaybackAPI": "/api/v1/trips/" + tripID + "/playback",
			},
		},
	})
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
		DateFrom: pp.DateFrom,
		DateTo:   pp.DateTo,
	})
	if err != nil {
		fmt.Printf("[Trips Error] Failed to list trips: %v\n", err)
		http.Error(w, "Failed to load trips", http.StatusInternalServerError)
		return
	}

	pd := newPaginationData(pp, res.Total, "/trips")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo

	if isDatastarRequest(r) {
		h.renderFragment(w, "trip_list.html", map[string]interface{}{
			"Trips":        res.Trips,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
			"KPIs":         h.tripKPIs(r.Context()),
		})
		return
	}

	h.renderPage(w, r, "trip_list.html", PageData{
		Title: "Trips",
		User:  session,
		Extra: map[string]interface{}{"Trips": res.Trips, "Pagination": pd, "Query": pp.Query, "StatusFilter": pp.Status, "DateFrom": pp.DateFrom, "DateTo": pp.DateTo, "KPIs": h.tripKPIs(r.Context())},
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

	type stopItem struct {
		ID              string
		StopSequence    int
		StopType        string
		LocationName    string
		Address         string
		Status          string
		ActualArrival   *time.Time
		ActualDeparture *time.Time
		RequiresPOD     bool
		PODUrl          string
		RequiresOTP     bool
		ConsigneeName   string
		ConsigneePhone  string
	}
	type progressionInfo struct {
		TotalStops        int
		CompletedStops    int
		ProgressPercent   float64
		AllStopsCompleted bool
	}
	var stopsList []stopItem
	var currentStop *stopItem
	var progression *progressionInfo

	if h.DB != nil {
		rows, err := h.DB.QueryContext(r.Context(), `
			SELECT id, stop_sequence, stop_type, COALESCE(location_name, ''), COALESCE(address, ''),
			       status, actual_arrival, actual_departure, COALESCE(requires_pod, 0), COALESCE(pod_url, ''),
			       COALESCE(requires_otp, 0), COALESCE(consignee_name, ''), COALESCE(consignee_phone, '')
			FROM trip_stops
			WHERE trip_id = ?
			ORDER BY stop_sequence ASC
		`, id)
		if err == nil {
			defer func() { _ = rows.Close() }()
			completedCount := 0
			for rows.Next() {
				var s stopItem
				var arrStr, depStr sql.NullString
				var reqPOD, reqOTP int
				if err := rows.Scan(&s.ID, &s.StopSequence, &s.StopType, &s.LocationName, &s.Address, &s.Status, &arrStr, &depStr, &reqPOD, &s.PODUrl, &reqOTP, &s.ConsigneeName, &s.ConsigneePhone); err == nil {
					s.RequiresPOD = reqPOD == 1
					s.RequiresOTP = reqOTP == 1
					if arrStr.Valid && arrStr.String != "" {
						if t, err := time.Parse(time.RFC3339, arrStr.String); err == nil {
							s.ActualArrival = &t
						}
					}
					if depStr.Valid && depStr.String != "" {
						if t, err := time.Parse(time.RFC3339, depStr.String); err == nil {
							s.ActualDeparture = &t
						}
					}
					if s.Status == "completed" {
						completedCount++
					} else if (s.Status != "skipped") && currentStop == nil {
						sCopy := s
						currentStop = &sCopy
					}
					stopsList = append(stopsList, s)
				}
			}
			if len(stopsList) > 0 {
				prog := progressionInfo{
					TotalStops:        len(stopsList),
					CompletedStops:    completedCount,
					ProgressPercent:   float64(completedCount) / float64(len(stopsList)) * 100.0,
					AllStopsCompleted: completedCount == len(stopsList),
				}
				if prog.AllStopsCompleted {
					prog.ProgressPercent = 100.0
				}
				progression = &prog
			}
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
			"Stops":             stopsList,
			"CurrentStop":       currentStop,
			"Progression":       progression,
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

func (h *TripHandlers) ReachStop(w http.ResponseWriter, r *http.Request) {
	h.init()
	tripID := chi.URLParam(r, "id")
	stopID := chi.URLParam(r, "stopId")
	tenantID := shared.TenantIDFromContext(r.Context())

	err := h.reachStopUC.Execute(r.Context(), tripapp.ReachStopCommand{
		TripID:   tripagg.TripID(tripID),
		StopID:   stopID,
		TenantID: tenantID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "arrived", "stop_id": stopID, "trip_id": tripID})
		return
	}
	http.Redirect(w, r, "/trips/"+tripID, http.StatusSeeOther)
}

// safePODExtension returns a sanitized, whitelisted extension for saved POD files.
func safePODExtension(filename, contentType string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filepath.Clean(filename))))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".svg":
		if ext == ".jpeg" {
			return ".jpg"
		}
		return ext
	}
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".jpg"
	}
}

func saveUploadedPODFile(header *multipart.FileHeader, baseDir string) (string, error) {
	if header == nil {
		return "", nil
	}
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, 512)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return "", err
	}
	if n == 0 {
		return "", nil
	}

	detected := http.DetectContentType(buf[:n])
	ext := safePODExtension(header.Filename, detected)

	podDir := filepath.Join(baseDir, "pod")
	if err := os.MkdirAll(podDir, 0o750); err != nil {
		return "", err
	}

	filename := uuid.NewString() + ext
	targetPath := filepath.Join(podDir, filename)

	dest, err := os.Create(targetPath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dest, io.MultiReader(bytes.NewReader(buf[:n]), file)); err != nil {
		_ = dest.Close()
		_ = os.Remove(targetPath)
		return "", err
	}
	if err := dest.Close(); err != nil {
		_ = os.Remove(targetPath)
		return "", err
	}

	return "/uploads/pod/" + filename, nil
}

func saveBase64PODImage(dataURI, baseDir string) (string, error) {
	parts := strings.SplitN(dataURI, ",", 2)
	if len(parts) != 2 {
		return "", errors.New("invalid data uri")
	}
	header := parts[0]
	data := parts[1]

	ext := ".png"
	if strings.Contains(header, "image/jpeg") || strings.Contains(header, "image/jpg") {
		ext = ".jpg"
	} else if strings.Contains(header, "image/webp") {
		ext = ".webp"
	}

	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", err
	}

	podDir := filepath.Join(baseDir, "pod")
	if err := os.MkdirAll(podDir, 0o750); err != nil {
		return "", err
	}

	filename := uuid.NewString() + ext
	targetPath := filepath.Join(podDir, filename)
	if err := os.WriteFile(targetPath, raw, 0o600); err != nil {
		return "", err
	}
	return "/uploads/pod/" + filename, nil
}

func (h *TripHandlers) SubmitStopPOD(w http.ResponseWriter, r *http.Request) {
	h.init()
	tripID := chi.URLParam(r, "id")
	if tripID == "" {
		tripID = chi.URLParam(r, "tripId")
	}
	stopID := chi.URLParam(r, "stopId")
	if stopID == "" {
		stopID = chi.URLParam(r, "stop_id")
	}
	tenantID := shared.TenantIDFromContext(r.Context())
	if tenantID == "" {
		tenantID = shared.DefaultTenant
	}

	uploadBaseDir := "./uploads"
	if h.Config != nil && h.Config.UploadDir != "" {
		uploadBaseDir = h.Config.UploadDir
	}

	var podURL, signatureURL, notes, otp string
	ct := r.Header.Get("Content-Type")

	if strings.Contains(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
		if err := r.ParseMultipartForm(25 << 20); err == nil {
			podURL = r.FormValue("pod_url")
			signatureURL = r.FormValue("signature_url")
			notes = r.FormValue("notes")
			otp = r.FormValue("otp")
			if stopID == "" {
				stopID = r.FormValue("stop_id")
			}
			if tripID == "" {
				tripID = r.FormValue("trip_id")
			}

			// Direct POD photo / document file upload
			for _, key := range []string{"pod_file", "photo", "pod_photo", "file"} {
				if fhs, ok := r.MultipartForm.File[key]; ok && len(fhs) > 0 {
					if saved, err := saveUploadedPODFile(fhs[0], uploadBaseDir); err == nil && saved != "" {
						podURL = saved
						break
					}
				}
			}

			// Direct consignee signature file upload
			for _, key := range []string{"signature_file", "signature", "signature_photo"} {
				if fhs, ok := r.MultipartForm.File[key]; ok && len(fhs) > 0 {
					if saved, err := saveUploadedPODFile(fhs[0], uploadBaseDir); err == nil && saved != "" {
						signatureURL = saved
						break
					}
				}
			}

			// Signature canvas data URI support
			sigData := r.FormValue("signature_data")
			if sigData == "" && strings.HasPrefix(r.FormValue("signature"), "data:image/") {
				sigData = r.FormValue("signature")
			}
			if strings.HasPrefix(sigData, "data:image/") {
				if saved, err := saveBase64PODImage(sigData, uploadBaseDir); err == nil && saved != "" {
					signatureURL = saved
				}
			}
		}
	} else if strings.Contains(ct, "application/json") {
		var req struct {
			TripID       string `json:"trip_id"`
			StopID       string `json:"stop_id"`
			PODURL       string `json:"pod_url"`
			SignatureURL string `json:"signature_url"`
			Notes        string `json:"notes"`
			OTP          string `json:"otp"`
			Photo        string `json:"photo"`
			Signature    string `json:"signature"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if tripID == "" && req.TripID != "" {
			tripID = req.TripID
		}
		if stopID == "" && req.StopID != "" {
			stopID = req.StopID
		}
		podURL = req.PODURL
		if podURL == "" && req.Photo != "" {
			podURL = req.Photo
		}
		signatureURL = req.SignatureURL
		if signatureURL == "" && req.Signature != "" {
			signatureURL = req.Signature
		}
		if strings.HasPrefix(signatureURL, "data:image/") {
			if saved, err := saveBase64PODImage(signatureURL, uploadBaseDir); err == nil && saved != "" {
				signatureURL = saved
			}
		}
		notes = req.Notes
		otp = req.OTP
	} else {
		_ = r.ParseForm()
		if tripID == "" {
			tripID = r.FormValue("trip_id")
		}
		if stopID == "" {
			stopID = r.FormValue("stop_id")
		}
		podURL = r.FormValue("pod_url")
		if podURL == "" {
			podURL = r.FormValue("photo")
		}
		signatureURL = r.FormValue("signature_url")
		if signatureURL == "" {
			signatureURL = r.FormValue("signature")
		}
		sigData := r.FormValue("signature_data")
		if strings.HasPrefix(sigData, "data:image/") {
			if saved, err := saveBase64PODImage(sigData, uploadBaseDir); err == nil && saved != "" {
				signatureURL = saved
			}
		} else if strings.HasPrefix(signatureURL, "data:image/") {
			if saved, err := saveBase64PODImage(signatureURL, uploadBaseDir); err == nil && saved != "" {
				signatureURL = saved
			}
		}
		notes = r.FormValue("notes")
		otp = r.FormValue("otp")
	}

	// Auto-locate default stop if omitted
	if stopID == "" && h.App != nil && h.App.DB != nil {
		var sID string
		_ = h.App.DB.QueryRowContext(r.Context(),
			`SELECT id FROM trip_stops WHERE trip_id = ? ORDER BY stop_sequence DESC LIMIT 1`, tripID).Scan(&sID)
		if sID != "" {
			stopID = sID
		}
	}

	err := h.submitStopPODUC.Execute(r.Context(), tripapp.SubmitStopPODCommand{
		TripID:       tripagg.TripID(tripID),
		StopID:       stopID,
		TenantID:     tenantID,
		PODURL:       podURL,
		SignatureURL: signatureURL,
		Notes:        notes,
		OTP:          otp,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Mirror to legacy trips table and stop_pod_attachments for cross-compatibility
	if h.App != nil && h.App.DB != nil {
		_, _ = h.App.DB.ExecContext(r.Context(), `
			UPDATE trips
			SET pod_url = COALESCE(NULLIF(?, ''), pod_url),
			    pod_photo_url = COALESCE(NULLIF(?, ''), pod_photo_url),
			    pod_signature_url = COALESCE(NULLIF(?, ''), pod_signature_url),
			    pod_notes = COALESCE(NULLIF(?, ''), pod_notes),
			    pod_captured_at = CURRENT_TIMESTAMP
			WHERE id = ? OR trip_number = ?`,
			podURL, podURL, signatureURL, notes, tripID, tripID,
		)
		if podURL != "" && stopID != "" {
			_, _ = h.App.DB.ExecContext(r.Context(), `
				INSERT INTO stop_pod_attachments (id, tenant_id, stop_id, trip_id, file_url, file_type, created_at)
				VALUES (?, ?, ?, ?, ?, 'photo', CURRENT_TIMESTAMP)`,
				uuid.NewString(), string(tenantID), stopID, tripID, podURL,
			)
		}
		if signatureURL != "" && stopID != "" {
			_, _ = h.App.DB.ExecContext(r.Context(), `
				INSERT INTO stop_pod_attachments (id, tenant_id, stop_id, trip_id, file_url, file_type, created_at)
				VALUES (?, ?, ?, ?, ?, 'signature', CURRENT_TIMESTAMP)`,
				uuid.NewString(), string(tenantID), stopID, tripID, signatureURL,
			)
		}
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "pod_verified",
			"stop_id":       stopID,
			"trip_id":       tripID,
			"pod_url":       podURL,
			"signature_url": signatureURL,
			"epod_url":      "/epod/" + tripID,
		})
		return
	}
	http.Redirect(w, r, "/epod/"+tripID, http.StatusSeeOther)
}

// EPODReceiptStopView represents an individual stop in the e-POD journey summary.
type EPODReceiptStopView struct {
	ID              string
	StopSequence    int
	StopType        string
	LocationName    string
	Address         string
	Status          string
	ActualArrival   string
	ActualDeparture string
	ConsigneeName   string
	ConsigneePhone  string
	ConsigneeEmail  string
	OTPRequired     bool
	OTPVerified     bool
	OTPVerifiedAt   string
	PODRequired     bool
	PODVerified     bool
	PODURL          string
	SignatureURL    string
	Notes           string
	IsSelected      bool
}

// EPODReceiptView holds all template data for the public e-POD verification page.
type EPODReceiptView struct {
	TripID            string
	TripNumber        string
	Status            string
	CompanyName       string
	CompanyLogo       string
	VehicleReg        string
	DriverName        string
	DriverPhone       string
	DepartureTime     string
	ArrivalTime       string
	DeliveredAt       string
	StopSequence      int
	StopType          string
	LocationName      string
	Address           string
	ConsigneeName     string
	ConsigneePhone    string
	ConsigneeEmail    string
	OTPRequired       bool
	OTPVerified       bool
	OTPVerifiedAt     string
	PODRequired       bool
	PODVerified       bool
	PODURL            string
	SignatureURL      string
	Notes             string
	VerificationHash  string
	CertificateNumber string
	GeneratedAt       string
	Stops             []EPODReceiptStopView
}

// PublicEPODCertificate renders the public verified electronic proof of delivery certificate (GET /epod/{tripId}).
func (h *TripHandlers) PublicEPODCertificate(w http.ResponseWriter, r *http.Request) {
	h.init()
	tripID := chi.URLParam(r, "tripId")
	if tripID == "" {
		tripID = chi.URLParam(r, "id")
	}
	stopID := chi.URLParam(r, "stopId")

	if tripID == "" {
		http.Error(w, "Trip ID required", http.StatusBadRequest)
		return
	}

	if h.App == nil || h.App.DB == nil {
		http.Error(w, "Database unavailable", http.StatusInternalServerError)
		return
	}

	var (
		resolvedTripID, tenantID, tripNumber, status string
		departureTime                                time.Time
		arrivalTime                                  sql.NullTime
		startedAt, reachedPickupAt                   sql.NullTime
		inTransitAt, deliveredAt                     sql.NullTime
		completedAt                                  sql.NullTime
		driverID, vehicleID                          sql.NullString
		podURL, podPhotoURL                          sql.NullString
		podSigURL, podConsName                       sql.NullString
		podConsPhone                                 sql.NullString
		podOTPVerified                               int
		podCapturedAt                                sql.NullTime
		podNotes                                     sql.NullString
	)

	err := h.App.DB.QueryRowContext(r.Context(), `
		SELECT id, tenant_id, trip_number, status, departure_time, arrival_time,
		       started_at, reached_pickup_at, in_transit_at, delivered_at, completed_at,
		       COALESCE(driver_id, ''), COALESCE(vehicle_id, ''),
		       COALESCE(pod_url, ''), COALESCE(pod_photo_url, ''), COALESCE(pod_signature_url, ''),
		       COALESCE(pod_consignee_name, ''), COALESCE(pod_consignee_phone, ''),
		       COALESCE(pod_otp_verified, 0), pod_captured_at, COALESCE(pod_notes, '')
		FROM trips
		WHERE id = ? OR trip_number = ?
		LIMIT 1
	`, tripID, tripID).Scan(
		&resolvedTripID, &tenantID, &tripNumber, &status, &departureTime, &arrivalTime,
		&startedAt, &reachedPickupAt, &inTransitAt, &deliveredAt, &completedAt,
		&driverID, &vehicleID,
		&podURL, &podPhotoURL, &podSigURL,
		&podConsName, &podConsPhone,
		&podOTPVerified, &podCapturedAt, &podNotes,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "e-POD Certificate not found for trip: "+tripID, http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to load trip: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Lookup vehicle
	var vehicleReg string
	if vehicleID.Valid && vehicleID.String != "" {
		_ = h.App.DB.QueryRowContext(r.Context(), `
			SELECT COALESCE(registration_number, vehicle_number, '') FROM vehicles WHERE id = ?
		`, vehicleID.String).Scan(&vehicleReg)
	}

	// Lookup driver
	var driverName, driverPhone string
	if driverID.Valid && driverID.String != "" {
		_ = h.App.DB.QueryRowContext(r.Context(), `
			SELECT COALESCE(first_name || ' ' || last_name, ''), COALESCE(phone, '')
			FROM drivers WHERE id = ? OR driver_id = ?
		`, driverID.String, driverID.String).Scan(&driverName, &driverPhone)
	}

	// Lookup company settings / name & logo
	companyName := "FlyFleet Logistics"
	companyLogo := ""
	var cName, cLogo sql.NullString
	if err := h.App.DB.QueryRowContext(r.Context(), `
		SELECT COALESCE(company_name, ''), COALESCE(logo_url, '') FROM company_settings WHERE id = 1 LIMIT 1
	`).Scan(&cName, &cLogo); err == nil {
		if cName.Valid && cName.String != "" {
			companyName = cName.String
		}
		if cLogo.Valid && cLogo.String != "" {
			companyLogo = cLogo.String
		}
	}
	if companyName == "FlyFleet Logistics" && tenantID != "" {
		var tName sql.NullString
		if err := h.App.DB.QueryRowContext(r.Context(), `SELECT COALESCE(name, '') FROM tenants WHERE id = ?`, tenantID).Scan(&tName); err == nil && tName.Valid && tName.String != "" {
			companyName = tName.String
		}
	}

	// Query trip_stops
	rows, err := h.App.DB.QueryContext(r.Context(), `
		SELECT id, stop_sequence, stop_type, COALESCE(location_name, ''), COALESCE(address, ''),
		       status, actual_arrival, actual_departure,
		       COALESCE(otp_required, 0), otp_verified_at,
		       COALESCE(pod_required, 0), COALESCE(pod_url, ''), COALESCE(pod_signature_url, ''),
		       pod_verified_at, COALESCE(pod_notes, ''),
		       COALESCE(consignee_name, ''), COALESCE(consignee_phone, ''), COALESCE(consignee_email, '')
		FROM trip_stops
		WHERE trip_id = ?
		ORDER BY stop_sequence ASC
	`, resolvedTripID)

	var stops []EPODReceiptStopView
	var selectedStop *EPODReceiptStopView

	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var s EPODReceiptStopView
			var arrTime, depTime, otpVerTime, podVerTime sql.NullTime
			var reqOTP, reqPOD int
			if err := rows.Scan(
				&s.ID, &s.StopSequence, &s.StopType, &s.LocationName, &s.Address,
				&s.Status, &arrTime, &depTime,
				&reqOTP, &otpVerTime,
				&reqPOD, &s.PODURL, &s.SignatureURL,
				&podVerTime, &s.Notes,
				&s.ConsigneeName, &s.ConsigneePhone, &s.ConsigneeEmail,
			); err == nil {
				s.OTPRequired = reqOTP == 1
				s.PODRequired = reqPOD == 1
				if otpVerTime.Valid {
					s.OTPVerified = true
					s.OTPVerifiedAt = otpVerTime.Time.Format("02 Jan 2006, 15:04 MST")
				}
				if podVerTime.Valid {
					s.PODVerified = true
				}
				if arrTime.Valid {
					s.ActualArrival = arrTime.Time.Format("02 Jan 2006, 15:04")
				}
				if depTime.Valid {
					s.ActualDeparture = depTime.Time.Format("02 Jan 2006, 15:04")
				}

				if stopID != "" && s.ID == stopID {
					s.IsSelected = true
					sc := s
					selectedStop = &sc
				}
				stops = append(stops, s)
			}
		}
	}

	// Pick default selected stop if not explicitly chosen
	if selectedStop == nil && len(stops) > 0 {
		for i := range stops {
			if stops[i].PODURL != "" || stops[i].SignatureURL != "" || stops[i].PODVerified {
				stops[i].IsSelected = true
				selectedStop = &stops[i]
				break
			}
		}
		if selectedStop == nil {
			for i := len(stops) - 1; i >= 0; i-- {
				if stops[i].StopType == "drop" || stops[i].Status == "completed" {
					stops[i].IsSelected = true
					selectedStop = &stops[i]
					break
				}
			}
		}
		if selectedStop == nil {
			stops[len(stops)-1].IsSelected = true
			selectedStop = &stops[len(stops)-1]
		}
	}

	// Prepare final view data
	finalPODURL := podPhotoURL.String
	if finalPODURL == "" {
		finalPODURL = podURL.String
	}
	finalSigURL := podSigURL.String
	finalConsName := podConsName.String
	finalConsPhone := podConsPhone.String
	finalNotes := podNotes.String
	finalOTPVerified := podOTPVerified == 1
	var finalOTPVerifiedAt string
	if finalOTPVerified && podCapturedAt.Valid {
		finalOTPVerifiedAt = podCapturedAt.Time.Format("02 Jan 2006, 15:04 MST")
	}

	stopSeq := 1
	stopType := "Delivery / Drop"
	locName := "Main Facility"
	addr := ""
	consEmail := ""

	if selectedStop != nil {
		stopSeq = selectedStop.StopSequence
		if selectedStop.StopType != "" {
			stopType = selectedStop.StopType
		}
		if selectedStop.LocationName != "" {
			locName = selectedStop.LocationName
		}
		if selectedStop.Address != "" {
			addr = selectedStop.Address
		}
		if selectedStop.ConsigneeName != "" {
			finalConsName = selectedStop.ConsigneeName
		}
		if selectedStop.ConsigneePhone != "" {
			finalConsPhone = selectedStop.ConsigneePhone
		}
		if selectedStop.ConsigneeEmail != "" {
			consEmail = selectedStop.ConsigneeEmail
		}
		if selectedStop.PODURL != "" {
			finalPODURL = selectedStop.PODURL
		}
		if selectedStop.SignatureURL != "" {
			finalSigURL = selectedStop.SignatureURL
		}
		if selectedStop.Notes != "" {
			finalNotes = selectedStop.Notes
		}
		if selectedStop.OTPVerified {
			finalOTPVerified = true
			if selectedStop.OTPVerifiedAt != "" {
				finalOTPVerifiedAt = selectedStop.OTPVerifiedAt
			}
		}
	}

	delivTimestamp := ""
	if deliveredAt.Valid {
		delivTimestamp = deliveredAt.Time.Format("02 Jan 2006, 15:04 MST")
	} else if arrivalTime.Valid {
		delivTimestamp = arrivalTime.Time.Format("02 Jan 2006, 15:04 MST")
	} else if podCapturedAt.Valid {
		delivTimestamp = podCapturedAt.Time.Format("02 Jan 2006, 15:04 MST")
	}

	// Generate tamper-proof SHA-256 digital verification hash
	rawHashInput := fmt.Sprintf("%s:%s:%s:%s:%s:%s", resolvedTripID, tripNumber, vehicleReg, finalConsName, delivTimestamp, tenantID)
	hBytes := sha256.Sum256([]byte(rawHashInput))
	verHash := strings.ToUpper(hex.EncodeToString(hBytes[:]))

	certNumber := "EPOD-" + tripNumber
	if strings.Contains(tripNumber, "TRIP-") {
		certNumber = strings.Replace(tripNumber, "TRIP-", "EPOD-", 1)
	}

	view := EPODReceiptView{
		TripID:            resolvedTripID,
		TripNumber:        tripNumber,
		Status:            status,
		CompanyName:       companyName,
		CompanyLogo:       companyLogo,
		VehicleReg:        vehicleReg,
		DriverName:        driverName,
		DriverPhone:       driverPhone,
		DepartureTime:     departureTime.Format("02 Jan 2006, 15:04"),
		DeliveredAt:       delivTimestamp,
		StopSequence:      stopSeq,
		StopType:          stopType,
		LocationName:      locName,
		Address:           addr,
		ConsigneeName:     finalConsName,
		ConsigneePhone:    finalConsPhone,
		ConsigneeEmail:    consEmail,
		OTPRequired:       true,
		OTPVerified:       finalOTPVerified,
		OTPVerifiedAt:     finalOTPVerifiedAt,
		PODRequired:       true,
		PODVerified:       finalPODURL != "" || finalSigURL != "" || finalOTPVerified,
		PODURL:            finalPODURL,
		SignatureURL:      finalSigURL,
		Notes:             finalNotes,
		VerificationHash:  verHash,
		CertificateNumber: certNumber,
		GeneratedAt:       time.Now().Format("02 Jan 2006, 15:04 MST"),
		Stops:             stops,
	}

	if wantsJSON(r) || r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(view)
		return
	}

	h.renderStandalone(w, "epod_receipt.html", view)
}

func (h *TripHandlers) renderStandalone(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if h.App == nil || h.App.Templates == nil {
		http.Error(w, "templates not initialized", http.StatusInternalServerError)
		return
	}
	tmpl := h.App.Templates.Lookup(name)
	if tmpl == nil {
		http.Error(w, fmt.Sprintf("template %q not found", name), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

func (h *TripHandlers) CompleteStop(w http.ResponseWriter, r *http.Request) {
	h.init()
	tripID := chi.URLParam(r, "id")
	stopID := chi.URLParam(r, "stopId")
	tenantID := shared.TenantIDFromContext(r.Context())

	err := h.completeStopUC.Execute(r.Context(), tripapp.CompleteStopCommand{
		TripID:   tripagg.TripID(tripID),
		StopID:   stopID,
		TenantID: tenantID,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed", "stop_id": stopID, "trip_id": tripID})
		return
	}
	http.Redirect(w, r, "/trips/"+tripID, http.StatusSeeOther)
}
