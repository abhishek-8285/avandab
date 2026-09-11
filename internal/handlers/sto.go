package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/sto"
)

// STOHandlers exposes HTTP APIs for STO management and Load Board bidding.
type STOHandlers struct {
	*App
	STOSvc *sto.Service
}

// RegisterAPIRoutes registers /api/v1/sto/* and /api/v1/loadboard/* routes.
func (h *STOHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/sto", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "sto", "read")).Get("/", h.APIListSTOs)
		r.With(middleware.RequirePermission(h.AuthSrv, "sto", "write")).Post("/", h.APICreateSTO)
		r.With(middleware.RequirePermission(h.AuthSrv, "sto", "read")).Get("/{id}", h.APIGetSTO)
		r.With(middleware.RequirePermission(h.AuthSrv, "sto", "write")).Post("/{id}/release", h.APIReleaseSTO)
		r.With(middleware.RequirePermission(h.AuthSrv, "sto", "write")).Post("/{id}/post-to-loadboard", h.APIPostToLoadBoard)
	})

	r.Route("/api/v1/loadboard", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "read")).Get("/listings", h.APIListListings)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "read")).Get("/listings/{id}", h.APIGetListing)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "read")).Get("/listings/{id}/bids", h.APIListBids)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/listings/{id}/bids", h.APISubmitBid)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/listings/{id}/award", h.APIAwardBid)
	})
}

func writeSTOJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSTOError(w http.ResponseWriter, code int, msg string) {
	writeSTOJSON(w, code, map[string]string{"error": msg})
}

func (h *STOHandlers) getService() *sto.Service {
	if h.STOSvc != nil {
		return h.STOSvc
	}
	repo := sto.NewSQLRepository(h.DB)
	return sto.NewService(repo, h.DB)
}

// APICreateSTO handles creation of a new STO draft.
func (h *STOHandlers) APICreateSTO(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	var in sto.CreateSTOInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeSTOError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if in.CreatedBy == "" {
		if user, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && user != nil {
			in.CreatedBy = user.UserID
		} else {
			in.CreatedBy = "system"
		}
	}

	svc := h.getService()
	record, err := svc.CreateSTO(r.Context(), tenantID, in)
	if err != nil {
		writeSTOError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeSTOJSON(w, http.StatusCreated, record)
}

// APIGetSTO handles fetching an STO by ID.
func (h *STOHandlers) APIGetSTO(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	id := chi.URLParam(r, "id")
	svc := h.getService()
	record, err := svc.GetSTO(r.Context(), tenantID, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeSTOError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSTOError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSTOJSON(w, http.StatusOK, record)
}

// APIListSTOs lists STOs with filters and pagination.
func (h *STOHandlers) APIListSTOs(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	status := r.URL.Query().Get("status")
	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	svc := h.getService()
	records, total, err := svc.ListSTOs(r.Context(), tenantID, status, limit, offset)
	if err != nil {
		writeSTOError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSTOJSON(w, http.StatusOK, map[string]interface{}{
		"items":  records,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// APIReleaseSTO releases a DRAFT STO.
func (h *STOHandlers) APIReleaseSTO(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	id := chi.URLParam(r, "id")
	svc := h.getService()
	record, err := svc.ReleaseSTO(r.Context(), tenantID, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeSTOError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSTOError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSTOJSON(w, http.StatusOK, record)
}

// APIPostToLoadBoard posts a RELEASED STO to the load board.
func (h *STOHandlers) APIPostToLoadBoard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	id := chi.URLParam(r, "id")

	var in sto.PostToLoadBoardInput
	// Optional body
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&in)
	}

	svc := h.getService()
	listing, err := svc.PostToLoadBoard(r.Context(), tenantID, id, in)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeSTOError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSTOError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSTOJSON(w, http.StatusCreated, listing)
}

// APIListListings returns load board listings.
func (h *STOHandlers) APIListListings(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	status := r.URL.Query().Get("status")
	originCity := r.URL.Query().Get("origin_city")
	destCity := r.URL.Query().Get("destination_city")

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	svc := h.getService()
	listings, total, err := svc.ListListings(r.Context(), tenantID, status, originCity, destCity, limit, offset)
	if err != nil {
		writeSTOError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeSTOJSON(w, http.StatusOK, map[string]interface{}{
		"items":  listings,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// APIGetListing gets a specific load board listing.
func (h *STOHandlers) APIGetListing(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	id := chi.URLParam(r, "id")
	svc := h.getService()
	listing, err := svc.GetListing(r.Context(), tenantID, id)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeSTOError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSTOError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSTOJSON(w, http.StatusOK, listing)
}

// APISubmitBid submits a carrier bid for a listing.
func (h *STOHandlers) APISubmitBid(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	listingID := chi.URLParam(r, "id")

	var in sto.SubmitBidInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeSTOError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.getService()
	bid, err := svc.SubmitBid(r.Context(), tenantID, listingID, in)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeSTOError(w, http.StatusNotFound, err.Error())
			return
		}
		writeSTOError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeSTOJSON(w, http.StatusCreated, bid)
}

// APIListBids returns bids submitted on a listing.
func (h *STOHandlers) APIListBids(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	listingID := chi.URLParam(r, "id")

	svc := h.getService()
	bids, err := svc.ListBids(r.Context(), tenantID, listingID)
	if err != nil {
		writeSTOError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeSTOJSON(w, http.StatusOK, bids)
}

// APIAwardBid awards a winning bid.
func (h *STOHandlers) APIAwardBid(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeSTOError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}
	listingID := chi.URLParam(r, "id")

	var in sto.AwardBidInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeSTOError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if strings.TrimSpace(in.BidID) == "" {
		writeSTOError(w, http.StatusBadRequest, "bid_id is required")
		return
	}

	svc := h.getService()
	result, err := svc.AwardBid(r.Context(), tenantID, listingID, in.BidID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			writeSTOError(w, http.StatusNotFound, errMsg)
			return
		}
		if strings.Contains(errMsg, "conflict") || strings.Contains(errMsg, "cannot be awarded") {
			writeSTOError(w, http.StatusConflict, errMsg)
			return
		}
		writeSTOError(w, http.StatusBadRequest, errMsg)
		return
	}
	writeSTOJSON(w, http.StatusOK, result)
}
