package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/trip/application"
	"transport-app/internal/trip/domain/aggregate"
)

// APITripHandler handles REST endpoints for the trip vertical slice.
type APITripHandler struct {
	createUC        *application.CreateTripUseCase
	assignDriverUC  *application.AssignDriverUseCase
	assignVehicleUC *application.AssignVehicleUseCase
	scheduleUC      *application.ScheduleTripUseCase
	startUC         *application.StartTripUseCase
	reachPickupUC   *application.ReachPickupUseCase
	startTransitUC  *application.StartTransitUseCase
	deliverUC       *application.DeliverUseCase
	completeUC      *application.CompleteTripUseCase
	cancelUC        *application.CancelTripUseCase
	getUC           *application.GetTripUseCase
	listUC          *application.ListTripsUseCase
	authSrv         auth.AuthorizationService
}

// NewAPITripHandler constructs an APITripHandler.
func NewAPITripHandler(
	createUC *application.CreateTripUseCase,
	assignDriverUC *application.AssignDriverUseCase,
	assignVehicleUC *application.AssignVehicleUseCase,
	scheduleUC *application.ScheduleTripUseCase,
	startUC *application.StartTripUseCase,
	reachPickupUC *application.ReachPickupUseCase,
	startTransitUC *application.StartTransitUseCase,
	deliverUC *application.DeliverUseCase,
	completeUC *application.CompleteTripUseCase,
	cancelUC *application.CancelTripUseCase,
	getUC *application.GetTripUseCase,
	listUC *application.ListTripsUseCase,
	authSrv auth.AuthorizationService,
) *APITripHandler {
	return &APITripHandler{
		createUC:        createUC,
		assignDriverUC:  assignDriverUC,
		assignVehicleUC: assignVehicleUC,
		scheduleUC:      scheduleUC,
		startUC:         startUC,
		reachPickupUC:   reachPickupUC,
		startTransitUC:  startTransitUC,
		deliverUC:       deliverUC,
		completeUC:      completeUC,
		cancelUC:        cancelUC,
		getUC:           getUC,
		listUC:          listUC,
		authSrv:         authSrv,
	}
}

// Register mounts all trip routes.
func (h *APITripHandler) Register(r chi.Router) {
	r.Route("/api/v1/trips", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.authSrv, "trips", "create")).Post("/", h.Create)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "read")).Get("/", h.List)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "read")).Get("/{id}", h.Get)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "assign")).Post("/{id}/assign-driver", h.AssignDriver)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "assign")).Post("/{id}/assign-vehicle", h.AssignVehicle)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/schedule", h.Schedule)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/start", h.Start)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/reach-pickup", h.ReachPickup)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/in-transit", h.StartTransit)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/deliver", h.Deliver)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/complete", h.Complete)
		r.With(middleware.RequirePermission(h.authSrv, "trips", "update")).Post("/{id}/cancel", h.Cancel)
	})
}

