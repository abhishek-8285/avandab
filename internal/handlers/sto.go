package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"errors"
	"time"
	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/rfq"
	"transport-app/internal/shared"
	"transport-app/internal/sto"
)

// STOHandlers exposes HTTP APIs for STO management and Load Board bidding.
type STOHandlers struct {
	*App
	STOSvc *sto.Service
}

// Routes registers web UI routes for Stock Transfer Orders under /sto.
func (h *STOHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "sto", "read")).Get("/", h.WebListSTOs)
	r.With(middleware.RequirePermission(h.AuthSrv, "sto", "write")).Post("/", h.WebCreateSTO)
	r.With(middleware.RequirePermission(h.AuthSrv, "sto", "write")).Post("/{id}/release", h.WebReleaseSTO)
	r.With(middleware.RequirePermission(h.AuthSrv, "sto", "write")).Post("/{id}/post-to-loadboard", h.WebPostToLoadBoard)
}

// LoadBoardRoutes registers web UI routes for the Load Board under /loadboard.
func (h *STOHandlers) LoadBoardRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "read")).Get("/", h.WebListLoadboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/listings/{id}/bids", h.WebSubmitBid)
}

// WebListSTOs renders the Stock Transfer Orders page.
func (h *STOHandlers) WebListSTOs(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)
	svc := h.getService()
	statusStr := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))

	list, total, err := svc.ListSTOs(r.Context(), tenantID, statusStr, 100, 0)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load STOs", err.Error(), user)
		return
	}
	data := PageData{
		Title: "Stock Transfers",
		User:  user,
		Extra: map[string]interface{}{
			"STOs":         list,
			"Total":        total,
			"StatusFilter": statusStr,
		},
	}
	h.renderPage(w, r, "sto_list.html", data)
}

// WebCreateSTO handles form submissions for creating new STOs.
func (h *STOHandlers) WebCreateSTO(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	svc := h.getService()

	qty, _ := strconv.ParseFloat(r.PostFormValue("quantity"), 64)
	user, _ := h.getUserFromContext(r)
	createdBy := "system"
	if user != nil && user.Name != "" {
		createdBy = user.Name
	}

	in := sto.CreateSTOInput{
		OriginFacilityID:      strings.TrimSpace(r.PostFormValue("origin_facility_id")),
		DestinationFacilityID: strings.TrimSpace(r.PostFormValue("destination_facility_id")),
		MaterialCode:          strings.TrimSpace(r.PostFormValue("material_code")),
		MaterialDescription:   strings.TrimSpace(r.PostFormValue("material_description")),
		Quantity:              qty,
		UOM:                   strings.ToUpper(strings.TrimSpace(r.PostFormValue("uom"))),
		RequiredDeliveryDate:  strings.TrimSpace(r.PostFormValue("required_delivery_date")),
		Notes:                 strings.TrimSpace(r.PostFormValue("notes")),
		CreatedBy:             createdBy,
	}

	if _, err := svc.CreateSTO(r.Context(), tenantID, in); err != nil {
		http.Redirect(w, r, "/sto?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/sto", http.StatusSeeOther)
}

// WebReleaseSTO releases an STO for transport.
func (h *STOHandlers) WebReleaseSTO(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	svc := h.getService()
	_, _ = svc.ReleaseSTO(r.Context(), tenantID, id)
	http.Redirect(w, r, "/sto", http.StatusSeeOther)
}

// WebPostToLoadBoard publishes an STO to the open marketplace.
func (h *STOHandlers) WebPostToLoadBoard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	svc := h.getService()
	in := sto.PostToLoadBoardInput{
		VehicleTypeRequired: "truck",
		TargetRate:          15000,
		MaxRate:             20000,
		Visibility:          sto.VisibilityPRIVATE,
		ExpiresHours:        48,
	}
	_, _ = svc.PostToLoadBoard(r.Context(), tenantID, id, in)
	http.Redirect(w, r, "/loadboard", http.StatusSeeOther)
}

// WebListLoadboard renders the Load Board listings page.
func (h *STOHandlers) WebListLoadboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)
	svc := h.getService()
	statusStr := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))

	listings, _, err := svc.ListListings(r.Context(), tenantID, statusStr, "", "", 100, 0)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load listings", err.Error(), user)
		return
	}
	data := PageData{
		Title: "Load Board",
		User:  user,
		Extra: map[string]interface{}{
			"Listings":     listings,
			"StatusFilter": statusStr,
		},
	}
	h.renderPage(w, r, "loadboard_list.html", data)
}

