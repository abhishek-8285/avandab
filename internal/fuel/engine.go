package fuel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"transport-app/internal/domain/trip"
	"transport-app/internal/founder/alerts"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/shared/ports"
)

// snapshotRow is the slice of telemetry_snapshots the engine consumes.
type snapshotRow struct {
	id        string
	tripID    string
	vehicleID string
	driverID  string
	ts        time.Time
	speed     float64
	fuelLevel float64
	odometer  float64
}

// vehicleFuelState is the per-vehicle in-memory state (Spec 03 §2.1).
type vehicleFuelState struct {
	vehicleID    string
	tenantID     string
	sensorFitted bool
	tankCapacity float64
	unit         string // "percent" (default) or "litres"

	window        []float64 // rolling smoothed readings, in litres
	pendingSpike  float64   // spike held for one more reading; NaN = none
	lastLevel     float64   // last smoothed level (litres); NaN before first reading
	baselineLevel float64   // level when the current movement started
	lastOdometer  float64
	lastTs        time.Time
	refillPending float64 // litres since last claim boundary / trip end

	stopStart  time.Time // long-stop (siphon) tracking; zero = not stopped
	tripID     string
	driverID   string
	tripActive bool // last known trip status was active (Spec 03 §2.5)
}

// engineConfig is a snapshot of all thresholds for one processing run.
type engineConfig struct {
	medianWindow      int
	spikeDeviationPct float64
	noiseFloorPct     float64
	refillThresholdL  float64
	theftThresholdL   float64
	siphonThresholdL  float64
	siphonStop        time.Duration
	stopSpeedKmh      float64
	odoToleranceKm    float64
	abnormalLPerKm    float64
	abnormalMarginPct float64
	levelUnit         string
	gapTolerance      time.Duration
	weightTheft       float64
	weightOdoRollback float64
}

// behaviourEvent is a scorecard input row written alongside a fuel event.
type behaviourEvent struct {
	driverID  string
	eventType string // fuel_theft_suspicion | odometer_rollback
	severity  string // high
	weight    float64
}

// fuelEvent is one durable anomaly/refill record to persist.
type fuelEvent struct {
	eventType  string
	estimated  float64
	confidence float64
	before     float64
	after      float64
	odoBefore  float64
	severity   string // alert priority mapping
	behaviour  *behaviourEvent
}

// alertSaver writes alert events to the outbox. The real implementation is
// outbox.OutboxWriter; the interface exists so tests can force failures for
// UoW atomicity verification (Spec 03 §2.9 checklist).
type alertSaver interface {
	SaveEvents(ctx context.Context, aggregateID, aggregateType string, events []any) error
}

// FuelEngine is the anomaly detection background loop (Spec 03 §1.2, §2).
// It is single-instance by design — see the package godoc.
type FuelEngine struct {
	db     *sql.DB
	uow    ports.UnitOfWork
	config *ConfigReader
	alerts alertSaver
	idGen  ports.IDGenerator
	log    *slog.Logger

	tenantID shared.TenantID

	// featureGate reports whether the fuel_audit feature is on for an org.
	// Nil means ungated (tests, tooling). Production wires the features
	// registry so one org disabling fuel_audit stops only its own sweep.
	featureGate func(tenantID string) bool

	mu    sync.Mutex
	state map[string]*vehicleFuelState // keyed by vehicle_id
	now   func() time.Time

	// behaviourHook runs after a sweep writes driver_behaviour_events rows,
	// one call per affected driver. The scorecard service (Spec 03 §4.3
	// incremental trigger) is wired here from main.go — the engine stays
	// free of the service import (would be a cycle).
	behaviourHook func(ctx context.Context, driverID string)
	siphonHook    func(ctx context.Context, vehicleID, tripID, driverID string, drop float64, stopMinutes int)
}

// maxSnapshotsPerSweep caps the number of snapshots processed in one tick.
const maxSnapshotsPerSweep = 500

// NewEngine constructs the fuel anomaly engine. `alertsSaver` may be nil, in
// which case an outbox writer over `db` is used (Spec 03 §1.2 rule 3).
func NewEngine(db *sql.DB, uow ports.UnitOfWork, cfg *ConfigReader, log *slog.Logger) *FuelEngine {
	e := &FuelEngine{
		db:       db,
		uow:      uow,
		config:   cfg,
		alerts:   outbox.NewOutboxWriter(db),
		idGen:    id.NewUUIDGenerator(),
		log:      log,
		tenantID: shared.DefaultTenant,
		state:    make(map[string]*vehicleFuelState),
		now:      time.Now,
	}
	return e
}

