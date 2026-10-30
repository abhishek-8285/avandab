package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/rfq"
	"transport-app/internal/shared"
)

type RFQHandlers struct {
	*App
}

func (h *RFQHandlers) RegisterAPIRoutes(r chi.Router) {
	// RFQ Lane Bidding & Procurement API
	r.Route("/api/v1/rfq", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "read")).Get("/", h.APIListRFQs)
		r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/", h.APICreateRFQ)

		r.Route("/{id}", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "read")).Get("/", h.APIGetRFQ)
			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/close", h.APICloseRFQ)
			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/cancel", h.APICancelRFQ)

			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "read")).Get("/bids", h.APIListBids)
			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/bids", h.APISubmitBid)
			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/award/{bidId}", h.APIAwardRFQ)

			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/bids/{bidId}/counter", h.APISubmitCounterOffer)
			r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "read")).Get("/bids/{bidId}/counters", h.APIListCounterOffers)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/counters/{counterId}/accept", h.APIAcceptCounterOffer)
		r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/counters/{counterId}/reject", h.APIRejectCounterOffer)
	})

	// Load Board Counter-Offer Negotiation routes (reuses load board permissions)
	r.Route("/api/v1/loadboard", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/listings/{id}/bids/{bidId}/counter", h.APISubmitLoadBoardCounter)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "read")).Get("/listings/{id}/bids/{bidId}/counters", h.APIListLoadBoardCounters)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/counters/{counterId}/accept", h.APIAcceptCounterOffer)
		r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/counters/{counterId}/reject", h.APIRejectCounterOffer)
	})
}

func writeRFQJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRFQError(w http.ResponseWriter, code int, msg string) {
	writeRFQJSON(w, code, map[string]string{"error": msg})
}

func (h *RFQHandlers) service() *rfq.Service {
	return rfq.NewService(rfq.NewRepository(h.DB))
}

func getRFQUser(r *http.Request, defaultVal string) string {
	if sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil && sess.UserID != "" {
		return sess.UserID
	}
	return defaultVal
}

func getRFQUserName(r *http.Request, defaultVal string) string {
	if sess, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData); ok && sess != nil && sess.Name != "" {
		return sess.Name
	}
	return defaultVal
}

// RFQ Handlers

func (h *RFQHandlers) APIListRFQs(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	svc := h.service()

	var filter rfq.ListRFQsFilter
	q := r.URL.Query()

	if status := q.Get("status"); status != "" {
		st := rfq.RFQStatus(strings.ToUpper(status))
		filter.Status = &st
	}
	if origin := q.Get("origin_city"); origin != "" {
		filter.OriginCity = origin
	}
	if dest := q.Get("destination_city"); dest != "" {
		filter.DestinationCity = dest
	}
	if vtype := q.Get("vehicle_type"); vtype != "" {
		filter.VehicleType = vtype
	}
	if search := q.Get("search"); search != "" {
		filter.Search = search
	}
	if limit := q.Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if offset := q.Get("offset"); offset != "" {
		if o, err := strconv.Atoi(offset); err == nil {
			filter.Offset = o
		}
	}

	rfqs, total, err := svc.ListRFQs(r.Context(), tenantID, filter)
	if err != nil {
		writeRFQError(w, http.StatusInternalServerError, "failed to query rfqs: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]interface{}{
		"rfqs":  rfqs,
		"total": total,
	})
}

func (h *RFQHandlers) APICreateRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	userID := getRFQUser(r, "system")

	var in rfq.CreateRFQInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeRFQError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.service()
	item, err := svc.CreateRFQ(r.Context(), tenantID, userID, in)
	if err != nil {
		if errors.Is(err, rfq.ErrInvalidInput) {
			writeRFQError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to create rfq: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusCreated, item)
}

func (h *RFQHandlers) APIGetRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	item, err := svc.GetRFQ(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "rfq not found")
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to fetch rfq: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, item)
}

func (h *RFQHandlers) APICloseRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	if err := svc.CloseRFQ(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "rfq not found")
			return
		}
		if errors.Is(err, rfq.ErrInvalidInput) {
			writeRFQError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to close rfq: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *RFQHandlers) APICancelRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	if err := svc.CancelRFQ(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "rfq not found")
			return
		}
		if errors.Is(err, rfq.ErrInvalidInput) {
			writeRFQError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to cancel rfq: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// RFQ Bids Handlers

func (h *RFQHandlers) APIListBids(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	svc := h.service()
	bids, err := svc.ListBids(r.Context(), tenantID, id)
	if err != nil {
		writeRFQError(w, http.StatusInternalServerError, "failed to query bids: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]interface{}{
		"bids": bids,
	})
}

func (h *RFQHandlers) APISubmitBid(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in rfq.SubmitRFQBidInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeRFQError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	svc := h.service()
	bid, err := svc.SubmitBid(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, rfq.ErrInvalidInput) {
			writeRFQError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, rfq.ErrRFQClosed) {
			writeRFQError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "rfq not found")
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to submit bid: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusCreated, bid)
}

