package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/tyre"
)

type TyreHandlers struct {
	*App
}

func (h *TyreHandlers) RegisterAPIRoutes(r chi.Router) {
	r.Route("/api/v1/tyres", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/", h.APIListTyres)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/", h.APICreateTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/summary", h.APIGetFleetSummary)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/rotate", h.APIRotateTyres)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}", h.APIGetTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Put("/{id}", h.APIUpdateTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/mount", h.APIMountTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/dismount", h.APIDismountTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/inspect", h.APIRecordInspection)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/retread", h.APIRetreadTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/scrap", h.APIScrapTyre)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}/activities", h.APIListActivities)
		r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/{id}/inspections", h.APIListInspections)
	})

	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/api/v1/vehicles/{id}/tyres", h.APIGetVehicleTyres)
}

// Routes mounts the Tyre Management web UI handlers at /tyres.
func (h *TyreHandlers) Routes(r chi.Router) {
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "read")).Get("/", h.WebTyresDashboard)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/", h.WebCreateTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/mount", h.WebMountTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/dismount", h.WebDismountTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/inspect", h.WebRecordInspection)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/retread", h.WebRetreadTyre)
	r.With(middleware.RequirePermission(h.AuthSrv, "tyres", "write")).Post("/{id}/scrap", h.WebScrapTyre)
}

// WebTyresDashboard renders the tyre lifecycle and CPKM management cockpit.
func (h *TyreHandlers) WebTyresDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	summary, err := h.service().GetFleetSummary(r.Context(), tenantID)
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load tyre metrics", err.Error(), user)
		return
	}

	statusFilter := r.URL.Query().Get("status")
	tyres, _, err := h.service().ListTyres(r.Context(), tyre.TyreFilter{
		TenantID: tenantID,
		Status:   statusFilter,
		Search:   r.URL.Query().Get("search"),
		Limit:    50,
	})
	if err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to load tyres", err.Error(), user)
		return
	}

	// Fetch vehicles for mounting dropdown
	rows, err := h.DB.QueryContext(r.Context(), `SELECT id, vehicle_number, registration_number
		FROM vehicles WHERE tenant_id = $1 ORDER BY vehicle_number ASC`, tenantID)
	var allVehicles []map[string]string
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, vNum, regNum string
			if err := rows.Scan(&id, &vNum, &regNum); err == nil {
				allVehicles = append(allVehicles, map[string]string{
					"ID":                 id,
					"VehicleNumber":      vNum,
					"RegistrationNumber": regNum,
				})
			}
		}
	}

	data := PageData{
		Title: "Tyre Management",
		User:  user,
		Extra: map[string]interface{}{
			"Summary":     summary,
			"Tyres":       tyres,
			"AllVehicles": allVehicles,
			"Status":      statusFilter,
		},
	}

	h.renderPage(w, r, "tyres_dashboard.html", data)
}

