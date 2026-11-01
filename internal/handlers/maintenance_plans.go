package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"errors"
	"strconv"
	"strings"
	"transport-app/internal/inventory"
	"transport-app/internal/maintenance"
	"transport-app/internal/maintenance/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
)

// RegisterPlanAPIRoutes mounts the /api/v1/maintenance/plans REST endpoints (Spec 04 §14, B3).
func (h *MaintenanceHandlers) RegisterPlanAPIRoutes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "read")).Get("/api/v1/maintenance/plans", h.APIListMaintenancePlans)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "read")).Get("/api/v1/maintenance/plans/{id}", h.APIGetMaintenancePlan)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "create")).Post("/api/v1/maintenance/plans", h.APICreateMaintenancePlan)
	r.With(middleware.RequirePermission(h.AuthSrv, "maintenance", "update")).Post("/api/v1/maintenance/plans/{id}/evaluate", h.APIEvaluateMaintenancePlan)
}

func writeMaintenanceJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeMaintenanceError(w http.ResponseWriter, code int, msg string) {
	writeMaintenanceJSON(w, code, map[string]string{"error": msg})
}

// CreateMaintenancePlanRequest wire payload.
type CreateMaintenancePlanRequest struct {
	PlanNumber         string   `json:"plan_number"`
	VehicleID          string   `json:"vehicle_id"`
	MeasuringPointID   *string  `json:"measuring_point_id,omitempty"`
	ServiceType        string   `json:"service_type"`
	Description        string   `json:"description"`
	CycleIntervalKM    *float64 `json:"cycle_interval_km,omitempty"`
	CycleIntervalDays  *int     `json:"cycle_interval_days,omitempty"`
	CallHorizonPercent float64  `json:"call_horizon_percent"`
	LastScheduledKM    *float64 `json:"last_scheduled_km,omitempty"`
	NextDueKM          *float64 `json:"next_due_km,omitempty"`
}

// APICreateMaintenancePlan creates a single cycle maintenance plan.
func (h *MaintenanceHandlers) APICreateMaintenancePlan(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if tenantID == "" {
		writeMaintenanceError(w, http.StatusUnauthorized, "unauthorized: tenant context missing")
		return
	}

	var req CreateMaintenancePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeMaintenanceError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.PlanNumber == "" || req.VehicleID == "" || req.ServiceType == "" {
		writeMaintenanceError(w, http.StatusBadRequest, "plan_number, vehicle_id, and service_type are required")
		return
	}

	if req.CallHorizonPercent <= 0 {
		req.CallHorizonPercent = 100.0
	}

	now := time.Now().UTC()
	planID := uuid.NewString()

	plan := domain.MaintenancePlan{
		ID:                 planID,
		TenantID:           tenantID,
		PlanNumber:         req.PlanNumber,
		VehicleID:          req.VehicleID,
		MeasuringPointID:   req.MeasuringPointID,
		ServiceType:        req.ServiceType,
		Description:        req.Description,
		CycleIntervalKM:    req.CycleIntervalKM,
		CycleIntervalDays:  req.CycleIntervalDays,
		CallHorizonPercent: req.CallHorizonPercent,
		LastScheduledKM:    req.LastScheduledKM,
		NextDueKM:          req.NextDueKM,
		Status:             "active",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// Compute initial projection
	currOdo, _ := h.repo.GetLatestOdometer(r.Context(), req.VehicleID)
	mpID := ""
	if req.MeasuringPointID != nil {
		mpID = *req.MeasuringPointID
	}
	annualEstimate, _ := h.repo.GetMeasuringPointAnnualEstimate(r.Context(), tenantID, req.VehicleID, mpID)

	nextKM, nextDate, _ := maintenance.CalculateProjection(plan, currOdo, annualEstimate, now)
	plan.NextDueKM = nextKM
	plan.NextDueDate = nextDate

	if err := h.repo.CreateMaintenancePlan(r.Context(), plan); err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeMaintenanceJSON(w, http.StatusCreated, plan)
}

// APIListMaintenancePlans returns active and configured maintenance plans for tenant.
func (h *MaintenanceHandlers) APIListMaintenancePlans(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := r.URL.Query().Get("vehicle_id")

	plans, err := h.repo.ListMaintenancePlans(r.Context(), tenantID, vehicleID)
	if err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
		"maintenance_plans": plans,
		"count":             len(plans),
	})
}