// WithAlertSaver overrides the outbox writer (tests inject failures here).
func (e *FuelEngine) WithAlertSaver(s alertSaver) *FuelEngine {
	if s != nil {
		e.alerts = s
	}
	return e
}

// WithFeatureGate scopes each sweep to orgs with the feature on.
// Chain after construction; safe to omit (ungated).
func (e *FuelEngine) WithFeatureGate(gate func(tenantID string) bool) *FuelEngine {
	e.featureGate = gate
	return e
}

// resolveVehicleTenants attributes vehicles to their orgs in one query.
// Unknown vehicles resolve to "" and are skipped by Tick.
func (e *FuelEngine) resolveVehicleTenants(ctx context.Context, vids []string) map[string]string {
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

// WithBehaviourHook registers the scorecard incremental recompute hook
// (Spec 03 §4.3). The hook runs AFTER the engine's own UnitOfWork commits,
// so it is safe to open its own transaction.
func (e *FuelEngine) WithBehaviourHook(hook func(ctx context.Context, driverID string)) *FuelEngine {
	if hook != nil {
		e.behaviourHook = hook
	}
	return e
}

// WithSiphonHook registers a hook invoked when a siphon_confirmed event is detected (Spec 16 §4).
func (e *FuelEngine) WithSiphonHook(hook func(ctx context.Context, vehicleID, tripID, driverID string, drop float64, stopMinutes int)) *FuelEngine {
	if hook != nil {
		e.siphonHook = hook
	}
	return e
}

// Run blocks until ctx is cancelled, sweeping on fuel.tick_interval_seconds.
func (e *FuelEngine) Run(ctx context.Context) {
	interval, err := e.config.GetDurationSeconds(ctx, string(e.tenantID),
		ConfigTickIntervalSeconds, DefaultTickInterval)
	if err != nil {
		interval = DefaultTickInterval
	}

	e.log.Info("fuel engine started", "interval", interval.String())
	if _, err := e.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		e.log.Error("fuel engine initial sweep failed", "error", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			e.log.Info("fuel engine stopped")
			return
		case <-ticker.C:
			if _, err := e.Tick(ctx); err != nil && !errors.Is(err, context.Canceled) {
				e.log.Error("fuel engine sweep failed", "error", err)
			}
		}
	}
}

// Tick processes one sweep: discovers active vehicles, attributes each to
// its org, then runs the detection pipeline per new snapshot. Orgs with the
// feature off and unknown vehicles are skipped, never processed as another
// org. Returns the number of snapshots consumed.
func (e *FuelEngine) Tick(ctx context.Context) (int, error) {
	// Discovery uses the compiled default gap window (a work-volume knob, not
	// correctness); per-org thresholds load below once the org is known.
	vehicles, err := e.activeVehicles(ctx, engineConfig{gapTolerance: DefaultGapTolerance})
	if err != nil {
		return 0, fmt.Errorf("fuel: active vehicles: %w", err)
	}
	tenantByVehicle := e.resolveVehicleTenants(ctx, vehicles)
	cfgs := map[string]engineConfig{}

	total := 0
	for _, vid := range vehicles {
		tenant := tenantByVehicle[vid]
		if tenant == "" {
			e.log.Warn("fuel engine: vehicle skipped (unknown org)", "vehicle", vid)
			continue
		}
		if e.featureGate != nil && !e.featureGate(tenant) {
			continue
		}
		cfg, ok := cfgs[tenant]
		if !ok {
			var err error
			cfg, err = e.loadConfig(ctx, tenant)
			if err != nil {
				return total, fmt.Errorf("fuel: load config: %w", err)
			}
			cfgs[tenant] = cfg
		}
		processed, err := e.processVehicle(ctx, vid, tenant, cfg)
		if err != nil {
			// A UoW failure means events were not durably written — abort the
			// sweep so the next tick retries from the same watermark. The
			// per-vehicle in-memory state already advanced, so an aborted
			// sweep may skip a beat; the next tick's loadAfter re-reads from
			// the DB and reprocesses from the last committed state.
			return total, fmt.Errorf("fuel: vehicle %s: %w", vid, err)
		}
		total += processed
	}
	return total, nil
}

