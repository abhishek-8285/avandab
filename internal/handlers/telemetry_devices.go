package handlers

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/middleware"
	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
	uow "transport-app/internal/shared/uow"
	"transport-app/internal/telemetry"
)

// TelemetryDeviceHandlers powers the device registry / provisioning / quarantine
// admin UI (Spec 01 §3, §4.1, §10). Mounted inside the protected web group so
// every route is gated by RequireAuth + telemetry:* permissions.
type TelemetryDeviceHandlers struct {
	*App
	service *telemetry.DeviceService
}

// NewTelemetryDeviceHandlers wires the DeviceService against the app DB.
func NewTelemetryDeviceHandlers(app *App, db *sql.DB, pepper string) *TelemetryDeviceHandlers {
	store := telemetry.NewDeviceStore(db)
	quarantine := telemetry.NewQuarantineStore(db)
	unit := uow.NewSQLUnitOfWork(db)
	return &TelemetryDeviceHandlers{
		App: app,
		service: telemetry.NewDeviceService(store, quarantine, unit, pepper,
			id.NewUUIDGenerator(), telemetry.NewAuditLogger(db)),
	}
}

// Routes mounts the device-registry routes.
func (h *TelemetryDeviceHandlers) Routes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "read")).Get("/", h.List)
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "update")).Get("/bulk", h.BulkRegisterForm)
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "update")).Post("/bulk", h.BulkRegister)
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "update")).Post("/{imei}/assign", h.Assign)
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "update")).Post("/{imei}/activate", h.Activate)
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "update")).Post("/{imei}/retire", h.Retire)
}

// QuarantineRoutes mounts the quarantine-admin routes.
func (h *TelemetryDeviceHandlers) QuarantineRoutes(r chi.Router) {
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "read")).Get("/", h.QuarantineQueue)
	r.With(middleware.ResourcePermission(h.AuthSrv, "telemetry", "update")).Post("/{id}/resolve", h.Resolve)
}

func (h *TelemetryDeviceHandlers) tenantID(ctx context.Context) string {
	// Fail closed: no DefaultTenant fallback. Reads with an empty tenant
	// return nothing; writes are blocked by requireTenant below.
	return string(shared.TenantIDFromContext(ctx))
}

func (h *TelemetryDeviceHandlers) withTenant(ctx context.Context) context.Context {
	return ctx
}

// requireTenant blocks mutating device-registry calls that arrive without a
// resolved tenant instead of writing them into tenant "1".
func (h *TelemetryDeviceHandlers) requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	if t := h.tenantID(r.Context()); t != "" {
		return t, true
	}
	http.Error(w, "tenant not set in request context", http.StatusBadRequest)
	return "", false
}

// List renders the devices table (full page or Datastar fragment).
func (h *TelemetryDeviceHandlers) List(w http.ResponseWriter, r *http.Request) {
	ctx := h.withTenant(r.Context())
	pp := parsePaginationParams(r)
	tenant := h.tenantID(ctx)

	store := telemetry.NewDeviceStore(h.DB)
	var devices []telemetry.Device
	var total int64
	var err error
	if pp.Query != "" || pp.Status != "" || pp.DateFrom != "" || pp.DateTo != "" {
		devices, err = store.ListByTenantFiltered(ctx, tenant, pp.Query, pp.Status, pp.DateFrom, pp.DateTo, pp.Limit, pp.Offset)
		if err == nil {
			total, err = store.CountByTenantFiltered(ctx, tenant, pp.Query, pp.Status, pp.DateFrom, pp.DateTo)
		}
	} else {
		devices, err = store.ListByTenant(ctx, tenant, pp.Limit, pp.Offset)
		if err == nil {
			total, err = store.CountByTenant(ctx, tenant)
		}
	}
	if err != nil {
		http.Error(w, "Failed to list devices", http.StatusInternalServerError)
		return
	}
	pd := newPaginationData(pp, total, "/telemetry/devices")
	pd.From = pp.DateFrom
	pd.To = pp.DateTo
	session, _ := h.getUserFromContext(r)

	flash := readFlashCookies(r, w)

	if isDatastarRequest(r) {
		h.renderFragment(w, "telemetry_device_row.html", map[string]interface{}{
			"Devices": devices, "Pagination": pd, "Query": pp.Query,
			"User": session, "StatusFilter": pp.Status,
			"DateFrom": pp.DateFrom, "DateTo": pp.DateTo,
		})
		return
	}

	h.renderPage(w, r, "telemetry_devices.html", PageData{
		Title:      "Telemetry Devices",
		User:       session,
		FlashError: flash.error, FlashSuccess: flash.success,
		Extra: map[string]interface{}{
			"Devices":      devices,
			"Pagination":   pd,
			"Query":        pp.Query,
			"StatusFilter": pp.Status,
			"DateFrom":     pp.DateFrom,
			"DateTo":       pp.DateTo,
		},
	})
}