// WebSubmitBid submits a carrier bid on a load board listing.
func (h *STOHandlers) WebSubmitBid(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	listingID := chi.URLParam(r, "id")
	svc := h.getService()

	bidAmt, _ := strconv.ParseFloat(r.PostFormValue("bid_amount"), 64)
	carrierName := strings.TrimSpace(r.PostFormValue("carrier_name"))
	carrierID := strings.TrimSpace(r.PostFormValue("carrier_id"))
	if carrierID == "" {
		carrierID = "carrier-ext"
	}
	in := sto.SubmitBidInput{
		CarrierID:   carrierID,
		CarrierName: carrierName,
		BidAmount:   bidAmt,
		Remarks:     strings.TrimSpace(r.PostFormValue("remarks")),
	}
	_, _ = svc.SubmitBid(r.Context(), tenantID, listingID, in)
	http.Redirect(w, r, "/loadboard", http.StatusSeeOther)
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

// ---------------------------------------------------------------------------
// Merged from rfq.go
// ---------------------------------------------------------------------------

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
	r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/api/v1/loadboard/listings/{id}/bids/{bidId}/counter", h.APISubmitLoadBoardCounter)
	r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "read")).Get("/api/v1/loadboard/listings/{id}/bids/{bidId}/counters", h.APIListLoadBoardCounters)
	r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/api/v1/loadboard/counters/{counterId}/accept", h.APIAcceptCounterOffer)
	r.With(middleware.RequirePermission(h.AuthSrv, "loadboard", "write")).Post("/api/v1/loadboard/counters/{counterId}/reject", h.APIRejectCounterOffer)
}

// Routes mounts the RFQ Lane Bidding web UI handlers at /rfq.
func (h *RFQHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "read")).Get("/", h.WebRFQDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/", h.WebCreateRFQ)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/{id}/close", h.WebCloseRFQ)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/{id}/cancel", h.WebCancelRFQ)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/{id}/bids", h.WebSubmitBid)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/{id}/award/{bidId}", h.WebAwardRFQ)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/{id}/bids/{bidId}/counter", h.WebSubmitCounterOffer)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/counters/{counterId}/accept", h.WebAcceptCounterOffer)
	r.With(middleware.RequirePermission(h.AuthSrv, "rfq", "write")).Post("/counters/{counterId}/reject", h.WebRejectCounterOffer)
}

// WebRFQDashboard renders active lane procurement contracts, bidding board, and rate negotiations.
func (h *RFQHandlers) WebRFQDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	statusParam := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status")))
	var statusFilter *rfq.RFQStatus
	if statusParam != "" {
		st := rfq.RFQStatus(statusParam)
		statusFilter = &st
	}

	search := r.URL.Query().Get("search")
	rfqs, _, _ := h.service().ListRFQs(r.Context(), tenantID, rfq.ListRFQsFilter{
		Status: statusFilter,
		Search: search,
		Limit:  50,
	})

	// Rollup counts
	var openCount, awardedCount, totalBids int
	for _, q := range rfqs {
		if q.Status == rfq.StatusOpen || q.Status == rfq.StatusUnderNegotiation {
			openCount++
		} else if q.Status == rfq.StatusAwarded {
			awardedCount++
		}
		totalBids += q.BidsCount
	}

	selectedRFQID := r.URL.Query().Get("rfq_id")
	var selectedRFQ *rfq.RFQ
	var selectedBids []rfq.RFQBid
	if len(rfqs) > 0 {
		if selectedRFQID == "" {
			selectedRFQID = rfqs[0].ID
		}
		for i := range rfqs {
			if rfqs[i].ID == selectedRFQID {
				selectedRFQ = &rfqs[i]
				break
			}
		}
		if selectedRFQ != nil {
			selectedBids, _ = h.service().ListBids(r.Context(), tenantID, selectedRFQ.ID)
		}
	}

	data := PageData{
		Title: "RFQ Lane Bidding",
		User:  user,
		Extra: map[string]interface{}{
			"RFQs":         rfqs,
			"SelectedRFQ":  selectedRFQ,
			"SelectedBids": selectedBids,
			"OpenCount":    openCount,
			"AwardedCount": awardedCount,
			"TotalBids":    totalBids,
			"StatusParam":  statusParam,
			"Search":       search,
		},
	}

	h.renderPage(w, r, "rfq_dashboard.html", data)
}

// WebCreateRFQ creates a new spot/contract lane bidding tender.
func (h *RFQHandlers) WebCreateRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	origin := strings.TrimSpace(r.FormValue("origin_city"))
	dest := strings.TrimSpace(r.FormValue("destination_city"))
	vType := strings.TrimSpace(r.FormValue("vehicle_type_required"))

	targetRate, _ := strconv.ParseFloat(r.FormValue("target_rate"), 64)
	maxRate, _ := strconv.ParseFloat(r.FormValue("max_acceptable_rate"), 64)
	if maxRate < targetRate {
		maxRate = targetRate * 1.15
	}
	cargoWeight, _ := strconv.ParseFloat(r.FormValue("cargo_weight_tons"), 64)

	now := time.Now()
	pickup := now.Add(48 * time.Hour)
	delivery := now.Add(96 * time.Hour)
	bidDeadline := now.Add(24 * time.Hour)

	userID := "shipper"
	if user != nil && user.UserID != "" {
		userID = user.UserID
	}

	in := rfq.CreateRFQInput{
		Title:               title,
		OriginCity:          origin,
		DestinationCity:     dest,
		VehicleTypeRequired: vType,
		CargoType:           strings.TrimSpace(r.FormValue("cargo_type")),
		CargoWeightTons:     cargoWeight,
		TargetRate:          targetRate,
		MaxAcceptableRate:   maxRate,
		PickupDate:          pickup,
		DeliveryDeadline:    delivery,
		BidDeadline:         bidDeadline,
	}

	if _, err := h.service().CreateRFQ(r.Context(), tenantID, userID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to publish RFQ", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq", http.StatusSeeOther)
}