// loadConfig reads all thresholds from company_config with compiled defaults.
func (e *FuelEngine) loadConfig(ctx context.Context, tenant string) (engineConfig, error) {
	t := tenant
	cfg := engineConfig{
		medianWindow:      DefaultMedianWindow,
		spikeDeviationPct: DefaultSpikeDeviationPct,
		noiseFloorPct:     DefaultNoiseFloorPct,
		refillThresholdL:  DefaultRefillThresholdL,
		theftThresholdL:   DefaultTheftDropThresholdL,
		siphonThresholdL:  DefaultSiphonDropThresholdL,
		siphonStop:        DefaultSiphonStopMinutes,
		stopSpeedKmh:      DefaultStopSpeedKmh,
		odoToleranceKm:    DefaultOdometerToleranceKm,
		abnormalLPerKm:    DefaultAbnormalDrainLPerKm,
		abnormalMarginPct: DefaultAbnormalDrainMargin,
		levelUnit:         "percent",
		gapTolerance:      DefaultGapTolerance,
		weightTheft:       DefaultWeightTheft,
		weightOdoRollback: DefaultWeightOdoRollback,
	}

	if v, err := e.config.GetFloat(ctx, t, ConfigMedianWindow, float64(cfg.medianWindow)); err == nil && v > 0 {
		cfg.medianWindow = int(v)
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigSpikeDeviationPct, cfg.spikeDeviationPct); err == nil && v > 0 {
		cfg.spikeDeviationPct = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigNoiseFloorPct, cfg.noiseFloorPct); err == nil && v >= 0 {
		cfg.noiseFloorPct = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigRefillThresholdL, cfg.refillThresholdL); err == nil && v > 0 {
		cfg.refillThresholdL = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigTheftDropThresholdL, cfg.theftThresholdL); err == nil && v > 0 {
		cfg.theftThresholdL = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigSiphonDropThresholdL, cfg.siphonThresholdL); err == nil && v > 0 {
		cfg.siphonThresholdL = v
	}
	if v, err := e.config.GetDurationMinutes(ctx, t, ConfigSiphonStopMinutes, cfg.siphonStop); err == nil && v > 0 {
		cfg.siphonStop = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigStopSpeedKmh, cfg.stopSpeedKmh); err == nil && v >= 0 {
		cfg.stopSpeedKmh = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigOdometerToleranceKm, cfg.odoToleranceKm); err == nil && v >= 0 {
		cfg.odoToleranceKm = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigAbnormalDrainLPerKm, cfg.abnormalLPerKm); err == nil && v >= 0 {
		cfg.abnormalLPerKm = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigAbnormalDrainMargin, cfg.abnormalMarginPct); err == nil && v >= 0 {
		cfg.abnormalMarginPct = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigWeightTheft, cfg.weightTheft); err == nil && v >= 0 {
		cfg.weightTheft = v
	}
	if v, err := e.config.GetFloat(ctx, t, ConfigWeightOdoRollback, cfg.weightOdoRollback); err == nil && v >= 0 {
		cfg.weightOdoRollback = v
	}
	if v, err := e.config.Get(ctx, t, ConfigLevelUnit); err == nil && v != "" {
		cfg.levelUnit = v
	}
	if v, err := e.config.GetDurationMinutes(ctx, t, ConfigGapToleranceMinutes, cfg.gapTolerance); err == nil && v > 0 {
		cfg.gapTolerance = v
	}
	return cfg, nil
}