// BulkRegisterForm renders the bulk-registration form.
func (h *TelemetryDeviceHandlers) BulkRegisterForm(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	h.renderForm(w, r, "telemetry_devices_register.html", PageData{Title: "Register Devices", User: session})
}

// BulkRegister accepts a JSON array or CSV paste and registers atomically.
func (h *TelemetryDeviceHandlers) BulkRegister(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	if _, ok := h.requireTenant(w, r); !ok {
		return
	}
	ctx := h.withTenant(r.Context())

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	cmds, parseErr := parseBulkDevices(body)
	if parseErr != nil {
		flash := "Invalid payload: " + parseErr.Error()
		if isDatastarRequest(r) {
			h.renderFragment(w, "telemetry_register_result.html", map[string]interface{}{"Error": flash})
			return
		}
		http.SetCookie(w, flashCookie("flash_error", flash))
		h.renderPage(w, r, "telemetry_devices_register.html", PageData{Title: "Register Devices", User: session, FlashError: flash})
		return
	}

	results, svcErr := h.service.BulkRegister(ctx, cmds)
	if svcErr != nil {
		flash := "Registration failed: " + svcErr.Error()
		if isDatastarRequest(r) {
			h.renderFragment(w, "telemetry_register_result.html", map[string]interface{}{"Error": flash, "Results": results})
			return
		}
		http.SetCookie(w, flashCookie("flash_error", flash))
		h.renderForm(w, r, "telemetry_devices_register.html", PageData{Title: "Register Devices", User: session, FlashError: flash})
		return
	}

	ok := 0
	for _, res := range results {
		if res.Success {
			ok++
		}
	}
	if isDatastarRequest(r) {
		h.renderFragment(w, "telemetry_register_result.html", map[string]interface{}{
			"Success": ok,
			"Total":   len(results),
			"Results": results,
		})
		return
	}
	http.SetCookie(w, flashCookie("flash_success",
		fmt.Sprintf("Registered %d/%d devices", ok, len(results))))
	http.Redirect(w, r, "/telemetry/devices", http.StatusSeeOther)
}

// Assign binds a device to a vehicle (inventory → assigned).
func (h *TelemetryDeviceHandlers) Assign(w http.ResponseWriter, r *http.Request) {
	imei := chi.URLParam(r, "imei")
	vehicleID := r.PostFormValue("vehicle_id")
	if _, ok := h.requireTenant(w, r); !ok {
		return
	}
	ctx := h.withTenant(r.Context())
	if err := h.service.AssignDevice(ctx, imei, vehicleID); err != nil {
		renderErrorFragment(w, r, "Assign failed", err.Error())
		return
	}
	renderReplaceRow(w, r, "/telemetry/devices/"+imei)
}

// Activate provisions the device secret and returns it once.
func (h *TelemetryDeviceHandlers) Activate(w http.ResponseWriter, r *http.Request) {
	imei := chi.URLParam(r, "imei")
	if _, ok := h.requireTenant(w, r); !ok {
		return
	}
	ctx := h.withTenant(r.Context())
	result, err := h.service.ActivateDevice(ctx, imei)
	if err != nil {
		renderErrorFragment(w, r, "Activation failed", err.Error())
		return
	}
	if isDatastarRequest(r) {
		h.renderFragment(w, "telemetry_device_secret.html", map[string]interface{}{
			"Device":    result.Device,
			"RawSecret": result.RawSecret,
		})
		return
	}
	http.SetCookie(w, flashCookie("flash_success", "Device activated — secret provisioned"))
	http.Redirect(w, r, "/telemetry/devices", http.StatusSeeOther)
}

// Retire moves a device to retired (quarantined by the pipeline thereafter).
func (h *TelemetryDeviceHandlers) Retire(w http.ResponseWriter, r *http.Request) {
	imei := chi.URLParam(r, "imei")
	if _, ok := h.requireTenant(w, r); !ok {
		return
	}
	ctx := h.withTenant(r.Context())
	if err := h.service.RetireDevice(ctx, imei); err != nil {
		renderErrorFragment(w, r, "Retire failed", err.Error())
		return
	}
	renderReplaceRow(w, r, "/telemetry/devices/"+imei)
}

