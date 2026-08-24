package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/alerts/repository"
	"transport-app/internal/apperr"
	"transport-app/internal/auth"
	"transport-app/internal/cache"
	"transport-app/internal/eta"
	"transport-app/internal/httpx"
	intEWB "transport-app/internal/integration/ewaybill"
	"transport-app/internal/service"
	"transport-app/internal/shared"

	"github.com/google/uuid"
)

// ConsoleHandlers serves the owner Command Center (Spec 22 §4.1):
// ranked alert inbox (S1), money strip (S2), fleet strip + context
// panel (S3) and inline panel actions (S4).
type ConsoleHandlers struct {
	app        *App
	repo       repository.AlertRepository
	pnl        *service.PNLService
	db         *sql.DB
	eta        *eta.EtaService
	fleetCache cache.Cache
	ewbClient  intEWB.Client
	ewbExtend  bool // EWB_EXTEND_ENABLED: call the real provider adapter
}

func NewConsoleHandlers(app *App, repo repository.AlertRepository, pnl *service.PNLService, db *sql.DB, etaSvc *eta.EtaService, fleetCache cache.Cache) *ConsoleHandlers {
	if fleetCache == nil {
		fleetCache = cache.Noop{}
	}
	return &ConsoleHandlers{app: app, repo: repo, pnl: pnl, db: db, eta: etaSvc, fleetCache: fleetCache}
}

// WithEwayBillAdapter attaches the integration client used by the inline
// extend action; only called when EWB_EXTEND_ENABLED is true.
func (h *ConsoleHandlers) WithEwayBillAdapter(client intEWB.Client, extendEnabled bool) *ConsoleHandlers {
	h.ewbClient = client
	h.ewbExtend = extendEnabled
	return h
}

// Page handles GET /console.
func (h *ConsoleHandlers) Page(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	alerts, err := h.repo.ListInbox(r.Context(), tenantID, "open", 50)
	if err != nil {
		h.app.renderError(w, http.StatusInternalServerError, "Console unavailable", "Could not load the alert inbox.", nil)
		return
	}
	user, _ := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	// Spec 22 §10-S12 — console-open usage event (best-effort analytics).
	if h.app != nil && h.app.Founder != nil && h.app.Founder.KPIs != nil && user != nil {
		h.app.Founder.KPIs.RecordConsoleUsage(r.Context(), tenantID, user.UserID, "console_open")
	}
	inbox := make([]map[string]any, 0, len(alerts))
	for _, a := range alerts {
		item := map[string]any{
			"ID":           a.ID,
			"Title":        a.Title,
			"Severity":     a.Severity,
			"SeverityRank": a.SeverityRank,
			"MoneyAtRisk":  a.MoneyAtRisk,
			"CreatedAt":    a.CreatedAt.Format("02 Jan 15:04"),
			"AckStatus":    a.AckStatus,
			"Occurrences":  a.Occurrences,
		}
		if a.EntityType != nil && *a.EntityType == "vehicle" && a.EntityID != nil {
			item["VehicleID"] = *a.EntityID
		}
		inbox = append(inbox, item)
	}

	// Money strip is best-effort on the page render: without PNL service or
	// on error the partial renders placeholders instead of failing the page.
	var strip *service.MoneyStrip
	if h.pnl != nil {
		strip, _ = h.pnl.GetMoneyStrip(r.Context(), tenantID, time.Now())
	}

	h.app.renderPage(w, r, "console.html", PageData{
		Title: "Command Center",
		User:  user,
		Extra: map[string]interface{}{
			"InboxAlerts": inbox,
			"MoneyStrip":  strip,
		},
	})
}

// MoneyStrip handles GET /api/dashboard/money-strip (Spec 22 §2.2).
func (h *ConsoleHandlers) MoneyStrip(w http.ResponseWriter, r *http.Request) {
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	if h.pnl == nil || h.repo == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	strip, err := h.pnl.GetMoneyStrip(r.Context(), tenantID, time.Now())
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	open, critical, err := h.repo.InboxCounts(r.Context(), tenantID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"date":        strip.Date,
		"revenue":     strip.Revenue,
		"spent":       strip.Spent,
		"receivables": strip.Receivables,
		"open_alerts": open,
		"critical":    critical,
	})
}

// ── S3: Fleet strip + vehicle context panel (Spec 22 §2.3, §4.1) ────────