// APIGetMaintenancePlan returns a single maintenance plan with live projections.
func (h *MaintenanceHandlers) APIGetMaintenancePlan(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	plan, err := h.repo.FindMaintenancePlan(r.Context(), tenantID, id)
	if err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if plan == nil {
		writeMaintenanceError(w, http.StatusNotFound, "maintenance plan not found")
		return
	}

	currOdo, _ := h.repo.GetLatestOdometer(r.Context(), plan.VehicleID)
	mpID := ""
	if plan.MeasuringPointID != nil {
		mpID = *plan.MeasuringPointID
	}
	annualEstimate, _ := h.repo.GetMeasuringPointAnnualEstimate(r.Context(), tenantID, plan.VehicleID, mpID)

	projKM, projDate, needsCall := maintenance.CalculateProjection(*plan, currOdo, annualEstimate, time.Now().UTC())

	writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
		"maintenance_plan":   plan,
		"current_odometer":   currOdo,
		"annual_estimate":    annualEstimate,
		"projected_due_km":   projKM,
		"projected_due_date": projDate,
		"call_horizon_due":   needsCall,
	})
}

// APIEvaluateMaintenancePlan triggers immediate schedule evaluation and work order creation.
func (h *MaintenanceHandlers) APIEvaluateMaintenancePlan(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	if h.scheduler == nil {
		h.scheduler = maintenance.NewPlanScheduler(h.DB, h.repo)
	}

	wo, err := h.scheduler.EvaluatePlan(r.Context(), tenantID, id, time.Now().UTC())
	if err != nil {
		writeMaintenanceError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if wo != nil {
		writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
			"status":         "call_triggered",
			"call_triggered": true,
			"work_order":     wo,
		})
		return
	}

	writeMaintenanceJSON(w, http.StatusOK, map[string]interface{}{
		"status":         "up_to_date",
		"call_triggered": false,
	})
}

// ---------------------------------------------------------------------------
// Merged from inventory.go
// ---------------------------------------------------------------------------

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

// Routes mounts the Inventory management web UI handlers at /inventory.
func (h *InventoryHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "read")).Get("/", h.WebInventoryDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/parts", h.WebCreatePart)
	r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/stocks/adjust", h.WebAdjustStock)
	r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/issue", h.WebIssuePart)
	r.With(middleware.RequirePermission(h.AuthSrv, "inventory", "write")).Post("/return", h.WebReturnPart)
}

