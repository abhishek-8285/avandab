package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/vehicle/application"
	"transport-app/internal/vehicle/domain/aggregate"
)

// APIVehicleHandler handles REST endpoints for the vehicle vertical slice
// (fleet registry, TMS SOP parity spec).
type APIVehicleHandler struct {
	createUC      *application.CreateVehicleUseCase
	updateUC      *application.UpdateVehicleUseCase
	deleteUC      *application.DeleteVehicleUseCase
	getUC         *application.GetVehicleUseCase
	listUC        *application.ListVehiclesUseCase
	createPointUC *application.CreateMeasuringPointUseCase
	recordUC      *application.RecordMeasurementUseCase
	listMeasUC    *application.ListVehicleMeasurements
	authSrv       auth.AuthorizationService
}

// NewAPIVehicleHandler constructs an APIVehicleHandler.
func NewAPIVehicleHandler(
	createUC *application.CreateVehicleUseCase,
	updateUC *application.UpdateVehicleUseCase,
	deleteUC *application.DeleteVehicleUseCase,
	getUC *application.GetVehicleUseCase,
	listUC *application.ListVehiclesUseCase,
	createPointUC *application.CreateMeasuringPointUseCase,
	recordUC *application.RecordMeasurementUseCase,
	listMeasUC *application.ListVehicleMeasurements,
	authSrv auth.AuthorizationService,
) *APIVehicleHandler {
	return &APIVehicleHandler{
		createUC:      createUC,
		updateUC:      updateUC,
		deleteUC:      deleteUC,
		getUC:         getUC,
		listUC:        listUC,
		createPointUC: createPointUC,
		recordUC:      recordUC,
		listMeasUC:    listMeasUC,
		authSrv:       authSrv,
	}
}

// Register mounts all vehicle routes.
func (h *APIVehicleHandler) Register(r chi.Router) {
	r.Route("/api/v1/vehicles", func(r chi.Router) {
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "read")).Get("/", h.List)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "create")).Post("/", h.Create)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "read")).Get("/{id}", h.Get)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "update")).Put("/{id}", h.Update)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "delete")).Delete("/{id}", h.Delete)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "update")).Post("/{id}/points", h.CreatePoint)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "read")).Get("/{id}/measurements", h.ListMeasurements)
		r.With(middleware.RequirePermission(h.authSrv, "vehicles", "update")).Post("/{id}/measurements", h.RecordMeasurement)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// vehicleRequest mirrors CreateVehicleRequest/UpdateVehicleRequest.
// Dates are YYYY-MM-DD (openapi format: date). Profile is a pointer:
// nil preserves the stored profile on PUT.
type vehicleRequest struct {
	RegistrationNumber string                    `json:"registration_number"`
	VehicleNumber      string                    `json:"vehicle_number"`
	VehicleType        string                    `json:"vehicle_type"`
	Capacity           *int64                    `json:"capacity"`
	FuelType           string                    `json:"fuel_type"`
	InsuranceExpiry    string                    `json:"insurance_expiry"`
	FitnessExpiry      string                    `json:"fitness_expiry"`
	PermitExpiry       string                    `json:"permit_expiry"`
	Status             string                    `json:"status"`
	CurrentMileage     *float64                  `json:"current_mileage"`
	Blocked            *bool                     `json:"blocked"`
	BlockedReason      string                    `json:"blocked_reason"`
	RCExpiry           string                    `json:"rc_expiry"`
	PUCExpiry          string                    `json:"puc_expiry"`
	Odometer           *float64                  `json:"odometer"`
	Profile            *aggregate.VehicleProfile `json:"profile"`
}

// parseAPIDateOpt parses an optional YYYY-MM-DD date; "" = not provided.
func parseAPIDateOpt(s, field string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, errors.New(field + " must be YYYY-MM-DD")
	}
	return &t, nil
}

func parseAPIDate(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New(field + " is required (YYYY-MM-DD)")
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, errors.New(field + " must be YYYY-MM-DD")
	}
	return t, nil
}