// fleetStripItem is one color-coded card in the fleet strip.
type fleetStripItem struct {
	ID        string   `json:"id"`
	Number    string   `json:"number"`
	Status    string   `json:"status"`
	Lat       *float64 `json:"lat,omitempty"`
	Lng       *float64 `json:"lng,omitempty"`
	SpeedKmph *float64 `json:"speed_kmph,omitempty"`
	At        string   `json:"at,omitempty"`
}

// Fleet handles GET /api/fleet — every vehicle with its latest position
// (single composite query; no N+1).
func (h *ConsoleHandlers) Fleet(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	rows, err := h.db.QueryContext(r.Context(), `
		SELECT v.id, v.vehicle_number, v.status,
		       p.latitude, p.longitude, p.speed, p.device_time
		FROM vehicles v
		LEFT JOIN vehicle_latest_position p ON p.vehicle_id = v.id
		WHERE v.tenant_id = ?
		ORDER BY v.vehicle_number
		LIMIT 500`, tenantID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	defer func() { _ = rows.Close() }()

	items := []fleetStripItem{}
	for rows.Next() {
		var it fleetStripItem
		var lat, lng, speed sql.NullFloat64
		var at sql.NullTime
		if err := rows.Scan(&it.ID, &it.Number, &it.Status, &lat, &lng, &speed, &at); err != nil {
			httpx.Error(w, r, err)
			return
		}
		if lat.Valid && lng.Valid {
			it.Lat, it.Lng = &lat.Float64, &lng.Float64
		}
		if speed.Valid {
			it.SpeedKmph = &speed.Float64
		}
		if at.Valid {
			it.At = at.Time.UTC().Format(time.RFC3339)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"vehicles": items})
}

// contextResponse is the Spec 22 §2.3 wire shape.
type contextResponse struct {
	Vehicle        vehicleCtx    `json:"vehicle"`
	Position       *positionCtx  `json:"position,omitempty"`
	Trip           *tripCtx      `json:"trip,omitempty"`
	Driver         *driverCtx    `json:"driver,omitempty"`
	PnlKmToday     float64       `json:"pnl_km_today"`
	KharchaPending []kharchaItem `json:"kharcha_pending"`
	EwayBill       *ewayCtx      `json:"eway_bill,omitempty"`
	FastagBalance  *float64      `json:"fastag_balance"`
	DocsExpiring   []docItem     `json:"docs_expiring"`
}

type vehicleCtx struct {
	ID     string `json:"id"`
	Number string `json:"number"`
	Status string `json:"status"`
}

type positionCtx struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	SpeedKmph float64 `json:"speed_kmph"`
	At        string  `json:"at"`
}

type tripCtx struct {
	ID    string     `json:"id"`
	Route string     `json:"route"`
	EtaAt *time.Time `json:"eta_at,omitempty"`
}

type driverCtx struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Phone *string `json:"phone,omitempty"`
}