// WebInventoryDashboard renders the spare parts catalog, stock balance, and ledger.
func (h *InventoryHandlers) WebInventoryDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	summary, err := h.service().GetSummary(r.Context(), tenantID)
	if err != nil {
		summary = &inventory.InventorySummary{}
	}

	categoryFilter := r.URL.Query().Get("category")
	searchFilter := r.URL.Query().Get("search")

	parts, _, err := h.service().ListParts(r.Context(), inventory.PartFilter{
		TenantID: tenantID,
		Category: categoryFilter,
		Search:   searchFilter,
		Limit:    100,
	})
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load parts", err.Error(), user)
		return
	}

	stocks, _, _ := h.service().ListStocks(r.Context(), inventory.StockFilter{
		TenantID: tenantID,
		Limit:    100,
	})

	transactions, _, _ := h.service().ListTransactions(r.Context(), inventory.TransactionFilter{
		TenantID: tenantID,
		Limit:    25,
	})

	// Fetch facilities for stock operations
	rowsF, err := h.DB.QueryContext(r.Context(), `SELECT id, name, facility_code FROM facilities WHERE tenant_id = $1 ORDER BY name ASC`, tenantID)
	var facilities []map[string]string
	if err == nil {
		defer func() { _ = rowsF.Close() }()
		for rowsF.Next() {
			var id, name, code string
			if err := rowsF.Scan(&id, &name, &code); err == nil {
				facilities = append(facilities, map[string]string{
					"ID":   id,
					"Name": name,
					"Code": code,
				})
			}
		}
	}

	// Fetch vehicles for issue/return
	rowsV, err := h.DB.QueryContext(r.Context(), `SELECT id, vehicle_number, registration_number FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	var vehicles []map[string]string
	if err == nil {
		defer func() { _ = rowsV.Close() }()
		for rowsV.Next() {
			var id, vNum, regNum string
			if err := rowsV.Scan(&id, &vNum, &regNum); err == nil {
				vehicles = append(vehicles, map[string]string{
					"ID":                 id,
					"VehicleNumber":      vNum,
					"RegistrationNumber": regNum,
				})
			}
		}
	}

	data := PageData{
		Title: "Parts & Spares Inventory",
		User:  user,
		Extra: map[string]interface{}{
			"Summary":        summary,
			"Parts":          parts,
			"Stocks":         stocks,
			"Transactions":   transactions,
			"Facilities":     facilities,
			"Vehicles":       vehicles,
			"CategoryFilter": categoryFilter,
			"Search":         searchFilter,
		},
	}

	h.renderPage(w, r, "inventory_dashboard.html", data)
}

// WebCreatePart creates a new spare part SKU in the catalog.
func (h *InventoryHandlers) WebCreatePart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	partNum := strings.TrimSpace(r.FormValue("part_number"))
	name := strings.TrimSpace(r.FormValue("name"))
	if partNum == "" || name == "" {
		h.renderError(w, http.StatusBadRequest, "Part Number and Name required", "Part number and name cannot be blank", user)
		return
	}

	cat := inventory.PartCategory(r.FormValue("category"))
	if !inventory.IsValidCategory(cat) {
		cat = inventory.CategoryGeneral
	}

	uom := inventory.PartUOM(r.FormValue("uom"))
	if !inventory.IsValidUOM(uom) {
		uom = inventory.UOMPieces
	}

	cost, _ := strconv.ParseFloat(r.FormValue("cost_price"), 64)
	selling, _ := strconv.ParseFloat(r.FormValue("selling_price"), 64)
	minStock, _ := strconv.ParseFloat(r.FormValue("min_stock_level"), 64)
	reorderQty, _ := strconv.ParseFloat(r.FormValue("reorder_quantity"), 64)

	in := inventory.CreatePartInput{
		PartNumber:       partNum,
		Name:             name,
		Category:         cat,
		Description:      strings.TrimSpace(r.FormValue("description")),
		UOM:              uom,
		CostPrice:        cost,
		SellingPrice:     selling,
		MinStockLevel:    &minStock,
		ReorderQuantity:  &reorderQty,
		OEMBrand:         strings.TrimSpace(r.FormValue("oem_brand")),
		CompatibleModels: strings.TrimSpace(r.FormValue("compatible_models")),
	}

	if _, err := h.service().CreatePart(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create part", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/inventory", http.StatusSeeOther)
}

// WebAdjustStock handles manual physical count reconciliation.
func (h *InventoryHandlers) WebAdjustStock(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	partID := strings.TrimSpace(r.FormValue("part_id"))
	facilityID := strings.TrimSpace(r.FormValue("facility_id"))
	newQty, _ := strconv.ParseFloat(r.FormValue("new_quantity_on_hand"), 64)
	reason := strings.TrimSpace(r.FormValue("reason"))

	performer := "Store Keeper"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := inventory.AdjustStockInput{
		PartID:            partID,
		FacilityID:        facilityID,
		NewQuantityOnHand: newQty,
		Reason:            reason,
		PerformedBy:       performer,
		Notes:             strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().AdjustStock(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to adjust stock", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/inventory", http.StatusSeeOther)
}

// WebIssuePart issues spares to a vehicle or work order.
func (h *InventoryHandlers) WebIssuePart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	qty, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)
	performer := "Store Keeper"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := inventory.IssuePartInput{
		PartID:      strings.TrimSpace(r.FormValue("part_id")),
		FacilityID:  strings.TrimSpace(r.FormValue("facility_id")),
		Quantity:    qty,
		VehicleID:   strings.TrimSpace(r.FormValue("vehicle_id")),
		WorkOrderID: strings.TrimSpace(r.FormValue("work_order_id")),
		PerformedBy: performer,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().IssuePart(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to issue part", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/inventory", http.StatusSeeOther)
}

// WebReturnPart returns unused parts back into facility stock.
func (h *InventoryHandlers) WebReturnPart(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	qty, _ := strconv.ParseFloat(r.FormValue("quantity"), 64)
	performer := "Store Keeper"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := inventory.ReturnPartInput{
		PartID:      strings.TrimSpace(r.FormValue("part_id")),
		FacilityID:  strings.TrimSpace(r.FormValue("facility_id")),
		Quantity:    qty,
		VehicleID:   strings.TrimSpace(r.FormValue("vehicle_id")),
		WorkOrderID: strings.TrimSpace(r.FormValue("work_order_id")),
		PerformedBy: performer,
		Notes:       strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().ReturnPart(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to return part", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/inventory", http.StatusSeeOther)
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