// WebCreateTyre handles form submission to add a new tyre to stock.
func (h *TyreHandlers) WebCreateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	serial := strings.TrimSpace(r.FormValue("serial_number"))
	if serial == "" {
		h.renderError(w, http.StatusBadRequest, "Serial number required", "serial_number cannot be blank", user)
		return
	}

	brand := strings.TrimSpace(r.FormValue("brand"))
	if brand == "" {
		brand = "MRF"
	}
	model := strings.TrimSpace(r.FormValue("model"))
	size := strings.TrimSpace(r.FormValue("size"))
	if size == "" {
		size = "295/80 R22.5"
	}

	tType := tyre.TyreType(r.FormValue("tyre_type"))
	if !tyre.IsValidTyreType(tType) {
		tType = tyre.TyreTypeRadial
	}

	initTread, _ := strconv.ParseFloat(r.FormValue("initial_tread_depth_mm"), 64)
	if initTread <= 0 {
		initTread = 15.0
	}
	minTread, _ := strconv.ParseFloat(r.FormValue("min_tread_depth_mm"), 64)
	if minTread <= 0 {
		minTread = 3.0
	}
	cost, _ := strconv.ParseFloat(r.FormValue("purchase_cost"), 64)

	in := tyre.CreateTyreInput{
		SerialNumber:        serial,
		Brand:               brand,
		Model:               model,
		Size:                size,
		TyreType:            tType,
		InitialTreadDepthMM: &initTread,
		MinTreadDepthMM:     &minTread,
		PurchaseCost:        cost,
		VendorName:          strings.TrimSpace(r.FormValue("vendor_name")),
		InvoiceNumber:       strings.TrimSpace(r.FormValue("invoice_number")),
	}

	if _, err := h.service().CreateTyre(r.Context(), tenantID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to create tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebMountTyre handles mounting a tyre onto an axle position.
func (h *TyreHandlers) WebMountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	vehID := strings.TrimSpace(r.FormValue("vehicle_id"))
	axle := strings.TrimSpace(r.FormValue("axle_position"))
	odo, _ := strconv.ParseFloat(r.FormValue("odometer_km"), 64)

	performer := "Fleet Tech"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.MountTyreInput{
		VehicleID:       vehID,
		AxlePosition:    axle,
		MountOdometerKM: odo,
		PerformedBy:     performer,
	}

	if _, err := h.service().MountTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to mount tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebDismountTyre handles dismounting a tyre to stock.
func (h *TyreHandlers) WebDismountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	odo, _ := strconv.ParseFloat(r.FormValue("odometer_km"), 64)
	reason := strings.TrimSpace(r.FormValue("reason"))

	performer := "Fleet Tech"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.DismountTyreInput{
		DismountOdometerKM: odo,
		Reason:             reason,
		PerformedBy:        performer,
	}

	if _, err := h.service().DismountTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to dismount tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebRecordInspection records tread depth and pressure check.
func (h *TyreHandlers) WebRecordInspection(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	tread, _ := strconv.ParseFloat(r.FormValue("tread_depth_mm"), 64)
	pressure, _ := strconv.ParseFloat(r.FormValue("pressure_psi"), 64)
	odo, _ := strconv.ParseFloat(r.FormValue("odometer_km"), 64)
	cond := tyre.InspectionCondition(r.FormValue("condition"))
	if cond == "" {
		cond = tyre.ConditionGood
	}

	inspector := "Inspector"
	if user != nil && user.UserID != "" {
		inspector = user.UserID
	}

	in := tyre.RecordInspectionInput{
		OdometerKM:    odo,
		TreadDepthMM:  tread,
		PressurePSI:   &pressure,
		Condition:     cond,
		ActionTaken:   strings.TrimSpace(r.FormValue("action_taken")),
		InspectorName: inspector,
		Notes:         strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().RecordInspection(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to record tyre inspection", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebRetreadTyre records sending or receiving tyre from retread.
func (h *TyreHandlers) WebRetreadTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	action := strings.TrimSpace(r.FormValue("action"))
	if action == "" {
		action = "SEND"
	}
	cost, _ := strconv.ParseFloat(r.FormValue("cost"), 64)
	newTread, _ := strconv.ParseFloat(r.FormValue("new_tread_depth_mm"), 64)

	performer := "Fleet Manager"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.RetreadTyreInput{
		Action:          action,
		VendorName:      strings.TrimSpace(r.FormValue("vendor_name")),
		Cost:            cost,
		NewTreadDepthMM: &newTread,
		PerformedBy:     performer,
		Notes:           strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().RetreadTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to process retread", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

// WebScrapTyre scraps a tyre and records salvage recovery.
func (h *TyreHandlers) WebScrapTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")
	user, _ := h.getUserFromContext(r)

	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "Invalid form submission", err.Error(), user)
		return
	}

	recovery, _ := strconv.ParseFloat(r.FormValue("recovery_amount"), 64)
	reason := strings.TrimSpace(r.FormValue("reason"))

	performer := "Fleet Manager"
	if user != nil && user.UserID != "" {
		performer = user.UserID
	}

	in := tyre.ScrapTyreInput{
		Reason:         reason,
		RecoveryAmount: recovery,
		PerformedBy:    performer,
		Notes:          strings.TrimSpace(r.FormValue("notes")),
	}

	if _, err := h.service().ScrapTyre(r.Context(), tenantID, tyreID, in); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Failed to scrap tyre", err.Error(), user)
		return
	}

	http.Redirect(w, r, "/tyres", http.StatusSeeOther)
}

func writeTyreJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeTyreError(w http.ResponseWriter, code int, msg string) {
	writeTyreJSON(w, code, map[string]string{"error": msg})
}

func (h *TyreHandlers) service() *tyre.Service {
	return tyre.NewService(tyre.NewSQLRepository(h.DB))
}

func (h *TyreHandlers) APIListTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	limit := 50
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = l
	}
	offset := 0
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	filter := tyre.TyreFilter{
		TenantID:  tenantID,
		VehicleID: r.URL.Query().Get("vehicle_id"),
		Status:    strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("status"))),
		Brand:     r.URL.Query().Get("brand"),
		Search:    r.URL.Query().Get("q"),
		Limit:     limit,
		Offset:    offset,
	}

	tyres, total, err := h.service().ListTyres(r.Context(), filter)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyres":  tyres,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *TyreHandlers) APICreateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in tyre.CreateTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	created, err := h.service().CreateTyre(r.Context(), tenantID, in)
	if err != nil {
		if errors.Is(err, tyre.ErrDuplicateSerial) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusCreated, map[string]interface{}{
		"tyre": created,
	})
}