type kharchaItem struct {
	ID       string  `json:"id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
}

type ewayCtx struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

type docItem struct {
	Kind      string `json:"kind"`
	ExpiresOn string `json:"expires_on"`
}

const contextCacheTTL = 60 * time.Second

// VehicleContext handles GET /api/fleet/{vehicleId}/context. Every section
// comes from its real tables via single tenant-scoped queries (no N+1),
// cached for 60s per (tenant, vehicle). Unknown vehicle for the tenant → 404.
func (h *ConsoleHandlers) VehicleContext(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	tenantID := string(shared.TenantIDFromContext(r.Context()))
	vehicleID := chi.URLParam(r, "vehicleId")
	cacheKey := "fleetctx:" + tenantID + ":" + vehicleID

	if blob, ok, _ := h.fleetCache.Get(r.Context(), cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(blob)
		return
	}

	ctx := r.Context()
	resp := contextResponse{KharchaPending: []kharchaItem{}, DocsExpiring: []docItem{}}

	// 1. Vehicle (existence + tenancy gate for everything below).
	err := h.db.QueryRowContext(ctx, `
		SELECT id, vehicle_number, status FROM vehicles WHERE id = ? AND tenant_id = ?`,
		vehicleID, tenantID).Scan(&resp.Vehicle.ID, &resp.Vehicle.Number, &resp.Vehicle.Status)
	if err == sql.ErrNoRows {
		httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
		return
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	// 2. Latest position.
	var lat, lng, speed sql.NullFloat64
	var deviceTime sql.NullTime
	posErr := h.db.QueryRowContext(ctx, `
		SELECT latitude, longitude, speed, device_time
		FROM vehicle_latest_position WHERE vehicle_id = ? AND tenant_id = ?`,
		vehicleID, tenantID).Scan(&lat, &lng, &speed, &deviceTime)
	if posErr == nil && lat.Valid && lng.Valid {
		p := &positionCtx{Lat: lat.Float64, Lng: lng.Float64}
		if speed.Valid {
			p.SpeedKmph = speed.Float64
		}
		if deviceTime.Valid {
			p.At = deviceTime.Time.UTC().Format(time.RFC3339)
		}
		resp.Position = p
	}

	// 3. Active trip + route.
	var tripID, driverID sql.NullString
	var routeLabel string
	var distance float64
	tripErr := h.db.QueryRowContext(ctx, `
		SELECT t.id, t.driver_id, COALESCE(r.source,'') || '→' || COALESCE(r.destination,''), COALESCE(r.distance,0)
		FROM trips t JOIN routes r ON r.id = t.route_id
		WHERE t.vehicle_id = ? AND t.tenant_id = ?
		  AND t.status IN ('assigned','started','reached_pickup','in_transit')
		ORDER BY t.departure_time DESC LIMIT 1`,
		vehicleID, tenantID).Scan(&tripID, &driverID, &routeLabel, &distance)
	hasTrip := tripErr == nil && tripID.Valid
	if hasTrip {
		t := &tripCtx{ID: tripID.String, Route: routeLabel}
		if h.eta != nil {
			if res, etaErr := h.eta.Calculate(ctx, tripID.String); etaErr == nil {
				arrival := res.ArrivalAt
				t.EtaAt = &arrival
			}
		}
		resp.Trip = t
	}

	// 4. Driver on the active trip.
	if hasTrip && driverID.Valid && driverID.String != "" {
		var d driverCtx
		var phone sql.NullString
		dErr := h.db.QueryRowContext(ctx, `
			SELECT id,
			       COALESCE(first_name,'') || ' ' || COALESCE(last_name,''),
			       phone
			FROM drivers WHERE id = ?`, driverID.String).Scan(&d.ID, &d.Name, &phone)
		if dErr == nil {
			if phone.Valid && phone.String != "" {
				d.Phone = &phone.String
			}
			resp.Driver = &d
		}
	}

	// 5. pnl per km today: live margin of the active trip over route length.
	if hasTrip && distance > 0 {
		var margin float64
		if mErr := h.db.QueryRowContext(ctx,
			`SELECT estimated_margin FROM trips WHERE id = ?`, tripID.String).Scan(&margin); mErr == nil {
			resp.PnlKmToday = margin / distance
		}
	}

	// 6. Pending kharcha on this vehicle's trips.
	kRows, kErr := h.db.QueryContext(ctx, `
		SELECT de.id, de.amount, COALESCE(de.category, de.expense_type, '')
		FROM driver_expenses de JOIN trips t ON de.trip_id = t.id
		WHERE t.vehicle_id = ? AND t.tenant_id = ? AND de.status = 'pending'
		ORDER BY de.created_at DESC LIMIT 10`, vehicleID, tenantID)
	if kErr == nil {
		defer func() { _ = kRows.Close() }()
		for kRows.Next() {
			var it kharchaItem
			if err := kRows.Scan(&it.ID, &it.Amount, &it.Category); err == nil {
				resp.KharchaPending = append(resp.KharchaPending, it)
			}
		}
	}

	// 7. Active e-way bill on the active trip.
	if hasTrip {
		var e ewayCtx
		var validUntil time.Time
		eErr := h.db.QueryRowContext(ctx, `
			SELECT eb.id, eb.valid_until FROM eway_bills eb
			WHERE eb.trip_id = ? AND eb.status = 'active'
			ORDER BY eb.valid_until ASC LIMIT 1`, tripID.String).
			Scan(&e.ID, &validUntil)
		if eErr == nil {
			e.ExpiresAt = validUntil.UTC().Format(time.RFC3339)
			resp.EwayBill = &e
		}
	}

	// 8. FASTag balance (sum across tags fitted to the vehicle).
	var balance float64
	bErr := h.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(balance),0) FROM fastag_tags
		WHERE vehicle_id = ? AND tenant_id = ?`, vehicleID, tenantID).Scan(&balance)
	if bErr == nil {
		resp.FastagBalance = &balance
	}

	// 9. Documents expiring within 30 days.
	dRows, dErr := h.db.QueryContext(ctx, `
		SELECT doc_type, expiry_date FROM vehicle_documents
		WHERE vehicle_id = ? AND expiry_date IS NOT NULL
		  AND expiry_date <= DATE('now', '+30 days')
		ORDER BY expiry_date ASC LIMIT 5`, vehicleID)
	if dErr == nil {
		defer func() { _ = dRows.Close() }()
		for dRows.Next() {
			var it docItem
			var expiry sql.NullTime
			if err := dRows.Scan(&it.Kind, &expiry); err == nil && expiry.Valid {
				it.ExpiresOn = expiry.Time.Format("2006-01-02")
				resp.DocsExpiring = append(resp.DocsExpiring, it)
			}
		}
	}

	blob, err := json.Marshal(resp)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	_ = h.fleetCache.Set(r.Context(), cacheKey, blob, contextCacheTTL)

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(blob)
}