// WebCloseRFQ manually closes bidding on an RFQ.
func (h *RFQHandlers) WebCloseRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := h.service().CloseRFQ(r.Context(), tenantID, id); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to close RFQ", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq", http.StatusSeeOther)
}

// WebCancelRFQ cancels an RFQ tender.
func (h *RFQHandlers) WebCancelRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := h.service().CancelRFQ(r.Context(), tenantID, id); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to cancel RFQ", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq", http.StatusSeeOther)
}

// WebSubmitBid submits a carrier rate bid on an open RFQ.
func (h *RFQHandlers) WebSubmitBid(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	bidAmount, _ := strconv.ParseFloat(r.FormValue("bid_amount"), 64)
	transitDays, _ := strconv.Atoi(r.FormValue("transit_days"))
	if transitDays <= 0 {
		transitDays = 2
	}
	vehs, _ := strconv.Atoi(r.FormValue("vehicles_available"))
	if vehs <= 0 {
		vehs = 1
	}

	carrierName := strings.TrimSpace(r.FormValue("carrier_name"))
	if carrierName == "" {
		carrierName = "Partner Carrier"
	}

	carrierID := "carrier-01"
	if user != nil && user.UserID != "" {
		carrierID = user.UserID
	}

	in := rfq.SubmitRFQBidInput{
		CarrierID:         carrierID,
		CarrierName:       carrierName,
		BidAmount:         bidAmount,
		TransitDays:       transitDays,
		VehiclesAvailable: vehs,
		Remarks:           strings.TrimSpace(r.FormValue("remarks")),
	}

	if _, err := h.service().SubmitBid(r.Context(), tenantID, id, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to submit bid", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq?rfq_id="+id, http.StatusSeeOther)
}

// WebAwardRFQ accepts a carrier bid and awards the contract.
func (h *RFQHandlers) WebAwardRFQ(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	bidID := chi.URLParam(r, "bidId")
	user, _ := h.getUserFromContext(r)

	userID := "shipper"
	if user != nil && user.UserID != "" {
		userID = user.UserID
	}

	if err := h.service().AwardRFQ(r.Context(), tenantID, id, bidID, userID); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to award RFQ", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq?rfq_id="+id, http.StatusSeeOther)
}

// WebSubmitCounterOffer submits a negotiated rate counter.
func (h *RFQHandlers) WebSubmitCounterOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")
	bidID := chi.URLParam(r, "bidId")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	counterRate, _ := strconv.ParseFloat(r.FormValue("counter_rate"), 64)
	userID := "shipper"
	userName := "Fleet Dispatcher"
	if user != nil {
		if user.UserID != "" {
			userID = user.UserID
		}
		if user.Name != "" {
			userName = user.Name
		}
	}

	in := rfq.SubmitCounterOfferInput{
		EntityType:    rfq.EntityRFQ,
		RFQID:         &id,
		SenderRole:    rfq.RoleShipper,
		OfferedAmount: counterRate,
		Remarks:       strings.TrimSpace(r.FormValue("remarks")),
	}

	if _, err := h.service().SubmitCounterOffer(r.Context(), tenantID, bidID, userID, userName, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to submit counter offer", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq?rfq_id="+id, http.StatusSeeOther)
}

// WebAcceptCounterOffer accepts a negotiated counter offer.
func (h *RFQHandlers) WebAcceptCounterOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	counterID := chi.URLParam(r, "counterId")
	user, _ := h.getUserFromContext(r)

	userID := "shipper"
	if user != nil && user.UserID != "" {
		userID = user.UserID
	}

	if err := h.service().AcceptCounterOffer(r.Context(), tenantID, counterID, userID); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to accept counter offer", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq", http.StatusSeeOther)
}

// WebRejectCounterOffer rejects a negotiated counter offer.
func (h *RFQHandlers) WebRejectCounterOffer(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	counterID := chi.URLParam(r, "counterId")
	user, _ := h.getUserFromContext(r)

	userID := "shipper"
	if user != nil && user.UserID != "" {
		userID = user.UserID
	}

	if err := h.service().RejectCounterOffer(r.Context(), tenantID, counterID, userID); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to reject counter offer", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/rfq", http.StatusSeeOther)
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