// QuarantineQueue lists open quarantine entries.
func (h *TelemetryDeviceHandlers) QuarantineQueue(w http.ResponseWriter, r *http.Request) {
	ctx := h.withTenant(r.Context())
	session, _ := h.getUserFromContext(r)
	store := telemetry.NewQuarantineStore(h.DB)
	entries, err := store.ListOpen(ctx, h.tenantID(ctx), 100)
	if err != nil {
		http.Error(w, "Failed to list quarantine", http.StatusInternalServerError)
		return
	}
	if isDatastarRequest(r) {
		h.renderFragment(w, "telemetry_quarantine_row.html", map[string]interface{}{"Entries": entries, "User": session})
		return
	}
	h.renderPage(w, r, "telemetry_quarantine_queue.html", PageData{
		Title: "Quarantine Queue",
		User:  session,
		Extra: map[string]interface{}{"Entries": entries},
	})
}

// Resolve applies an admin decision to a quarantine entry (Spec 01 §7).
func (h *TelemetryDeviceHandlers) Resolve(w http.ResponseWriter, r *http.Request) {
	session, _ := h.getUserFromContext(r)
	entryID := chi.URLParam(r, "id")
	action := r.PostFormValue("action")
	vehicleID := r.PostFormValue("vehicle_id")
	var vID *string
	if vehicleID != "" {
		vID = &vehicleID
	}
	if _, ok := h.requireTenant(w, r); !ok {
		return
	}
	ctx := h.withTenant(r.Context())
	if err := h.service.ResolveQuarantine(ctx, telemetry.ResolveQuarantineCommand{
		EntryID: entryID, Action: action, VehicleID: vID,
		UserID: session.UserID,
	}); err != nil {
		renderErrorFragment(w, r, "Resolve failed", err.Error())
		return
	}
	w.Header().Set("HX-Redirect", "/telemetry/quarantine")
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// ── small render helpers (kept in this file to avoid app.go churn) ─────

func renderReplaceRow(w http.ResponseWriter, r *http.Request, rowID string) {
	if isDatastarRequest(r) {
		w.Header().Set("HX-Redirect", "/telemetry/devices")
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/telemetry/devices", http.StatusSeeOther)
}

func renderErrorFragment(w http.ResponseWriter, r *http.Request, title, msg string) {
	if isDatastarRequest(r) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusUnprocessableEntity)
		escaped := "<div class=\"p-3 rounded bg-status-alert/10 text-status-alert\">" +
			"<strong>" + html.EscapeString(title) + "</strong>: " + html.EscapeString(msg) + "</div>"
		_, _ = w.Write([]byte(escaped))
		return
	}
	http.SetCookie(w, flashCookie("flash_error", title+": "+msg))
	http.Redirect(w, r, "/telemetry/devices", http.StatusSeeOther)
}

func flashCookie(name, value string) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/",
		HttpOnly: true, MaxAge: 5,
	}
}

type flashPair struct{ success, error string }

func readFlashCookies(r *http.Request, w http.ResponseWriter) flashPair {
	var f flashPair
	if c, err := r.Cookie("flash_success"); err == nil && c.Value != "" {
		f.success = c.Value
		http.SetCookie(w, &http.Cookie{Name: "flash_success", Value: "", Path: "/", MaxAge: -1})
	}
	if c, err := r.Cookie("flash_error"); err == nil && c.Value != "" {
		f.error = c.Value
		http.SetCookie(w, &http.Cookie{Name: "flash_error", Value: "", Path: "/", MaxAge: -1})
	}
	return f
}

// parseBulkDevices accepts a JSON array of RegisterDeviceCommand objects or a
// CSV paste: one device per line as imei,serial_number,device_type,vehicle_id.
func parseBulkDevices(body []byte) ([]telemetry.RegisterDeviceCommand, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, errors.New("empty payload")
	}
	if trimmed[0] == '[' || trimmed[0] == '{' {
		var arr []telemetry.RegisterDeviceCommand
		if err := json.Unmarshal([]byte(trimmed), &arr); err != nil {
			return nil, fmt.Errorf("json: %w", err)
		}
		return arr, nil
	}

	// CSV path
	reader := csv.NewReader(strings.NewReader(trimmed))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv: %w", err)
	}
	if len(records) > 500 {
		return nil, telemetry.ErrBatchTooLarge
	}
	var cmds []telemetry.RegisterDeviceCommand
	for _, rec := range records {
		if len(rec) < 1 || rec[0] == "" {
			continue
		}
		cmd := telemetry.RegisterDeviceCommand{IMEI: rec[0]}
		if len(rec) > 1 && rec[1] != "" {
			cmd.SerialNumber = &rec[1]
		}
		if len(rec) > 2 && rec[2] != "" {
			cmd.DeviceType = rec[2]
		}
		if len(rec) > 3 && rec[3] != "" {
			cmd.VehicleID = &rec[3]
		}
		cmds = append(cmds, cmd)
	}
	return cmds, nil
}