func (h *TyreHandlers) APIGetTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	t, err := h.service().GetTyre(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": t,
	})
}

func (h *TyreHandlers) APIUpdateTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.UpdateTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().UpdateTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": updated,
	})
}

func (h *TyreHandlers) APIMountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.MountTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	mounted, err := h.service().MountTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrAxleOccupied) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		if errors.Is(err, tyre.ErrInvalidStatus) {
			writeTyreError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": mounted,
	})
}

func (h *TyreHandlers) APIDismountTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.DismountTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	dismounted, err := h.service().DismountTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrInvalidStatus) {
			writeTyreError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": dismounted,
	})
}

func (h *TyreHandlers) APIRotateTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	var in tyre.RotateTyresInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if err := h.service().RotateTyres(r.Context(), tenantID, in); err != nil {
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "success",
		"message": "tyres rotated successfully",
	})
}

func (h *TyreHandlers) APIRecordInspection(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.RecordInspectionInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	insp, err := h.service().RecordInspection(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusCreated, map[string]interface{}{
		"inspection": insp,
	})
}

func (h *TyreHandlers) APIRetreadTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.RetreadTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	updated, err := h.service().RetreadTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		if errors.Is(err, tyre.ErrMaxRetreadsReached) {
			writeTyreError(w, http.StatusConflict, err.Error())
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": updated,
	})
}

func (h *TyreHandlers) APIScrapTyre(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	id := chi.URLParam(r, "id")

	var in tyre.ScrapTyreInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeTyreError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	scrapped, err := h.service().ScrapTyre(r.Context(), tenantID, id, in)
	if err != nil {
		if errors.Is(err, tyre.ErrTyreNotFound) {
			writeTyreError(w, http.StatusNotFound, "tyre not found")
			return
		}
		writeTyreError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"tyre": scrapped,
	})
}

func (h *TyreHandlers) APIGetVehicleTyres(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehID := chi.URLParam(r, "id")

	tyres, err := h.service().GetVehicleTyres(r.Context(), tenantID, vehID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"vehicle_id": vehID,
		"tyres":      tyres,
	})
}

func (h *TyreHandlers) APIGetFleetSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))

	summary, err := h.service().GetFleetSummary(r.Context(), tenantID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"summary": summary,
	})
}

func (h *TyreHandlers) APIListActivities(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")

	activities, err := h.service().ListActivities(r.Context(), tenantID, tyreID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"activities": activities,
	})
}

func (h *TyreHandlers) APIListInspections(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	tyreID := chi.URLParam(r, "id")

	inspections, err := h.service().ListInspections(r.Context(), tenantID, tyreID)
	if err != nil {
		writeTyreError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeTyreJSON(w, http.StatusOK, map[string]interface{}{
		"inspections": inspections,
	})
}
