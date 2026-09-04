package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/booking/application"
	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/booking/presentation/api/dto"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type APIBookingHandler struct {
	createUC   *application.CreateBookingUseCase
	confirmUC  *application.ConfirmBookingUseCase
	cancelUC   *application.CancelBookingUseCase
	updateUC   *application.UpdateBookingUseCase
	completeUC *application.CompleteBookingUseCase
	deleteUC   *application.DeleteBookingUseCase
	getUC      *application.GetBookingUseCase
	listUC     *application.ListBookingsUseCase
	authSrv    auth.AuthorizationService
}

func NewAPIBookingHandler(
	createUC *application.CreateBookingUseCase,
	confirmUC *application.ConfirmBookingUseCase,
	cancelUC *application.CancelBookingUseCase,
	updateUC *application.UpdateBookingUseCase,
	completeUC *application.CompleteBookingUseCase,
	deleteUC *application.DeleteBookingUseCase,
	getUC *application.GetBookingUseCase,
	listUC *application.ListBookingsUseCase,
	authSrv auth.AuthorizationService,
) *APIBookingHandler {
	return &APIBookingHandler{
		createUC:   createUC,
		confirmUC:  confirmUC,
		cancelUC:   cancelUC,
		updateUC:   updateUC,
		completeUC: completeUC,
		deleteUC:   deleteUC,
		getUC:      getUC,
		listUC:     listUC,
		authSrv:    authSrv,
	}
}

func (h *APIBookingHandler) Register(r chi.Router) {
	r.Route("/api/v1/bookings", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "create")).Post("/", h.Create)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "read")).Get("/", h.List)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "read")).Get("/{id}", h.Get)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "update")).Put("/{id}", h.Update)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "approve")).Post("/{id}/confirm", h.Confirm)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "cancel")).Post("/{id}/cancel", h.Cancel)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "update")).Post("/{id}/complete", h.Complete)
		r.With(middleware.RequirePermission(h.authSrv, "bookings", "delete")).Delete("/{id}", h.Delete)
	})
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *APIBookingHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CustomerID     string   `json:"customer_id"`
		RouteID        string   `json:"route_id"`
		PickupDate     string   `json:"pickup_date"`
		VehicleType    string   `json:"vehicle_type"`
		Passengers     int64    `json:"passengers"`
		CargoWeight    *float64 `json:"cargo_weight"`
		Price          float64  `json:"price"`
		Notes          string   `json:"notes"`
		IdempotencyKey string   `json:"idempotency_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}

	cmd := application.CreateBookingCommand{
		TenantID:       shared.TenantIDFromContext(r.Context()),
		CustomerID:     req.CustomerID,
		RouteID:        req.RouteID,
		PickupDate:     req.PickupDate,
		VehicleType:    req.VehicleType,
		Passengers:     req.Passengers,
		CargoWeight:    req.CargoWeight,
		Price:          req.Price,
		Notes:          req.Notes,
		IdempotencyKey: req.IdempotencyKey,
	}

	id, err := h.createUC.Execute(r.Context(), cmd)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": string(id)})
}

func (h *APIBookingHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}
	search := r.URL.Query().Get("search")
	status := r.URL.Query().Get("status")

	res, err := h.listUC.Execute(r.Context(), application.ListBookingsQuery{
		TenantID: shared.TenantIDFromContext(r.Context()),
		Page:     page,
		Limit:    limit,
		Search:   search,
		Status:   status,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to list bookings")
		return
	}

	dtos := make([]dto.BookingDTO, len(res.Bookings))
	for i, b := range res.Bookings {
		dtos[i] = dto.BookingDTO{
			ID:            b.ID,
			BookingNumber: b.BookingNumber,
			CustomerID:    b.CustomerID,
			RouteID:       b.RouteID,
			PickupDate:    b.PickupDate,
			VehicleType:   b.VehicleType,
			Passengers:    b.Passengers,
			CargoWeight:   b.CargoWeight,
			Price:         b.Price,
			Notes:         b.Notes,
			Status:        b.Status,
			CreatedAt:     b.CreatedAt,
			UpdatedAt:     b.UpdatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"bookings": dtos,
		"total":    res.Total,
	})
}

func (h *APIBookingHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := h.getUC.Execute(r.Context(), application.GetBookingQuery{
		BookingID: aggregate.BookingID(id),
		TenantID:  shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "Booking not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dto.BookingDTO{
		ID:            res.ID,
		BookingNumber: res.BookingNumber,
		CustomerID:    res.CustomerID,
		RouteID:       res.RouteID,
		PickupDate:    res.PickupDate,
		VehicleType:   res.VehicleType,
		Passengers:    res.Passengers,
		CargoWeight:   res.CargoWeight,
		Price:         res.Price,
		Notes:         res.Notes,
		Status:        res.Status,
		CreatedAt:     res.CreatedAt,
		UpdatedAt:     res.UpdatedAt,
	})
}

func (h *APIBookingHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.confirmUC.Execute(r.Context(), application.ConfirmBookingCommand{
		BookingID: aggregate.BookingID(id),
		TenantID:  shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "confirmed"})
}

func (h *APIBookingHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.cancelUC.Execute(r.Context(), application.CancelBookingCommand{
		BookingID: aggregate.BookingID(id),
		TenantID:  shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

func (h *APIBookingHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CustomerID  string   `json:"customer_id"`
		RouteID     string   `json:"route_id"`
		PickupDate  string   `json:"pickup_date"`
		VehicleType string   `json:"vehicle_type"`
		Passengers  int64    `json:"passengers"`
		CargoWeight *float64 `json:"cargo_weight"`
		Price       float64  `json:"price"`
		Notes       string   `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	cmd := application.UpdateBookingCommand{
		BookingID:   aggregate.BookingID(chi.URLParam(r, "id")),
		TenantID:    shared.TenantIDFromContext(r.Context()),
		CustomerID:  req.CustomerID,
		RouteID:     req.RouteID,
		PickupDate:  req.PickupDate,
		VehicleType: req.VehicleType,
		Passengers:  req.Passengers,
		CargoWeight: req.CargoWeight,
		Price:       req.Price,
		Notes:       req.Notes,
	}

	if err := h.updateUC.Execute(r.Context(), cmd); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

func (h *APIBookingHandler) Complete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.completeUC.Execute(r.Context(), application.CompleteBookingCommand{
		BookingID: aggregate.BookingID(id),
		TenantID:  shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "completed"})
}

func (h *APIBookingHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	err := h.deleteUC.Execute(r.Context(), application.DeleteBookingCommand{
		BookingID: aggregate.BookingID(id),
		TenantID:  shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
