package safety

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	founderalerts "transport-app/internal/founder/alerts"
	"transport-app/internal/fuel"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/shared/ports"
)

type alertSaver interface {
	SaveEvents(ctx context.Context, aggregateID, aggregateType string, events []any) error
}

type Engine struct {
	db     *sql.DB
	uow    ports.UnitOfWork
	config *fuel.ConfigReader
	alerts alertSaver
	idGen  ports.IDGenerator
	log    *slog.Logger

	tenantID shared.TenantID
	loc      *time.Location

	// featureGate reports whether the scorecard/safety feature is on for an
	// org. Nil means ungated (tests, tooling). Production wires the features
	// registry so one org disabling it stops only its own sweep.
	featureGate func(tenantID string) bool

	mu    sync.Mutex
	state map[string]*vehicleState
	now   func() time.Time

	behaviourHook func(ctx context.Context, driverID string)
}

func NewEngine(db *sql.DB, uow ports.UnitOfWork, cfg *fuel.ConfigReader, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		db:       db,
		uow:      uow,
		config:   cfg,
		alerts:   outbox.NewOutboxWriter(db),
		idGen:    id.NewUUIDGenerator(),
		log:      log,
		tenantID: shared.DefaultTenant,
		loc:      time.Local,
		state:    make(map[string]*vehicleState),
		now:      time.Now,
	}
}

func (e *Engine) WithBehaviourHook(hook func(ctx context.Context, driverID string)) *Engine {
	if hook != nil {
		e.behaviourHook = hook
	}
	return e
}

func (e *Engine) WithAlertSaver(s alertSaver) *Engine {
	if s != nil {
		e.alerts = s
	}
	return e
}

func (e *Engine) WithLocation(loc *time.Location) *Engine {
	if loc != nil {
		e.loc = loc
	}
	return e
}

func (e *Engine) WithTenantID(tenantID shared.TenantID) *Engine {
	if tenantID != "" {
		e.tenantID = tenantID
	}
	return e
}

// WithFeatureGate scopes each sweep to orgs with the feature on.
// Chain after construction; safe to omit (ungated).
func (e *Engine) WithFeatureGate(gate func(tenantID string) bool) *Engine {
	e.featureGate = gate
	return e
}

// resolveVehicleTenants attributes vehicles to their orgs in one query.
// Unknown vehicles resolve to "" and are skipped by Tick.
func (e *Engine) resolveVehicleTenants(ctx context.Context, vids []string) map[string]string {
	out := make(map[string]string, len(vids))
	if len(vids) == 0 {
		return out
	}
	placeholders := strings.Repeat("?,", len(vids))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]any, len(vids))
	for i, id := range vids {
		args[i] = id
	}
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, tenant_id FROM vehicles WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var vehID, tenant string
		if err := rows.Scan(&vehID, &tenant); err == nil {
			out[vehID] = tenant
		}
	}
	return out
}

func (e *Engine) Run(ctx context.Context) {
	policy, err := LoadSafetyPolicy(ctx, string(e.tenantID), e.config)
	interval := DefaultTickInterval
	if err == nil && policy.GapTolerance > 0 {
		interval = DefaultTickInterval
	}
	e.log.Info("safety engine started", "interval", interval.String())
	if _, err := e.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		e.log.Error("safety engine initial sweep failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			e.log.Info("safety engine stopped")
			return
		case <-ticker.C:
			if _, err := e.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				e.log.Error("safety engine sweep failed", "error", err)
			}
		}
	}
}

func (e *Engine) Tick(ctx context.Context) (int, error) {
	// Discovery uses the zero policy (24h fallback inside activeVehicles);
	// per-org policies load below once the org is known.
	vehicles, err := e.activeVehicles(ctx, SafetyPolicy{})
	if err != nil {
		return 0, fmt.Errorf("safety: active vehicles: %w", err)
	}
	tenantByVehicle := e.resolveVehicleTenants(ctx, vehicles)
	policies := map[string]SafetyPolicy{}

	total := 0
	for _, vid := range vehicles {
		tenant := tenantByVehicle[vid]
		if tenant == "" {
			e.log.Warn("safety engine: vehicle skipped (unknown org)", "vehicle", vid)
			continue
		}
		if e.featureGate != nil && !e.featureGate(tenant) {
			continue
		}
		policy, ok := policies[tenant]
		if !ok {
			var err error
			policy, err = LoadSafetyPolicy(ctx, tenant, e.config)
			if err != nil {
				return total, fmt.Errorf("safety: load policy: %w", err)
			}
			policies[tenant] = policy
		}
		n, err := e.processVehicle(ctx, vid, tenant, policy)
		if err != nil {
			return total, fmt.Errorf("safety: vehicle %s: %w", vid, err)
		}
		total += n
	}
	return total, nil
}

