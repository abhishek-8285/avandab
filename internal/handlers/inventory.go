package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/inventory"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

type InventoryHandlers struct {
	*App
}

func (h *InventoryHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/inventory", func(r chi.Router) {
		r.Route("/parts", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/", h.APIListParts)
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/", h.APICreatePart)
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/{id}", h.APIGetPart)
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Put("/{id}", h.APIUpdatePart)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/stocks", h.APIListStocks)
		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/stocks/adjust", h.APIAdjustStock)
		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/issue", h.APIIssuePart)
		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/return", h.APIReturnPart)
		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/transfer", h.APITransferStock)
		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/transactions", h.APIListTransactions)

		r.Route("/bills", func(r chi.Router) {
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/", h.APIListPurchaseBills)
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/", h.APICreatePurchaseBill)
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/{id}", h.APIGetPurchaseBill)
			r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/{id}/receive", h.APIReceivePurchaseBill)
		})

		r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/summary", h.APIGetSummary)
	})
}

func writeInventoryJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeInventoryError(w http.ResponseWriter, code int, msg string) {
	writeInventoryJSON(w, code, map[string]string{"error": msg})
}

func (h *InventoryHandlers) service() *inventory.Service {
	return inventory.NewService(inventory.NewSQLRepository(h.DB))
}

func (h *InventoryHandlers) APIListParts(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	lowStockOnly := r.URL.Query().Get("low_stock") == "true" || r.URL.Query().Get("low_stock") == "1"
	activeOnly := r.URL.Query().Get("active") != "false"

	filter := inventory.PartFilter{
		TenantID:     tenantID,
		Category:     strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("category"))),
		Search:       r.URL.Query().Get("q"),
		LowStockOnly: lowStockOnly,
		ActiveOnly:   activeOnly,
		Limit:        limit,
		Offset:       offset,
	}

	parts, total, err := h.service().ListParts(r.Context(), filter)
	if err != nil {
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"parts":  parts,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *InventoryHandlers) APICreatePart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in inventory.CreatePartInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	created, err := h.service().CreatePart(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, inventory.ErrDuplicatePartNumber) {
			writeInventoryError(w, http.StatusConflict, err.Error())
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusCreated, map[string]interface{}{
		"part": created,
	})
}

func (h *InventoryHandlers) APIGetPart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	p, err := h.service().GetPart(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, inventory.ErrPartNotFound) {
			writeInventoryError(w, http.StatusNotFound, "part not found")
			return
		}
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"part": p,
	})
}

func (h *InventoryHandlers) APIUpdatePart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in inventory.UpdatePartInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().UpdatePart(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, inventory.ErrPartNotFound) {
			writeInventoryError(w, http.StatusNotFound, "part not found")
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"part": updated,
	})
}

func (h *InventoryHandlers) APIListStocks(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	lowStock := r.URL.Query().Get("low_stock") == "true" || r.URL.Query().Get("low_stock") == "1"

	filter := inventory.StockFilter{
		TenantID:     tenantID,
		FacilityID:   r.URL.Query().Get("facility_id"),
		PartID:       r.URL.Query().Get("part_id"),
		LowStockOnly: lowStock,
		Limit:        limit,
		Offset:       offset,
	}

	stocks, total, err := h.service().ListStocks(r.Context(), filter)
	if err != nil {
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"stocks": stocks,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *InventoryHandlers) APIAdjustStock(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in inventory.AdjustStockInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	tx, err := h.service().AdjustStock(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, inventory.ErrPartNotFound) {
			writeInventoryError(w, http.StatusNotFound, "part not found")
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"transaction": tx,
	})
}

func (h *InventoryHandlers) APIIssuePart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in inventory.IssuePartInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	tx, err := h.service().IssuePart(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, inventory.ErrPartNotFound) {
			writeInventoryError(w, http.StatusNotFound, "part not found")
			return
		}
		if errors.Is(err, inventory.ErrInsufficientStock) {
			writeInventoryError(w, http.StatusConflict, err.Error())
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"transaction": tx,
	})
}

func (h *InventoryHandlers) APIReturnPart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in inventory.ReturnPartInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	tx, err := h.service().ReturnPart(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, inventory.ErrPartNotFound) {
			writeInventoryError(w, http.StatusNotFound, "part not found")
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"transaction": tx,
	})
}

func (h *InventoryHandlers) APITransferStock(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in inventory.TransferStockInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	outTx, inTx, err := h.service().TransferStock(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, inventory.ErrPartNotFound) {
			writeInventoryError(w, http.StatusNotFound, "part not found")
			return
		}
		if errors.Is(err, inventory.ErrInsufficientStock) {
			writeInventoryError(w, http.StatusConflict, err.Error())
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"out_transaction": outTx,
		"in_transaction":  inTx,
	})
}

func (h *InventoryHandlers) APIListTransactions(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := inventory.TransactionFilter{
		TenantID:        tenantID,
		PartID:          r.URL.Query().Get("part_id"),
		FacilityID:      r.URL.Query().Get("facility_id"),
		TransactionType: strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("type"))),
		WorkOrderID:     r.URL.Query().Get("work_order_id"),
		VehicleID:       r.URL.Query().Get("vehicle_id"),
		Limit:           limit,
		Offset:          offset,
	}

	txs, total, err := h.service().ListTransactions(r.Context(), filter)
	if err != nil {
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"transactions": txs,
		"total":        total,
		"limit":        limit,
		"offset":       offset,
	})
}

func (h *InventoryHandlers) APICreatePurchaseBill(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in inventory.CreatePurchaseBillInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeInventoryError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	bill, err := h.service().CreatePurchaseBill(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, inventory.ErrDuplicateBillNumber) {
			writeInventoryError(w, http.StatusConflict, err.Error())
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusCreated, map[string]interface{}{
		"bill": bill,
	})
}

func (h *InventoryHandlers) APIGetPurchaseBill(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	bill, err := h.service().GetPurchaseBill(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, inventory.ErrBillNotFound) {
			writeInventoryError(w, http.StatusNotFound, "purchase bill not found")
			return
		}
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"bill": bill,
	})
}

func (h *InventoryHandlers) APIListPurchaseBills(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := inventory.PurchaseBillFilter{
		TenantID:   tenantID,
		FacilityID: r.URL.Query().Get("facility_id"),
		Status:     strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
		Search:     r.URL.Query().Get("q"),
		Limit:      limit,
		Offset:     offset,
	}

	bills, total, err := h.service().ListPurchaseBills(r.Context(), filter)
	if err != nil {
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"bills":  bills,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *InventoryHandlers) APIReceivePurchaseBill(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in struct {
		ReceivedBy string `json:"received_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	receivedBy := strings.TrimSpace(in.ReceivedBy)
	if receivedBy == "" {
		receivedBy = "Store Keeper"
	}

	bill, err := h.service().ReceivePurchaseBill(r.Context(), tenantID, id, receivedBy)
	if err != nil {
		if errors.Is(err, inventory.ErrBillNotFound) {
			writeInventoryError(w, http.StatusNotFound, "purchase bill not found")
			return
		}
		if errors.Is(err, inventory.ErrBillAlreadyProcessed) {
			writeInventoryError(w, http.StatusConflict, err.Error())
			return
		}
		writeInventoryError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"bill": bill,
	})
}

func (h *InventoryHandlers) APIGetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	summary, err := h.service().GetSummary(r.Context(), tenantID)
	if err != nil {
		writeInventoryError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeInventoryJSON(w, http.StatusOK, map[string]interface{}{
		"summary": summary,
	})
}