// ── S4: Inline panel actions (Spec 22 §2.3, §10 Step 4) ─────────────────

type extendBody struct {
	ValidUptoHours int `json:"valid_upto_hours"`
}

// ExtendEwayBill handles POST /api/ewaybill/{id}/extend
// {"valid_upto_hours":4} → {"ok":true,"new_expiry":"..."}.
// Default behavior is the repo-wide mock contract: shift expiry locally.
// With EWB_EXTEND_ENABLED=true the provider adapter is consulted first and
// its returned validity wins. Every call writes an eway_bill_events row and
// an audit_logs entry (Spec 22 Step 4 exit gate).
func (h *ConsoleHandlers) ExtendEwayBill(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		httpx.Error(w, r, apperr.New(apperr.CodeInternal))
		return
	}
	var body extendBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil || body.ValidUptoHours <= 0 || body.ValidUptoHours > 24 {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "valid_upto_hours must be between 1 and 24"})
		return
	}

	ctx := r.Context()
	tenantID := string(shared.TenantIDFromContext(ctx))
	ewbID := chi.URLParam(r, "id")

	var tripID sql.NullString
	var validUntil time.Time
	err := h.db.QueryRowContext(ctx, `
		SELECT e.trip_id, e.valid_until
		FROM eway_bills e JOIN trips t ON e.trip_id = t.id
		WHERE e.id = ? AND t.tenant_id = ?`, ewbID, tenantID).
		Scan(&tripID, &validUntil)
	if err == sql.ErrNoRows {
		httpx.Error(w, r, apperr.New(apperr.CodeNotFound))
		return
	}
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	newExpiry := validUntil.Add(time.Duration(body.ValidUptoHours) * time.Hour)
	if h.ewbExtend && h.ewbClient != nil {
		var ewbNumber string
		if nErr := h.db.QueryRowContext(ctx,
			`SELECT ewb_number FROM eway_bills WHERE id = ?`, ewbID).Scan(&ewbNumber); nErr == nil {
			if resp, extErr := h.ewbClient.Extend(ctx, ewbNumber, intEWB.ExtendRequest{
				EwbNumber: ewbNumber,
				Reason:    "console_inline_extend",
			}); extErr == nil && !resp.ValidUpto.IsZero() {
				newExpiry = resp.ValidUpto
			}
		}
	}

	if _, uErr := h.db.ExecContext(ctx,
		`UPDATE eway_bills SET valid_until = ? WHERE id = ?`,
		newExpiry, ewbID); uErr != nil {
		httpx.Error(w, r, uErr)
		return
	}
	if _, eErr := h.db.ExecContext(ctx, `
		INSERT INTO eway_bill_events (id, ewb_number, trip_id, event_type, payload, created_by)
		SELECT ?, ewb_number, trip_id, 'EXTENDED', ?, ? FROM eway_bills WHERE id = ?`,
		uuid.NewString(), fmt.Sprintf(`{"hours":%d}`, body.ValidUptoHours), contextUserID(r), ewbID); eErr != nil {
		slog.Warn("eway_bill_events insert failed", "ewb", ewbID, "error", eErr)
	}

	writeAuditLog(r, h.db, "ewaybill.extend", "eway_bills", ewbID, map[string]any{
		"new_expiry":       newExpiry.UTC().Format(time.RFC3339),
		"valid_upto_hours": body.ValidUptoHours,
	})

	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"new_expiry": newExpiry.UTC().Format(time.RFC3339),
	})
}

// writeAuditLog records one audit trail row for a console action; failures
// are logged but never fail the action itself.
func writeAuditLog(r *http.Request, db *sql.DB, action, table, recordID string, newValues any) {
	blob, err := json.Marshal(newValues)
	if err != nil {
		return
	}
	_, _ = db.ExecContext(r.Context(), `
		INSERT INTO audit_logs (id, user_id, action, table_name, record_id, new_values, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		uuid.NewString(), contextUserID(r), action, table, recordID, string(blob))
}