// activeVehicles returns vehicle IDs with snapshots within the gap tolerance
// window (Spec 03 §11 item 4: vehicles idle beyond the gap don't need work).
func (e *FuelEngine) activeVehicles(ctx context.Context, cfg engineConfig) ([]string, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT DISTINCT s.vehicle_id
		 FROM telemetry_snapshots s
		 WHERE s.vehicle_id IS NOT NULL AND s.vehicle_id != ''
		   AND s.timestamp > ?
		 ORDER BY s.vehicle_id`,
		timeStr(e.now().Add(-cfg.gapTolerance)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

// processVehicle handles one vehicle: warms state on cache miss, then feeds
// new snapshots through the pipeline.
func (e *FuelEngine) processVehicle(ctx context.Context, vehicleID, tenant string, cfg engineConfig) (int, error) {
	meta, err := e.vehicleMeta(ctx, vehicleID)
	if err != nil {
		return 0, err
	}

	e.mu.Lock()
	st, exists := e.state[vehicleID]
	if !exists {
		st = &vehicleFuelState{
			vehicleID:     vehicleID,
			tenantID:      tenant,
			sensorFitted:  meta.sensorFitted,
			tankCapacity:  meta.tankCapacity,
			unit:          cfg.levelUnit,
			pendingSpike:  math.NaN(),
			lastLevel:     math.NaN(),
			baselineLevel: math.NaN(),
		}
		e.state[vehicleID] = st
	} else {
		st.tenantID = tenant
	}
	e.mu.Unlock()

	if !exists {
		// Warm-up: replay the last median_window snapshots so deltas on the
		// first live reading are meaningful (Spec 03 §2, §13 item 13). Replay
		// runs the full pipeline so anomalies inside the window are detected;
		// after restart the watermark (lastTs) advances past them, so
		// subsequent ticks do not re-process.
		warm, err := e.loadRecent(ctx, vehicleID, cfg.medianWindow)
		if err != nil {
			return 0, err
		}
		for _, s := range warm {
			if err := e.pipeline(ctx, st, s, cfg, true); err != nil {
				return 0, err
			}
		}
		return len(warm), nil
	}

	next, err := e.loadAfter(ctx, vehicleID, st.lastTs)
	if err != nil {
		return 0, err
	}
	for _, s := range next {
		if err := e.pipeline(ctx, st, s, cfg, false); err != nil {
			return 0, err
		}
	}
	return len(next), nil
}

// vehicleMeta loads the fuel-sensor columns added by the geofence spec (00042).
func (e *FuelEngine) vehicleMeta(ctx context.Context, vehicleID string) (vehicleMeta, error) {
	var m vehicleMeta
	var sensor sql.NullInt64
	var cap sql.NullFloat64
	err := e.db.QueryRowContext(ctx,
		`SELECT COALESCE(fuel_sensor_fitted, 0), tank_capacity_litres
		 FROM vehicles WHERE id = ?`, vehicleID).Scan(&sensor, &cap)
	if err != nil {
		return m, fmt.Errorf("fuel: vehicle %s: %w", vehicleID, err)
	}
	m.sensorFitted = sensor.Valid && sensor.Int64 != 0
	if cap.Valid {
		m.tankCapacity = cap.Float64
	}
	return m, nil
}

type vehicleMeta struct {
	sensorFitted bool
	tankCapacity float64
}

// loadRecent returns the last `n` snapshots for a vehicle, oldest first.
func (e *FuelEngine) loadRecent(ctx context.Context, vehicleID string, n int) ([]snapshotRow, error) {
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, COALESCE(trip_id,''), vehicle_id, COALESCE(driver_id,''),
		        timestamp, COALESCE(speed,0), COALESCE(fuel_level,0), COALESCE(odometer,0)
		 FROM telemetry_snapshots
		 WHERE vehicle_id = ?
		 ORDER BY timestamp DESC
		 LIMIT ?`, vehicleID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []snapshotRow
	for rows.Next() {
		var s snapshotRow
		if err := rows.Scan(&s.id, &s.tripID, &s.vehicleID, &s.driverID,
			&s.ts, &s.speed, &s.fuelLevel, &s.odometer); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse: newest first → oldest first for pipeline order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// loadAfter returns snapshots strictly newer than `after` (zero time = none
// pending; warm-up covers the initial window), oldest first.
func (e *FuelEngine) loadAfter(ctx context.Context, vehicleID string, after time.Time) ([]snapshotRow, error) {
	if after.IsZero() {
		return nil, nil
	}
	rows, err := e.db.QueryContext(ctx,
		`SELECT id, COALESCE(trip_id,''), vehicle_id, COALESCE(driver_id,''),
		        timestamp, COALESCE(speed,0), COALESCE(fuel_level,0), COALESCE(odometer,0)
		 FROM telemetry_snapshots
		 WHERE vehicle_id = ? AND timestamp > ?
		 ORDER BY timestamp ASC
		 LIMIT ?`, vehicleID, timeStr(after), maxSnapshotsPerSweep)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []snapshotRow
	for rows.Next() {
		var s snapshotRow
		if err := rows.Scan(&s.id, &s.tripID, &s.vehicleID, &s.driverID,
			&s.ts, &s.speed, &s.fuelLevel, &s.odometer); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// pipeline runs the detection pipeline (Spec 03 §2.2) for one snapshot and
// persists any resulting events atomically with the outbox alert.
func (e *FuelEngine) pipeline(ctx context.Context, st *vehicleFuelState, s snapshotRow, cfg engineConfig, warmup bool) error {
	// Trip status context: distinguishes theft (stationary, active trip) from
	// abnormal drain (in_transit) and detects trip-end resets (Spec 03 §2.4).
	status, tripDriver := e.tripStatus(ctx, s.tripID)
	tripActive := s.tripID == "" || isActiveTripStatus(status)
	if st.tripActive && !tripActive && st.tripID == s.tripID {
		// Trip ended: flush the refill accumulator and reset the baseline
		// (Spec 03 §2.4 — the durable per-trip total is fuel_events rows).
		st.refillPending = 0
		st.baselineLevel = st.lastLevel
	}
	st.tripID = s.tripID
	if s.driverID != "" {
		st.driverID = s.driverID
	} else if tripDriver != "" {
		st.driverID = tripDriver
	}
	st.tripActive = tripActive

	// Gap tolerance: a telemetry gap beyond the limit resets the smoothing
	// window and long-stop tracking (Spec 03 §11 item 4). Must be evaluated
	// against the PREVIOUS timestamp, before the watermark advances.
	if !warmup && !st.lastTs.IsZero() && s.ts.Sub(st.lastTs) > cfg.gapTolerance {
		st.window = nil
		st.baselineLevel = st.lastLevel
		st.stopStart = time.Time{}
	}
	if s.ts.After(st.lastTs) {
		st.lastTs = s.ts
	}

	// Odometer rollback is sensor-independent (Spec 03 §2.2 step 8).
	odoBefore := st.lastOdometer
	if err := e.odoRollback(ctx, st, s, cfg); err != nil {
		return err
	}

	if !st.sensorFitted || st.tankCapacity <= 0 {
		// Sensor missing or capacity unknown: level checks are skipped
		// (Spec 03 §2.2 step 1); odometer-only processing continues.
		return nil
	}

	litres := st.toLitres(clampFuel(s.fuelLevel, st.tankCapacity))

	// First reading: establish the baseline WITHOUT spike holding — there is
	// no history to compare against, so no anomaly is possible (Spec 03 §2.2
	// pipeline entry). The reading joins the window for future medians and
	// seeds the long-stop (siphon) tracker when stationary.
	if math.IsNaN(st.lastLevel) {
		st.window = append(st.window, litres)
		if len(st.window) > cfg.medianWindow {
			st.window = st.window[len(st.window)-cfg.medianWindow:]
		}
		st.lastLevel = litres
		st.baselineLevel = litres
		if math.Abs(s.speed) < cfg.stopSpeedKmh {
			st.stopStart = s.ts
		}
		return nil
	}

	// Median smoothing with spike hold (Spec 03 §2.2 steps 2-3): a single
	// reading deviating beyond spike_deviation_pct of the window median is
	// held for one more reading before the pipeline acts on it.
	if !math.IsNaN(st.pendingSpike) {
		// Previous reading was a spike; this reading confirms or rejects it.
		// Both join the window and the pipeline acts on the resulting median.
		st.window = append(st.window, st.pendingSpike)
		st.pendingSpike = math.NaN()
	} else if len(st.window) > 0 {
		if deviates(litres, median(st.window), cfg.spikeDeviationPct) {
			st.pendingSpike = litres
			return nil
		}
	}

	st.window = append(st.window, litres)
	if len(st.window) > cfg.medianWindow {
		st.window = st.window[len(st.window)-cfg.medianWindow:]
	}
	smoothed := median(st.window)

	delta := smoothed - st.lastLevel
	noise := st.tankCapacity * cfg.noiseFloorPct / 100.0

	events := e.detect(st, s, cfg, smoothed, delta, noise, status, odoBefore)
	st.lastLevel = smoothed
	if len(events) == 0 {
		return nil
	}

	// Multi-table write + outbox alert in one transaction (Spec 03 §1.2 rule 5).
	var behaviourDrivers []string
	err := e.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		var alertsToSave []any
		for _, ev := range events {
			if err := e.insertFuelEvent(txCtx, st, s, ev); err != nil {
				return err
			}
			if ev.behaviour != nil {
				if err := e.insertBehaviour(txCtx, st, s, *ev.behaviour); err != nil {
					return err
				}
				d := ev.behaviour.driverID
				if d == "" {
					d = st.driverID
				}
				if d != "" && !containsString(behaviourDrivers, d) {
					behaviourDrivers = append(behaviourDrivers, d)
				}
			}
			alertsToSave = append(alertsToSave, e.buildAlert(st, s, ev))
		}
		if len(alertsToSave) > 0 {
			if err := e.alerts.SaveEvents(txCtx, s.vehicleID, "Vehicle", alertsToSave); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, d := range behaviourDrivers {
		if e.behaviourHook != nil {
			e.behaviourHook(ctx, d)
		}
	}
	for _, ev := range events {
		if ev.eventType == "siphon_confirmed" && e.siphonHook != nil {
			stopMinutes := int(cfg.siphonStop.Minutes())
			e.siphonHook(ctx, s.vehicleID, s.tripID, st.driverID, ev.estimated, stopMinutes)
		}
	}
	return nil
}

// detect classifies one smoothed reading against the thresholds and returns
// the anomaly events to persist (empty = clean reading). Mutates state only;
// persistence happens in pipeline's UnitOfWork.
func (e *FuelEngine) detect(st *vehicleFuelState, s snapshotRow, cfg engineConfig,
	smoothed, delta, noise float64, tripStatus string, odoBefore float64) []fuelEvent {

	stationary := math.Abs(s.speed) < cfg.stopSpeedKmh
	stoppedFor := time.Duration(0)
	if stationary {
		if st.stopStart.IsZero() {
			st.stopStart = s.ts
		}
		stoppedFor = s.ts.Sub(st.stopStart)
	} else {
		st.stopStart = time.Time{}
	}

	drop := st.baselineLevel - smoothed
	jump := smoothed - st.baselineLevel

	var events []fuelEvent
	switch {
	// Siphon first: a long stationary stop with a large drop is the more
	// specific diagnosis and must win over plain theft (Spec 03 §2.2 step 7).
	case stationary && st.tripActive && !st.stopStart.IsZero() &&
		stoppedFor >= cfg.siphonStop && drop >= cfg.siphonThresholdL:
		events = append(events, fuelEvent{
			eventType: "siphon_confirmed", estimated: drop, confidence: 1.0,
			before: st.baselineLevel, after: smoothed, odoBefore: odoBefore, severity: "critical",
			behaviour: &behaviourEvent{eventType: "fuel_theft_suspicion", severity: "high", weight: cfg.weightTheft},
		})
		st.baselineLevel = smoothed

	// Drain / theft suspected: drop beyond the threshold while stationary on
	// an active trip (or between trips) without trip end (Spec 03 §2.2 step 5).
	case stationary && st.tripActive && drop >= cfg.theftThresholdL:
		events = append(events, fuelEvent{
			eventType: "drain_theft_suspected", estimated: drop, confidence: 1.0,
			before: st.baselineLevel, after: smoothed, odoBefore: odoBefore, severity: "high",
			behaviour: &behaviourEvent{eventType: "fuel_theft_suspicion", severity: "high", weight: cfg.weightTheft},
		})
		st.baselineLevel = smoothed

	// Abnormal drain: drop while moving in_transit exceeding expected
	// consumption by the configured margin (Spec 03 §2.2 step 6).
	case !stationary && st.tripActive && tripStatus == "in_transit" &&
		drop >= expectedBurn(odoBefore, s, cfg):
		events = append(events, fuelEvent{
			eventType: "abnormal_drain", estimated: drop, confidence: 0.9,
			before: st.baselineLevel, after: smoothed, odoBefore: odoBefore, severity: "medium",
		})
		st.baselineLevel = smoothed

	// Refill: jump beyond the threshold sustained across consecutive
	// readings (Spec 03 §2.2 step 4). Accumulated from the baseline so a
	// multi-reading rise cannot slip under the threshold.
	case jump >= cfg.refillThresholdL:
		st.refillPending += jump
		events = append(events, fuelEvent{
			eventType: "refill_detected", estimated: jump, confidence: 1.0,
			before: st.baselineLevel, after: smoothed, odoBefore: odoBefore, severity: "low",
		})
		st.baselineLevel = smoothed
	}

	if len(events) == 0 {
		// No threshold crossed. The baseline follows the level only when the
		// movement is explained (consumption while driving) or is noise —
		// stationary drops and small rises accumulate toward their thresholds.
		if !stationary || math.Abs(delta) < noise {
			st.baselineLevel = smoothed
		}
	}
	return events
}

// expectedBurn returns the litres a moving vehicle is expected to consume
// over the odometer delta, inflated by the abnormal-drain margin.
func expectedBurn(odoBefore float64, s snapshotRow, cfg engineConfig) float64 {
	odoDelta := 0.0
	if s.odometer > 0 && odoBefore > 0 {
		odoDelta = s.odometer - odoBefore
	}
	return odoDelta * cfg.abnormalLPerKm * (1 + cfg.abnormalMarginPct/100.0)
}

// odoRollback detects odometer decreases beyond tolerance (Spec 03 §2.2 step 8)
// and resets the baseline to the new reading. Works with or without a fuel
// sensor.
func (e *FuelEngine) odoRollback(ctx context.Context, st *vehicleFuelState, s snapshotRow, cfg engineConfig) error {
	if s.odometer <= 0 {
		return nil
	}
	if st.lastOdometer > 0 && s.odometer < st.lastOdometer-cfg.odoToleranceKm {
		ev := fuelEvent{
			eventType: "odometer_rollback", estimated: st.lastOdometer - s.odometer,
			confidence: 1.0, before: st.lastOdometer, after: s.odometer, odoBefore: st.lastOdometer,
			severity:  "high",
			behaviour: &behaviourEvent{eventType: "odometer_rollback", severity: "high", weight: cfg.weightOdoRollback},
		}
		err := e.uow.Execute(ctx, func(txCtx ports.TxContext) error {
			if err := e.insertFuelEvent(txCtx, st, s, ev); err != nil {
				return err
			}
			if ev.behaviour != nil {
				if err := e.insertBehaviour(txCtx, st, s, *ev.behaviour); err != nil {
					return err
				}
			}
			return e.alerts.SaveEvents(txCtx, s.vehicleID, "Vehicle", []any{e.buildAlert(st, s, ev)})
		})
		if err != nil {
			return err
		}
		if e.behaviourHook != nil && ev.behaviour != nil {
			d := ev.behaviour.driverID
			if d == "" {
				d = st.driverID
			}
			if d != "" {
				e.behaviourHook(ctx, d)
			}
		}
	}
	// Baseline reset: the odometer guard now starts from the new reading
	// (Spec 03 §2.2 step 8 — "reset baseline to new reading").
	st.lastOdometer = s.odometer
	return nil
}

// tripStatus reads the trip status and driver via plain SQL (Spec 03 §13
// item 3 — the engine needs to know in_transit vs stationary).
func (e *FuelEngine) tripStatus(ctx context.Context, tripID string) (status, driverID string) {
	if tripID == "" {
		return "", ""
	}
	row := e.db.QueryRowContext(ctx,
		`SELECT status, COALESCE(driver_id, '') FROM trips WHERE id = ?`, tripID)
	if err := row.Scan(&status, &driverID); err != nil {
		return "", ""
	}
	return status, driverID
}

// isActiveTripStatus mirrors domain/trip.ActiveTripStatuses (Spec 03 §2.5).
func isActiveTripStatus(status string) bool {
	for _, s := range trip.ActiveTripStatuses {
		if string(s) == status {
			return true
		}
	}
	return false
}

// insertFuelEvent writes one durable anomaly/refill record. Honors the
// UnitOfWork transaction when called inside one (Spec 03 §1.2 rule 5).
func (e *FuelEngine) insertFuelEvent(ctx context.Context, st *vehicleFuelState, s snapshotRow, ev fuelEvent) error {
	db := txOrDB(ctx, e.db)
	_, err := db.ExecContext(ctx,
		`INSERT INTO fuel_events
		    (id, vehicle_id, trip_id, driver_id, event_type,
		     fuel_level_before, fuel_level_after, odometer_before, odometer_after,
		     estimated_litres, confidence, details, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.idGen.GenerateUUID(), s.vehicleID, strOrEmpty(s.tripID), strOrEmpty(st.driverID),
		ev.eventType, ev.before, ev.after, ev.odoBefore, s.odometer,
		ev.estimated, ev.confidence, ev.details(), timeStr(s.ts))
	return err
}

// insertBehaviour writes one scorecard input row (driver must be known —
// attribution via snapshot or trips.driver_id, Spec 03 §11 item 11). Honors
// the UnitOfWork transaction when called inside one.
func (e *FuelEngine) insertBehaviour(ctx context.Context, st *vehicleFuelState, s snapshotRow, b behaviourEvent) error {
	if b.driverID == "" {
		b.driverID = st.driverID
	}
	if b.driverID == "" {
		e.log.Warn("fuel engine: behaviour event skipped, unknown driver",
			"vehicle", s.vehicleID, "event_type", b.eventType)
		return nil
	}
	db := txOrDB(ctx, e.db)
	_, err := db.ExecContext(ctx,
		`INSERT INTO driver_behaviour_events
		    (id, driver_id, trip_id, vehicle_id, event_type, severity, weight, metadata, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.idGen.GenerateUUID(), b.driverID, strOrEmpty(s.tripID), s.vehicleID,
		b.eventType, b.severity, b.weight, "{}", timeStr(s.ts))
	return err
}

// txOrDB returns a query/exec interface bound to the active transaction when
// the context carries one (repository.TxFromContext), otherwise the plain
// connection.
func txOrDB(ctx context.Context, db *sql.DB) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return db
}

// buildAlert constructs the AlertEvent emitted to the outbox relay
// (Spec 03 §1.2 rule 4 — founder/alerts shape with Category "FUEL").
func (e *FuelEngine) buildAlert(st *vehicleFuelState, s snapshotRow, ev fuelEvent) alerts.AlertEvent {
	meta := map[string]interface{}{
		"vehicle_id":       s.vehicleID,
		"trip_id":          s.tripID,
		"driver_id":        st.driverID,
		"event_type":       ev.eventType,
		"estimated_litres": ev.estimated,
		"fuel_before":      ev.before,
		"fuel_after":       ev.after,
		"odometer_before":  st.lastOdometer,
		"odometer_after":   s.odometer,
	}
	return alerts.AlertEvent{
		ID:        e.idGen.GenerateUUID(),
		Category:  alerts.CategoryFuel,
		Priority:  alertPriority(ev.severity),
		Title:     alertTitle(ev.eventType),
		Summary:   fmt.Sprintf("%s on vehicle %s (est. %.1f L)", alertTitle(ev.eventType), s.vehicleID, ev.estimated),
		CompanyID: st.tenantID,
		Metadata:  meta,
		Timestamp: s.ts,
	}
}

func alertPriority(severity string) alerts.Priority {
	switch severity {
	case "critical":
		return alerts.PriorityCritical
	case "high":
		return alerts.PriorityHigh
	case "medium":
		return alerts.PriorityMedium
	default:
		return alerts.PriorityLow
	}
}

func alertTitle(eventType string) string {
	switch eventType {
	case "refill_detected":
		return "Fuel refill detected"
	case "drain_theft_suspected":
		return "Suspected fuel theft/drain"
	case "abnormal_drain":
		return "Abnormal fuel drain"
	case "siphon_confirmed":
		return "Fuel siphon confirmed"
	case "odometer_rollback":
		return "Odometer rollback"
	default:
		return eventType
	}
}

// fuelEvent helpers ---------------------------------------------------------

func (ev fuelEvent) details() string {
	return fmt.Sprintf(`{"before":%.2f,"after":%.2f,"est_litres":%.2f,"confidence":%.2f}`,
		ev.before, ev.after, ev.estimated, ev.confidence)
}

func (st *vehicleFuelState) toLitres(level float64) float64 {
	if st.unit == "litres" {
		return level
	}
	return level * st.tankCapacity / 100.0
}

// clampFuel bounds a raw fuel_level to [0, tankCapacity] (Spec 03 §2.2 step 1).
func clampFuel(level, capacity float64) float64 {
	if level < 0 {
		return 0
	}
	if level > capacity {
		return capacity
	}
	return level
}

// median returns the middle value of the window (Spec 03 §2.2 step 2). For
// even-length windows the lower middle is used, matching the floor semantics
// of the rolling median.
func median(w []float64) float64 {
	if len(w) == 0 {
		return math.NaN()
	}
	sorted := make([]float64, len(w))
	copy(sorted, w)
	sort.Float64s(sorted)
	return sorted[(len(sorted)-1)/2]
}

// deviates reports whether a reading deviates from the median by more than
// the spike_deviation_pct (Spec 03 §2.2 step 2).
func deviates(reading, med, pct float64) bool {
	if math.IsNaN(med) {
		return false
	}
	if med < 1e-9 {
		return reading > 1e-9
	}
	return math.Abs(reading-med)/med > pct/100.0
}

// timeStr formats a time for SQLite TEXT comparison (matches the format the
// ingestion pipeline stores via the modernc driver — zero-padded, so lexical
// order equals chronological order).
func timeStr(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

func strOrEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// containsString reports whether s is present in the slice.
func containsString(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