func (e *Engine) activeVehicles(ctx context.Context, policy SafetyPolicy) ([]string, error) {
	cutoff := e.now().Add(-policy.GapTolerance)
	if policy.GapTolerance <= 0 {
		cutoff = e.now().Add(-24 * time.Hour)
	}
	rows, err := e.db.QueryContext(ctx,
		`SELECT DISTINCT vehicle_id FROM telemetry_snapshots
		 WHERE vehicle_id IS NOT NULL AND vehicle_id != '' AND timestamp > ?
		 ORDER BY vehicle_id LIMIT ?`, timeStr(cutoff), defaultSnapshotsPerSweep)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (e *Engine) processVehicle(ctx context.Context, vehicleID, tenant string, policy SafetyPolicy) (int, error) {
	e.mu.Lock()
	st, ok := e.state[vehicleID]
	if !ok {
		st = newState()
		e.state[vehicleID] = st
	}
	e.mu.Unlock()

	var frames []snapshot
	var err error
	if st.watermark.IsZero() {
		frames, err = e.loadRecent(ctx, vehicleID, defaultWarmupReplayFrames)
		if err != nil {
			return 0, err
		}
		for _, f := range frames {
			detect(policy, st, f, e.loc)
		}
		return 0, nil
	}

	frames, err = e.loadAfter(ctx, vehicleID, st.watermark)
	if err != nil {
		return 0, err
	}

	emitted := 0
	for _, f := range frames {
		if f.driverID == "" {
			f.driverID = e.resolveDriver(ctx, vehicleID, f.tripID)
		}
		if f.driverID == "" && st.driverID != "" {
			f.driverID = st.driverID
		}
		events := detect(policy, st, f, e.loc)
		if len(events) == 0 {
			continue
		}
		attributed := make([]detectedEvent, 0, len(events))
		for _, ev := range events {
			if f.driverID == "" {
				e.log.Warn("safety engine: event skipped, unknown driver",
					"vehicle", vehicleID, "event_type", ev.eventType)
				continue
			}
			attributed = append(attributed, ev)
		}
		if len(attributed) == 0 {
			continue
		}
		persistedCount, err := e.persist(ctx, f, attributed, tenant)
		if err != nil {
			return emitted, err
		}
		emitted += persistedCount
		if persistedCount > 0 && e.behaviourHook != nil {
			e.behaviourHook(ctx, f.driverID)
		}
	}
	return emitted, nil
}

// ProcessSnapshotsDirect allows direct in-memory / event-driven evaluation of snapshots.
func (e *Engine) ProcessSnapshotsDirect(ctx context.Context, policy SafetyPolicy, vehicleID string, frames []snapshot) (int, error) {
	tenant := e.resolveVehicleTenants(ctx, []string{vehicleID})[vehicleID]
	e.mu.Lock()
	st, ok := e.state[vehicleID]
	if !ok {
		st = newState()
		e.state[vehicleID] = st
	}
	e.mu.Unlock()

	emitted := 0
	for _, f := range frames {
		if f.driverID == "" && st.driverID != "" {
			f.driverID = st.driverID
		}
		events := detect(policy, st, f, e.loc)
		if len(events) == 0 {
			continue
		}
		attributed := make([]detectedEvent, 0, len(events))
		for _, ev := range events {
			if f.driverID == "" {
				continue
			}
			attributed = append(attributed, ev)
		}
		if len(attributed) == 0 {
			continue
		}
		persistedCount, err := e.persist(ctx, f, attributed, tenant)
		if err != nil {
			return emitted, err
		}
		emitted += persistedCount
		if persistedCount > 0 && e.behaviourHook != nil {
			e.behaviourHook(ctx, f.driverID)
		}
	}
	return emitted, nil
}

func (e *Engine) persist(ctx context.Context, s snapshot, events []detectedEvent, tenant string) (int, error) {
	persistedCount := 0
	err := e.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		db := txOrDB(txCtx, e.db)
		alerts := make([]any, 0)
		for _, ev := range events {
			// Deterministic idempotency identity: tenant + vehicle + event_type + timestamp
			eventID := fmt.Sprintf("dbe_%s_%s_%d", s.vehicleID, ev.eventType, ev.occurredAt.Unix())
			res, err := db.ExecContext(txCtx,
				`INSERT OR IGNORE INTO driver_behaviour_events
				    (id, driver_id, trip_id, vehicle_id, event_type, severity, weight, metadata, occurred_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				eventID, s.driverID, strOrNull(s.tripID), s.vehicleID,
				ev.eventType, ev.severity, ev.weight, ev.metadata, timeStr(ev.occurredAt))
			if err != nil {
				return err
			}
			rowsAffected, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if rowsAffected > 0 {
				persistedCount++
				alerts = append(alerts, e.buildAlert(s, ev, tenant))
			}
		}
		if len(alerts) > 0 {
			return e.alerts.SaveEvents(txCtx, s.vehicleID, "Vehicle", alerts)
		}
		return nil
	})
	return persistedCount, err
}

func (e *Engine) buildAlert(s snapshot, ev detectedEvent, tenant string) founderalerts.AlertEvent {
	var meta map[string]interface{}
	_ = json.Unmarshal([]byte(ev.metadata), &meta)
	title := alertTitle(ev.eventType)
	summary := fmt.Sprintf("%s — vehicle %s, driver %s", title, s.vehicleID, s.driverID)
	if meta != nil {
		b, _ := json.Marshal(meta)
		summary = fmt.Sprintf("%s (%s)", summary, string(b))
	}
	return founderalerts.AlertEvent{
		ID:        e.idGen.GenerateUUID(),
		Category:  founderalerts.CategorySafety,
		Priority:  priorityFor(ev.severity),
		Title:     title,
		Summary:   summary,
		CompanyID: tenant,
		Metadata: map[string]interface{}{
			"vehicle_id": s.vehicleID,
			"trip_id":    s.tripID,
			"driver_id":  s.driverID,
			"event_type": ev.eventType,
			"scorecard":  true,
		},
		Timestamp: ev.occurredAt,
	}
}

func priorityFor(severity string) founderalerts.Priority {
	switch severity {
	case "high":
		return founderalerts.PriorityHigh
	case "medium":
		return founderalerts.PriorityMedium
	default:
		return founderalerts.PriorityLow
	}
}

func alertTitle(eventType string) string {
	switch eventType {
	case EventSpeeding:
		return "Speeding detected"
	case EventHarshBraking:
		return "Harsh braking detected"
	case EventHarshAccel:
		return "Harsh acceleration detected"
	case EventIdling:
		return "Excessive idling detected"
	case EventNightDriving:
		return "Night driving detected"
	default:
		return eventType
	}
}

func (e *Engine) loadRecent(ctx context.Context, vehicleID string, n int) ([]snapshot, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT s.id, COALESCE(s.trip_id,''), s.vehicle_id, COALESCE(t.driver_id,''),
		        s.timestamp, COALESCE(s.speed,0), COALESCE(s.latitude, 0), COALESCE(s.longitude, 0), s.ignition
		 FROM telemetry_snapshots s
		 LEFT JOIN trips t ON s.trip_id = t.id
		 WHERE s.vehicle_id = ?
		 ORDER BY s.timestamp DESC
		 LIMIT ?`, vehicleID, n)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out, err := scanSnapshots(rows)
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (e *Engine) loadAfter(ctx context.Context, vehicleID string, after time.Time) ([]snapshot, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT s.id, COALESCE(s.trip_id,''), s.vehicle_id, COALESCE(t.driver_id,''),
		        s.timestamp, COALESCE(s.speed,0), COALESCE(s.latitude, 0), COALESCE(s.longitude, 0), s.ignition
		 FROM telemetry_snapshots s
		 LEFT JOIN trips t ON s.trip_id = t.id
		 WHERE s.vehicle_id = ? AND s.timestamp > ?
		 ORDER BY s.timestamp ASC
		 LIMIT ?`, vehicleID, timeStr(after), defaultSnapshotsPerSweep)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanSnapshots(rows)
}

func scanSnapshots(rows *sql.Rows) ([]snapshot, error) {
	var out []snapshot
	for rows.Next() {
		var s snapshot
		var ign sql.NullBool
		if err := rows.Scan(&s.id, &s.tripID, &s.vehicleID, &s.driverID,
			&s.ts, &s.speed, &s.lat, &s.lng, &ign); err != nil {
			return nil, err
		}
		if ign.Valid {
			v := ign.Bool
			s.ignition = &v
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (e *Engine) resolveDriver(ctx context.Context, vehicleID, tripID string) string {
	if tripID != "" {
		if d := e.tripDriver(ctx, tripID); d != "" {
			return d
		}
	}
	var driverID string
	if err := e.db.QueryRowContext(ctx,
		`SELECT COALESCE(driver_id,'') FROM trips WHERE vehicle_id = ? AND status IN ('in_transit', 'dispatched', 'active', 'scheduled') ORDER BY departure_time DESC LIMIT 1`, vehicleID).Scan(&driverID); err == nil && driverID != "" {
		return driverID
	}
	return ""
}

func (e *Engine) tripDriver(ctx context.Context, tripID string) string {
	if tripID == "" {
		return ""
	}
	var driverID string
	if err := e.db.QueryRowContext(ctx,
		`SELECT COALESCE(driver_id,'') FROM trips WHERE id = ?`, tripID).Scan(&driverID); err != nil {
		return ""
	}
	return driverID
}

func txOrDB(ctx context.Context, db *sql.DB) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return db
}

func timeStr(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func strOrNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