func (h *RFQHandlers) APIAwardRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	rfqID := chi.URLParam(r, "id")
	bidID := chi.URLParam(r, "bidId")
	userID := getRFQUser(r, "system")

	svc := h.service()
	if err := svc.AwardRFQ(r.Context(), tenantID, rfqID, bidID, userID); err != nil {
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "rfq or bid not found")
			return
		}
		if errors.Is(err, rfq.ErrRFQClosed) || errors.Is(err, rfq.ErrBidDecided) {
			writeRFQError(w, http.StatusConflict, err.Error())
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to award rfq: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// Counter-Offers Handlers

func (h *RFQHandlers) APISubmitCounterOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	rfqID := chi.URLParam(r, "id")
	bidID := chi.URLParam(r, "bidId")
	userID := getRFQUser(r, "system")
	userName := getRFQUserName(r, "User")

	var in rfq.SubmitCounterOfferInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeRFQError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	in.EntityType = rfq.EntityRFQ
	in.RFQID = &rfqID

	svc := h.service()
	co, err := svc.SubmitCounterOffer(r.Context(), tenantID, bidID, userID, userName, in)
	if err != nil {
		if errors.Is(err, rfq.ErrInvalidInput) {
			writeRFQError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, rfq.ErrRFQClosed) || errors.Is(err, rfq.ErrBidDecided) {
			writeRFQError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "rfq or bid not found")
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to submit counter offer: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusCreated, co)
}

func (h *RFQHandlers) APISubmitLoadBoardCounter(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	listingID := chi.URLParam(r, "id")
	bidID := chi.URLParam(r, "bidId")
	userID := getRFQUser(r, "system")
	userName := getRFQUserName(r, "User")

	var in rfq.SubmitCounterOfferInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeRFQError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	in.EntityType = rfq.EntityLoadBoard
	in.ListingID = &listingID

	svc := h.service()
	co, err := svc.SubmitCounterOffer(r.Context(), tenantID, bidID, userID, userName, in)
	if err != nil {
		if errors.Is(err, rfq.ErrInvalidInput) {
			writeRFQError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "listing or bid not found")
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to submit load board counter offer: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusCreated, co)
}

func (h *RFQHandlers) APIListCounterOffers(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	bidID := chi.URLParam(r, "bidId")

	svc := h.service()
	counters, err := svc.ListCounterOffers(r.Context(), tenantID, bidID)
	if err != nil {
		writeRFQError(w, http.StatusInternalServerError, "failed to query counter offers: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]interface{}{
		"counter_offers": counters,
	})
}

func (h *RFQHandlers) APIListLoadBoardCounters(w http.ResponseWriter, r *http.Request) {
	h.APIListCounterOffers(w, r)
}

func (h *RFQHandlers) APIAcceptCounterOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	counterID := chi.URLParam(r, "counterId")
	userID := getRFQUser(r, "system")

	svc := h.service()
	if err := svc.AcceptCounterOffer(r.Context(), tenantID, counterID, userID); err != nil {
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "counter offer not found")
			return
		}
		if errors.Is(err, rfq.ErrCounterNotPending) || errors.Is(err, rfq.ErrCounterExpired) {
			writeRFQError(w, http.StatusConflict, err.Error())
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to accept counter offer: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (h *RFQHandlers) APIRejectCounterOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	counterID := chi.URLParam(r, "counterId")
	userID := getRFQUser(r, "system")

	svc := h.service()
	if err := svc.RejectCounterOffer(r.Context(), tenantID, counterID, userID); err != nil {
		if errors.Is(err, rfq.ErrNotFound) {
			writeRFQError(w, http.StatusNotFound, "counter offer not found")
			return
		}
		if errors.Is(err, rfq.ErrCounterNotPending) {
			writeRFQError(w, http.StatusConflict, err.Error())
			return
		}
		writeRFQError(w, http.StatusInternalServerError, "failed to reject counter offer: "+err.Error())
		return
	}

	writeRFQJSON(w, http.StatusOK, map[string]bool{"success": true})
}