func (h *APIVehicleHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req vehicleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	insExp, err := parseAPIDate(req.InsuranceExpiry, "insurance_expiry")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	fitExp, err := parseAPIDate(req.FitnessExpiry, "fitness_expiry")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	perExp, err := parseAPIDate(req.PermitExpiry, "permit_expiry")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	var profile aggregate.VehicleProfile
	if req.Profile != nil {
		profile = *req.Profile
	}
	capacity := int64(0)
	if req.Capacity != nil {
		capacity = *req.Capacity
	}
	rcExp, err := parseAPIDateOpt(req.RCExpiry, "rc_expiry")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	pucExp, err := parseAPIDateOpt(req.PUCExpiry, "puc_expiry")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var odometer float64
	if req.Odometer != nil {
		odometer = *req.Odometer
	}
	var blocked bool
	if req.Blocked != nil {
		blocked = *req.Blocked
	}
	id, err := h.createUC.Execute(r.Context(), application.CreateVehicleCommand{
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: req.RegistrationNumber,
		VehicleNumber:      req.VehicleNumber,
		VehicleType:        aggregate.VehicleType(req.VehicleType),
		Capacity:           capacity,
		FuelType:           aggregate.FuelType(req.FuelType),
		InsuranceExpiry:    insExp,
		FitnessExpiry:      fitExp,
		PermitExpiry:       perExp,
		CurrentMileage:     req.CurrentMileage,
		Blocked:            blocked,
		BlockedReason:      req.BlockedReason,
		RCExpiry:           rcExp,
		PUCExpiry:          pucExp,
		Odometer:           odometer,
		Profile:            profile,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": string(id)})
}

func (h *APIVehicleHandler) List(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}

	res, err := h.listUC.Execute(r.Context(), application.ListVehiclesQuery{
		TenantID:   shared.TenantIDFromContext(r.Context()),
		Page:       page,
		Limit:      limit,
		Search:     r.URL.Query().Get("search"),
		Status:     r.URL.Query().Get("status"),
		FleetClass: r.URL.Query().Get("fleet_class"),
		Ownership:  r.URL.Query().Get("ownership"),
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Failed to list vehicles")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"vehicles": res.Vehicles, "total": res.Total})
}

func (h *APIVehicleHandler) Get(w http.ResponseWriter, r *http.Request) {
	dto, err := h.getUC.Execute(r.Context(), application.GetVehicleQuery{
		ID:       aggregate.VehicleID(chi.URLParam(r, "id")),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "vehicle not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "Failed to get vehicle")
		return
	}

	writeJSON(w, http.StatusOK, dto)
}

func (h *APIVehicleHandler) Update(w http.ResponseWriter, r *http.Request) {
	var req vehicleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Merge over stored values so partial PUTs never clobber the profile
	// or compliance dates with zero values.
	stored, err := h.getUC.Execute(r.Context(), application.GetVehicleQuery{
		ID:       aggregate.VehicleID(chi.URLParam(r, "id")),
		TenantID: shared.TenantIDFromContext(r.Context()),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "vehicle not found")
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "Failed to get vehicle")
		return
	}

	merge := func(s, fallback string) string {
		if s != "" {
			return s
		}
		return fallback
	}
	insExp := stored.InsuranceExpiry
	if req.InsuranceExpiry != "" {
		if insExp, err = parseAPIDate(req.InsuranceExpiry, "insurance_expiry"); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	fitExp := stored.FitnessExpiry
	if req.FitnessExpiry != "" {
		if fitExp, err = parseAPIDate(req.FitnessExpiry, "fitness_expiry"); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	perExp := stored.PermitExpiry
	if req.PermitExpiry != "" {
		if perExp, err = parseAPIDate(req.PermitExpiry, "permit_expiry"); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	mileage := stored.CurrentMileage
	if req.CurrentMileage != nil {
		mileage = req.CurrentMileage
	}
	rcExp := stored.RCExpiry
	if req.RCExpiry != "" {
		if rcExp, err = parseAPIDateOpt(req.RCExpiry, "rc_expiry"); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	pucExp := stored.PUCExpiry
	if req.PUCExpiry != "" {
		if pucExp, err = parseAPIDateOpt(req.PUCExpiry, "puc_expiry"); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	odometer := stored.Odometer
	if req.Odometer != nil {
		odometer = *req.Odometer
	}
	blocked := stored.Blocked
	if req.Blocked != nil {
		blocked = *req.Blocked
	}
	blockedReason := stored.BlockedReason
	if req.BlockedReason != "" {
		blockedReason = req.BlockedReason
	}
	if aggregate.VehicleStatus(req.Status) == aggregate.VehicleBlocked {
		blocked = true
	}
	capacity := stored.Capacity
	if req.Capacity != nil {
		capacity = *req.Capacity
	}
	profile := stored.Profile
	if req.Profile != nil {
		profile = *req.Profile
	}
	status := stored.Status
	if req.Status != "" {
		status = req.Status
	}

	err = h.updateUC.Execute(r.Context(), application.UpdateVehicleCommand{
		ID:                 aggregate.VehicleID(chi.URLParam(r, "id")),
		TenantID:           shared.TenantIDFromContext(r.Context()),
		RegistrationNumber: merge(req.RegistrationNumber, stored.RegistrationNumber),
		VehicleNumber:      merge(req.VehicleNumber, stored.VehicleNumber),
		VehicleType:        aggregate.VehicleType(merge(req.VehicleType, stored.VehicleType)),
		Capacity:           capacity,
		FuelType:           aggregate.FuelType(merge(req.FuelType, stored.FuelType)),
		InsuranceExpiry:    insExp,
		FitnessExpiry:      fitExp,
		PermitExpiry:       perExp,
		Status:             aggregate.VehicleStatus(status),
		CurrentMileage:     mileage,
		Blocked:            blocked,
		BlockedReason:      blockedReason,
		RCExpiry:           rcExp,
		PUCExpiry:          pucExp,
		Odometer:           odometer,
		Profile:            profile,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *APIVehicleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.deleteUC.Execute(r.Context(),
		aggregate.VehicleID(chi.URLParam(r, "id")),
		shared.TenantIDFromContext(r.Context()))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeAPIError(w, http.StatusNotFound, "vehicle not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *APIVehicleHandler) CreatePoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind           string  `json:"kind"`
		MeasPosition   string  `json:"meas_position"`
		Unit           string  `json:"unit"`
		AnnualEstimate float64 `json:"annual_estimate"`
		Description    string  `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	point, err := h.createPointUC.Execute(r.Context(), application.CreateMeasuringPointCommand{
		TenantID:       shared.TenantIDFromContext(r.Context()),
		VehicleID:      chi.URLParam(r, "id"),
		Kind:           req.Kind,
		MeasPosition:   req.MeasPosition,
		Unit:           req.Unit,
		AnnualEstimate: req.AnnualEstimate,
		Description:    req.Description,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": point.ID})
}

func (h *APIVehicleHandler) ListMeasurements(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	docs, err := h.listMeasUC.Execute(r.Context(),
		shared.TenantIDFromContext(r.Context()),
		chi.URLParam(r, "id"), limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "Failed to list measurements")
		return
	}

	writeJSON(w, http.StatusOK, docs)
}

func (h *APIVehicleHandler) RecordMeasurement(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PointID    string  `json:"point_id"`
		Counter    float64 `json:"counter_reading"`
		MeasuredAt string  `json:"measured_at"`
		ReadBy     string  `json:"read_by"`
		Remarks    string  `json:"remarks"`
		RecordedBy string  `json:"recorded_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	var measuredAt time.Time
	if req.MeasuredAt != "" {
		var err error
		if measuredAt, err = parseAPIDate(req.MeasuredAt, "measured_at"); err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	doc, err := h.recordUC.Execute(r.Context(), application.RecordMeasurementCommand{
		TenantID:   shared.TenantIDFromContext(r.Context()),
		PointID:    req.PointID,
		Counter:    req.Counter,
		MeasuredAt: measuredAt,
		ReadBy:     req.ReadBy,
		Remarks:    req.Remarks,
		RecordedBy: req.RecordedBy,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(doc)
}
