package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/customer/application"
	"transport-app/internal/httpx"
	"transport-app/internal/shared"
)

type CustomerHandler struct {
	svc *application.CustomerAppService
}

func NewCustomerHandler(svc *application.CustomerAppService) *CustomerHandler {
	return &CustomerHandler{svc: svc}
}

func (h *CustomerHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/customer", func(r chi.Router) {
		r.Post("/quotes", h.CreateQuote)
		r.Post("/bookings", h.CreateBooking)
		r.Get("/bookings", h.ListBookings)
		r.Get("/bookings/{id}/tracking", h.GetBookingTracking)
		r.Post("/bookings/{id}/cancel", h.CancelBooking)
	})
}

func (h *CustomerHandler) getTenantAndCustomer(r *http.Request) (string, string, bool) {
	// Fail closed: no silent DefaultTenant fallback. A request without a
	// resolved tenant must never read or write tenant "1" data.
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		return "", "", false
	}

	session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || session == nil || session.UserID == "" {
		return tenantID, "", false
	}

	return tenantID, session.UserID, true
}

func (h *CustomerHandler) CreateQuote(w http.ResponseWriter, r *http.Request) {
	tenantID, customerID, ok := h.getTenantAndCustomer(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var req application.CreateQuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	quote, err := h.svc.CreateQuote(r.Context(), tenantID, customerID, req)
	if err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusCreated, quote)
}

func (h *CustomerHandler) CreateBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, customerID, ok := h.getTenantAndCustomer(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var req application.CreateBookingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	// Honor Idempotency-Key HTTP header if present
	idempHeader := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempHeader != "" && req.IdempotencyKey == "" {
		req.IdempotencyKey = idempHeader
	}

	resp, err := h.svc.CreateBooking(r.Context(), tenantID, customerID, req)
	if err != nil {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	if resp.IsIdempotent {
		httpx.JSON(w, http.StatusOK, resp)
	} else {
		httpx.JSON(w, http.StatusCreated, resp)
	}
}

func (h *CustomerHandler) GetBookingTracking(w http.ResponseWriter, r *http.Request) {
	tenantID, customerID, ok := h.getTenantAndCustomer(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	bookingID := chi.URLParam(r, "id")
	if bookingID == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "booking id is required"})
		return
	}

	tracking, err := h.svc.GetBookingTracking(r.Context(), tenantID, customerID, bookingID)
	if err != nil {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, tracking)
}

func (h *CustomerHandler) ListBookings(w http.ResponseWriter, r *http.Request) {
	tenantID, customerID, ok := h.getTenantAndCustomer(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if val, err := strconv.Atoi(o); err == nil && val >= 0 {
			offset = val
		}
	}

	list, err := h.svc.ListBookings(r.Context(), tenantID, customerID, limit, offset)
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]interface{}{"bookings": list})
}

func (h *CustomerHandler) CancelBooking(w http.ResponseWriter, r *http.Request) {
	tenantID, customerID, ok := h.getTenantAndCustomer(r)
	if !ok {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	bookingID := chi.URLParam(r, "id")
	if bookingID == "" {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "booking id is required"})
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Reason == "" {
		body.Reason = "Customer cancelled booking"
	}

	err := h.svc.CancelBooking(r.Context(), tenantID, customerID, bookingID, body.Reason)
	if err != nil {
		httpx.JSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	httpx.JSON(w, http.StatusOK, map[string]string{"status": "cancelled", "message": "Booking cancelled successfully"})
}