func (h *APITripHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BookingID      *string `json:"booking_id"`
		RouteID        string  `json:"route_id"`
		DepartureTime  string  `json:"departure_time"`
		Remarks        string  `json:"remarks"`
		IdempotencyKey string  `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}

	depTime, err := time.Parse(time.RFC3339, req.DepartureTime)
	if err != nil {
		http.Error(w, "departure_time must be RFC3339", http.StatusBadRequest)
		return
	}

	id, err := h.createUC.Execute(r.Context(), application.CreateTripCommand{
		TenantID:       shared.TenantIDFromContext(r.Context()),
		BookingID:      req.BookingID,
		RouteID:        req.RouteID,
		DepartureTime:  depTime,
		Remarks:        req.Remarks,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": string(id)})
}

func (h *APITripHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}

	driverIDFilter := r.URL.Query().Get("driver_id")
	if driverIDFilter == "me" {
		session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
		if !ok || session == nil || session.UserID == "" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}

		res, err := h.listUC.Execute(r.Context(), application.ListTripsQuery{
			TenantID:   shared.TenantIDFromContext(r.Context()),
			Page:       page,
			Limit:      limit,
			Search:     r.URL.Query().Get("search"),
			Status:     r.URL.Query().Get("status"),
			DriverID:   "me",
			AuthUserID: session.UserID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no driver linked to this user"})
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to list trips"})
			return
		}

		type MobileTripDTO struct {
			ID            string `json:"id"`
			TripNumber    string `json:"trip_number"`
			DriverName    string `json:"driver_name"`
			VehiclePlate  string `json:"vehicle_plate"`
			Origin        string `json:"origin"`
			Destination   string `json:"destination"`
			Status        string `json:"status"`
			DepartureTime string `json:"departure_time"`
		}

		trips := make([]MobileTripDTO, len(res.Trips))
		for i, t := range res.Trips {
			driverName := strings.TrimSpace(t.DriverFirstName + " " + t.DriverLastName)
			depTime := ""
			if !t.DepartureTime.IsZero() {
				depTime = t.DepartureTime.Format(time.RFC3339)
			}
			vehiclePlate := t.VehicleRegistrationNumber
			if vehiclePlate == "" {
				vehiclePlate = t.VehicleNumber
			}
			trips[i] = MobileTripDTO{
				ID:            t.ID,
				TripNumber:    t.TripNumber,
				DriverName:    driverName,
				VehiclePlate:  vehiclePlate,
				Origin:        t.RouteSource,
				Destination:   t.RouteDestination,
				Status:        t.Status,
				DepartureTime: depTime,
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"trips": trips,
			"total": res.Total,
		})
		return
	}

	res, err := h.listUC.Execute(r.Context(), application.ListTripsQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     page,
		Limit:    limit,
		Search:   r.URL.Query().Get("search"),
		Status:   r.URL.Query().Get("status"),
		DriverID: driverIDFilter,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to list trips"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"trips": res.Trips, "total": res.Total})
}

func (h *APITripHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := h.getUC.Execute(r.Context(), application.GetTripQuery{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		http.Error(w, "Trip not found", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(res)
}

func (h *APITripHandler) AssignDriver(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		DriverID            string `json:"driver_id"`
		OverrideMaintenance bool   `json:"override_maintenance"`
		OverrideReason      string `json:"override_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.OverrideMaintenance && h.authSrv != nil {
		if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
			if !h.authSrv.Can(s.UserID, "maintenance", "update") {
				http.Error(w, "maintenance override requires maintenance:update permission", http.StatusForbidden)
				return
			}
		}
	}
	if err := h.assignDriverUC.Execute(r.Context(), application.AssignDriverCommand{
		TripID:              aggregate.TripID(id),
		DriverID:            req.DriverID,
		TenantID:            shared.TenantIDFromContext(r.Context()),
		OverrideMaintenance: req.OverrideMaintenance,
		OverrideReason:      req.OverrideReason,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "driver_assigned"})
}

func (h *APITripHandler) AssignVehicle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		VehicleID           string `json:"vehicle_id"`
		OverrideMaintenance bool   `json:"override_maintenance"`
		OverrideReason      string `json:"override_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.OverrideMaintenance && h.authSrv != nil {
		if s, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && s != nil {
			if !h.authSrv.Can(s.UserID, "maintenance", "update") {
				http.Error(w, "maintenance override requires maintenance:update permission", http.StatusForbidden)
				return
			}
		}
	}
	if err := h.assignVehicleUC.Execute(r.Context(), application.AssignVehicleCommand{
		TripID:              aggregate.TripID(id),
		VehicleID:           req.VehicleID,
		TenantID:            shared.TenantIDFromContext(r.Context()),
		OverrideMaintenance: req.OverrideMaintenance,
		OverrideReason:      req.OverrideReason,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "vehicle_assigned"})
}

func (h *APITripHandler) Schedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.scheduleUC.Execute(r.Context(), application.ScheduleTripCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "scheduled"})
}

func (h *APITripHandler) Start(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.startUC.Execute(r.Context(), application.StartTripCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started"})
}

func (h *APITripHandler) ReachPickup(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.reachPickupUC.Execute(r.Context(), application.ReachPickupCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "reached_pickup"})
}

func (h *APITripHandler) StartTransit(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.startTransitUC.Execute(r.Context(), application.StartTransitCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "in_transit"})
}

func (h *APITripHandler) Deliver(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.deliverUC.Execute(r.Context(), application.DeliverCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "delivered"})
}

func (h *APITripHandler) Complete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.completeUC.Execute(r.Context(), application.CompleteTripCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

func (h *APITripHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.cancelUC.Execute(r.Context(), application.CancelTripCommand{
		TripID:   aggregate.TripID(id),
		TenantID: shared.TenantIDFromContext(r.Context()),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}
