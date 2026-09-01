package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/events"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/outbox"
	"transport-app/internal/shared/ports"
)

// Ingestor processes RawFrames through the canonical pipeline.
type Ingestor struct {
	deviceStore *DeviceStore
	quarantine  *QuarantineStore
	guard       *OdometerGuard
	outbox      *outbox.OutboxWriter
	bus         events.EventBus // dual-write fast-path (post-commit)
	audit       AuditLogger     // for spoof / guard audit logging
	uow         ports.UnitOfWork
	idGen       ports.IDGenerator
	cfg         IngestConfig
	queue       *AsyncIngestQueue
}

// IngestConfig holds pipeline configuration.
type IngestConfig struct {
	OdometerMaxRegressionKM float64
	FuelClampDeltaPct       float64
	RawRetentionDays        int
	BatchSize               int
	FlushInterval           time.Duration

	// Device-health guard thresholds (migration 00117). Zero values fall
	// back to the defaults applied in NewIngestor.
	BatteryLowPct     float64       // phone battery % at/below which low_battery fires (20)
	LowVoltageV       float64       // external voltage at/below which power_cut fires (6.0)
	GSMPoorLevel      int           // gsm signal at/below which poor_signal fires (1)
	ParkedDedupWindow time.Duration // motion=0 frames within this window may be deduped (10 min)
	ParkedDedupMaxM   float64       // max distance from last parked frame for dedup (50 m)
}

// applyDefaults fills zero-valued guard thresholds. Centralized so wiring
// code can pass an empty IngestConfig without silently disabling guards.
func (c *IngestConfig) applyDefaults() {
	if c.BatteryLowPct <= 0 {
		c.BatteryLowPct = 20
	}
	if c.LowVoltageV <= 0 {
		c.LowVoltageV = 6.0
	}
	if c.GSMPoorLevel <= 0 {
		c.GSMPoorLevel = 1
	}
	if c.ParkedDedupWindow <= 0 {
		c.ParkedDedupWindow = 10 * time.Minute
	}
	if c.ParkedDedupMaxM <= 0 {
		c.ParkedDedupMaxM = 50
	}
}

// NewIngestor constructs an Ingestor wired to the given persistence layer.
func NewIngestor(
	db *sql.DB,
	uow ports.UnitOfWork,
	bus events.EventBus,
	idGen ports.IDGenerator,
	audit AuditLogger,
	cfg IngestConfig,
) *Ingestor {
	if audit == nil {
		audit = NewAuditLogger(db)
	}
	cfg.applyDefaults()
	return &Ingestor{
		deviceStore: NewDeviceStore(db),
		quarantine:  NewQuarantineStore(db),
		guard:       NewOdometerGuard(NewDeviceStore(db), cfg.OdometerMaxRegressionKM, cfg.FuelClampDeltaPct, audit),
		outbox:      outbox.NewOutboxWriter(db),
		bus:         bus,
		audit:       audit,
		uow:         uow,
		idGen:       idGen,
		cfg:         cfg,
	}
}

// txOrDB returns the active transaction from context, or the fallback DB.
// Both implement ExecContext/QueryRowContext/QueryContext.
func txOrDB(ctx context.Context, fallback *sql.DB) interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
} {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return tx
	}
	return fallback
}

// SetQueue attaches an async buffer queue to the Ingestor.
func (ing *Ingestor) SetQueue(q *AsyncIngestQueue) {
	ing.queue = q
}

// IngestAsync pushes a frame into the in-memory ring-buffer for asynchronous processing.
// Returns nil if accepted, or error if queue is saturated.
func (ing *Ingestor) IngestAsync(ctx context.Context, frame RawFrame) error {
	if ing.queue == nil {
		_, err := ing.IngestRawFrame(ctx, frame)
		return err
	}
	if !ing.queue.Push(frame) {
		return errors.New("telemetry async buffer full")
	}
	return nil
}

// IngestRawFrame processes a single RawFrame through the canonical pipeline.
// Steps 1–11 (all DB writes) happen in a single transaction via uow.Execute.
// Step 12 (in-memory bus publish) happens after the transaction commits.
// Spec 17 §5.3
func (ing *Ingestor) IngestRawFrame(ctx context.Context, frame RawFrame) (IngestResult, error) {
	var result IngestResult
	var positionEvent *PositionEvent
	var sosEvent *SOSEvent
	var alertEvent *AlertEvent

	err := ing.uow.Execute(ctx, func(txCtx ports.TxContext) error {
		// Step 1: Device lookup
		device, err := ing.deviceStore.GetByIMEI(txCtx, frame.IMEI)
		if err != nil {
			return fmt.Errorf("device lookup: %w", err)
		}

		// Step 2: Unknown IMEI → quarantine
		if device == nil {
			return ing.quarantineFrame(txCtx, frame, QuarantineReasonUnknownDevice)
		}

		// Step 3: Non-active device → quarantine
		if device.Status != DeviceStatusActive {
			reason := QuarantineReasonRetiredDevice
			if device.Status == DeviceStatusQuarantined {
				reason = QuarantineReasonQuarantinedDevice
			}
			return ing.quarantineFrame(txCtx, frame, reason)
		}

		// Step 4: Insert raw event (dedup via partial unique index)
		rawEventID := ing.idGen.GenerateUUID()
		payloadJSON, _ := json.Marshal(frame)
		deduped, err := ing.insertRawEvent(txCtx, rawEventID, device.TenantID, frame, string(payloadJSON))
		if err != nil {
			return fmt.Errorf("raw event insert: %w", err)
		}
		if deduped {
			result.Deduped = true
			result.Accepted = true
			return nil // replay — no further processing
		}

		// Step 5: Odometer guard (monotonic check)
		adjustedOdometer := frame.Odometer
		if frame.Odometer != nil {
			adj, guardFired, err := ing.guard.CheckOdometer(txCtx, frame.IMEI, *frame.Odometer)
			if err != nil {
				return fmt.Errorf("odometer guard: %w", err)
			}
			if guardFired {
				adjustedOdometer = &adj
			}
		}

		// Step 6: Fuel clamp
		adjustedFuel := frame.FuelLevel
		if frame.FuelLevel != nil {
			adj, clampFired, err := ing.guard.ClampFuelLevel(txCtx, frame.IMEI, *frame.FuelLevel)
			if err != nil {
				return fmt.Errorf("fuel clamp: %w", err)
			}
			if clampFired {
				adjustedFuel = &adj
			}
		}

		// Resolve vehicle ID from the device (frame carries no vehicle_id).
		var vehicleID string
		if device.VehicleID != nil {
			vehicleID = *device.VehicleID
		}
		if vehicleID == "" {
			var vid string
			if errV := ing.deviceStore.db.QueryRowContext(txCtx, `
				SELECT COALESCE(t.vehicle_id, v.id, '')
				FROM drivers d
				LEFT JOIN trips t ON (t.driver_id = d.id OR t.driver_id = d.driver_id) AND t.status IN ('assigned', 'started', 'reached_pickup', 'in_transit')
				LEFT JOIN vehicles v ON (v.registration_number = d.notes OR v.vehicle_number = d.notes) AND v.tenant_id = d.tenant_id
				WHERE d.id = ? OR d.driver_id = ?
				LIMIT 1`, frame.IMEI, frame.IMEI).Scan(&vid); errV == nil && vid != "" {
				vehicleID = vid
			}
		}

		// Step 7: Parked-dedup check (migration 00117): a motion=0 frame that
		// is within ParkedDedupWindow of the last stored frame AND within
		// ParkedDedupMaxM of it (last frame also parked) adds no information.
		// The position/latest/snapshot writes are skipped but the raw event
		// (audit) and last_seen (liveness) still record the frame.
		parked := frame.Motion != nil && !*frame.Motion && ing.parkedDuplicate(txCtx, frame)

		// Step 7b: Insert position
		positionID := ing.idGen.GenerateUUID()
		receivedAt := time.Now().UTC()
		if !parked {
			if err := ing.insertPosition(txCtx, positionID, device.TenantID, vehicleID, frame, adjustedOdometer, adjustedFuel, rawEventID, receivedAt); err != nil {
				return fmt.Errorf("position insert: %w", err)
			}
		}

		// Step 8: Upsert vehicle_latest_position (only newer device_time wins).
		// Invalid-fix gate (migration 00117): a frame the device flagged as an
		// invalid GNSS fix (or with too few satellites) is stored in history for
		// audit but must never overwrite the live-map row — this is the parking-
		// drift corruption guard. Frames without an explicit Valid pointer are
		// trusted (back-compat: legacy providers never set it).
		if vehicleID != "" && !parked && (frame.Valid == nil || *frame.Valid) {
			if err := ing.upsertLatestPosition(txCtx, vehicleID, device.TenantID, frame, adjustedOdometer, adjustedFuel, receivedAt); err != nil {
				return fmt.Errorf("latest position upsert: %w", err)
			}
		}

		// Step 9: Ignition trip boundary (placeholder — cross-spec concern)
		// TODO: Detect ignition transitions and emit TripStartEvent/TripStopEvent.
		// This requires coordination with the booking spec. Deferred to Phase 2.

		// Step 10: Enrich + INSERT telemetry_snapshots
		if !parked {
			if err := ing.insertSnapshot(txCtx, frame, device, vehicleID, adjustedOdometer, adjustedFuel, positionID, receivedAt); err != nil {
				return fmt.Errorf("snapshot insert: %w", err)
			}
		}

		// Step 11: Outbox write (same tx). SOS rides the same transaction so
		// the emergency event can never be lost while the position commits.
		positionEvent = &PositionEvent{
			EventID:     positionID,
			TenantID:    device.TenantID,
			DeviceIMEI:  frame.IMEI,
			VehicleID:   vehicleID,
			DriverID:    frame.DriverID,
			TripID:      frame.TripID,
			Latitude:    frame.Latitude,
			Longitude:   frame.Longitude,
			Speed:       frame.Speed,
			Heading:     frame.Heading,
			Ignition:    frame.Ignition,
			EngineHours: frame.EngineHours,
			Accuracy:    frame.Accuracy,
			FuelLevel:   adjustedFuel,
			Odometer:    adjustedOdometer,
			DeviceTime:  frame.DeviceTime,
			ReceivedAt:  receivedAt,
		}

		aggregateID := vehicleID
		if aggregateID == "" {
			aggregateID = frame.IMEI // IMEI when unassigned (Decision D6)
		}
		var eventsToSave []any
		if !parked {
			eventsToSave = append(eventsToSave, positionEvent)
		}
		// Device-health guard (migration 00117): transition detection against
		// the previous stored row. Fires at most one alert (power_cut >
		// low_battery > poor_signal) per frame and only on a healthy→unhealthy
		// crossing, so recovery re-arms it without alert spam.
		if !parked {
			if alert := ing.deviceHealthGuard(txCtx, positionID, device, vehicleID, frame); alert != nil {
				alertEvent = alert
				eventsToSave = append(eventsToSave, alertEvent)
			}
		}
		if frame.SOS {
			sosEvent = &SOSEvent{
				EventID:    ing.idGen.GenerateUUID(),
				TenantID:   device.TenantID,
				DeviceIMEI: frame.IMEI,
				VehicleID:  vehicleID,
				DriverID:   frame.DriverID,
				Latitude:   frame.Latitude,
				Longitude:  frame.Longitude,
				OccurredAt: receivedAt,
				DeviceTime: frame.DeviceTime,
			}
			eventsToSave = append(eventsToSave, sosEvent)
		}
		if len(eventsToSave) > 0 {
			if err := ing.outbox.SaveEvents(txCtx, aggregateID, "Vehicle", eventsToSave); err != nil {
				return fmt.Errorf("outbox write: %w", err)
			}
		}

		// Update last_seen_at last.
		if err := ing.deviceStore.UpdateLastSeen(txCtx, frame.IMEI); err != nil {
			return fmt.Errorf("last_seen update: %w", err)
		}

		result.Accepted = true
		return nil
	})

	if err != nil {
		return result, err
	}

	// Step 12 (post-commit): Dual-write fast-path — publish to in-memory bus
	// for real-time consumers (SSE hub in Spec 04). Fire-and-forget; if the
	// bus publish fails the outbox relay catches up within 5s.
	if result.Accepted && !result.Deduped && positionEvent != nil {
		snapshotPayload := map[string]interface{}{
			"vehicle_id": positionEvent.VehicleID,
			"trip_id":    positionEvent.TripID,
			"lat":        positionEvent.Latitude,
			"lng":        positionEvent.Longitude,
			"speed":      positionEvent.Speed,
			"fuel_level": positionEvent.FuelLevel,
			"odometer":   positionEvent.Odometer,
			"timestamp":  positionEvent.DeviceTime,
		}
		ing.bus.Publish(ctx, events.Event{
			Type:    BusEventTelemetrySnapshot,
			Payload: snapshotPayload,
		})
		if sosEvent != nil {
			ing.bus.Publish(ctx, events.Event{
				Type:    EventTypeSOS,
				Payload: sosEvent,
			})
		}
	}
	if result.Accepted && !result.Deduped && alertEvent != nil {
		// Device-health alert → alerts pipeline (engine normalizes the
		// "telemetry.alert" type and routes into the alert inbox).
		ing.bus.Publish(ctx, events.Event{
			Type:    BusEventTelemetryAlert,
			Payload: alertEvent,
		})
	}

	return result, nil
}

// parkedDuplicate reports whether the frame is a redundant parked report:
// the last stored row for the IMEI was also parked (motion=0), is within
// ParkedDedupWindow, and within ParkedDedupMaxM of the incoming frame.
func (ing *Ingestor) parkedDuplicate(ctx context.Context, frame RawFrame) bool {
	db := txOrDB(ctx, ing.deviceStore.db)
	var lastTime time.Time
	var lat, lng sql.NullFloat64
	var motion sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT device_time, latitude, longitude, motion
		FROM telemetry_positions WHERE imei = ?
		ORDER BY device_time DESC LIMIT 1`, frame.IMEI).Scan(&lastTime, &lat, &lng, &motion)
	if err != nil || !motion.Valid || motion.Int64 != 0 || !lat.Valid || !lng.Valid {
		return false
	}
	// Window compares DEVICE times, not wall clock: a phone flushing a parked
	// frame queued 30 minutes ago must be judged against its own fix gap, not
	// arrival time. Out-of-order (stale) frames are always stored.
	gap := frame.DeviceTime.Sub(lastTime)
	if gap < 0 || gap > ing.cfg.ParkedDedupWindow {
		return false
	}
	return haversineMeters(lat.Float64, lng.Float64, frame.Latitude, frame.Longitude) <= ing.cfg.ParkedDedupMaxM
}

// haversineMeters returns the great-circle distance in metres (mirrors the
// ewaybill/helpers.go helper; kept local to avoid a cross-module dependency).
func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusM = 6371000.0
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusM * math.Asin(math.Sqrt(a))
}

// deviceHealthGuard compares the frame against the previously stored row for
// the IMEI and emits at most one AlertEvent per healthy→unhealthy crossing
// (priority: power_cut > low_battery > poor_signal). The current position row
// (excludePositionID) is excluded so the comparison is against the prior
// frame. Recovery (a healthy frame later) re-arms the alert — stateless
// hysteresis with no extra table.
func (ing *Ingestor) deviceHealthGuard(ctx context.Context, excludePositionID string, device *Device, vehicleID string, frame RawFrame) *AlertEvent {
	if frame.BatteryLevel == nil && frame.ExternalVoltage == nil && frame.GSMSignal == nil {
		return nil
	}
	db := txOrDB(ctx, ing.deviceStore.db)
	var prevBatt, prevVolt sql.NullFloat64
	var prevGsm sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT battery_level, external_voltage, gsm_signal
		FROM telemetry_positions
		WHERE imei = ? AND id <> ?
		ORDER BY device_time DESC LIMIT 1`, frame.IMEI, excludePositionID).
		Scan(&prevBatt, &prevVolt, &prevGsm)
	if err != nil && err != sql.ErrNoRows {
		return nil // guard must never fail ingestion
	}

	mk := func(kind, severity, details string) *AlertEvent {
		return &AlertEvent{
			EventID:    ing.idGen.GenerateUUID(),
			TenantID:   device.TenantID,
			DeviceIMEI: frame.IMEI,
			VehicleID:  vehicleID,
			DriverID:   frame.DriverID,
			AlertType:  kind,
			Severity:   severity,
			Latitude:   frame.Latitude,
			Longitude:  frame.Longitude,
			Details:    details,
			OccurredAt: time.Now().UTC(),
		}
	}

	// power_cut / tamper: hardwired trackers report external voltage; a drop
	// from healthy to near-zero means the tracker was unplugged or lost power.
	if frame.ExternalVoltage != nil && prevVolt.Valid && prevVolt.Float64 > ing.cfg.LowVoltageV &&
		*frame.ExternalVoltage <= ing.cfg.LowVoltageV {
		kind := AlertKindPowerCut
		if device.DeviceType == DeviceTypeHardware {
			kind = AlertKindTamper
		}
		return mk(kind, SeverityCritical,
			fmt.Sprintf("External voltage dropped from %.1fV to %.1fV (device %s)", prevVolt.Float64, *frame.ExternalVoltage, frame.IMEI))
	}
	// low_battery: phone/app devices crossing the low threshold.
	if frame.BatteryLevel != nil && prevBatt.Valid && prevBatt.Float64 > ing.cfg.BatteryLowPct &&
		*frame.BatteryLevel <= ing.cfg.BatteryLowPct {
		return mk(AlertKindLowBattery, SeverityWarning,
			fmt.Sprintf("Device battery dropped from %.0f%% to %.0f%% (device %s)", prevBatt.Float64, *frame.BatteryLevel, frame.IMEI))
	}
	// poor_signal: GSM level collapse — triage signal for "vehicle vanished".
	if frame.GSMSignal != nil && prevGsm.Valid && int(prevGsm.Int64) > ing.cfg.GSMPoorLevel &&
		*frame.GSMSignal <= ing.cfg.GSMPoorLevel {
		return mk(AlertKindPoorSignal, SeverityWarning,
			fmt.Sprintf("GSM signal dropped from %d to %d (device %s)", prevGsm.Int64, *frame.GSMSignal, frame.IMEI))
	}
	return nil
}

// quarantineFrame inserts a quarantine entry for a frame that cannot be
// processed. For unknown devices, the default tenant is used (Decision D6).
func (ing *Ingestor) quarantineFrame(ctx context.Context, frame RawFrame, reason string) error {
	rawPayload, _ := json.Marshal(frame)
	tenantID := shared.DefaultTenant
	if dev, _ := ing.deviceStore.GetByIMEI(ctx, frame.IMEI); dev != nil {
		tenantID = shared.TenantID(dev.TenantID)
	}
	entry := QuarantineEntry{
		ID:         ing.idGen.GenerateUUID(),
		TenantID:   string(tenantID),
		IMEI:       frame.IMEI,
		Source:     frame.Provider,
		RawPayload: string(rawPayload),
		Reason:     reason,
		Status:     QuarantineStatusOpen,
		CreatedAt:  time.Now().UTC(),
	}
	return ing.quarantine.Quarantine(ctx, entry)
}

// insertRawEvent inserts a raw event with dedup. Returns true if the frame was
// a replay (provider_msg_id already seen via partial unique index).
// An empty provider_msg_id is stored as SQL NULL: the partial unique index
// (`WHERE provider_msg_id IS NOT NULL`) does not cover NULL, so NULL-keyed
// frames are always inserted and never deduped (Decision D2).
func (ing *Ingestor) insertRawEvent(ctx context.Context, id, tenantID string, frame RawFrame, payload string) (bool, error) {
	db := txOrDB(ctx, ing.deviceStore.db)
	var providerMsgID any
	if frame.ProviderMsgID != "" {
		providerMsgID = frame.ProviderMsgID
	}
	res, err := db.ExecContext(ctx,
		`INSERT OR IGNORE INTO telemetry_raw_events
            (id, tenant_id, imei, device_time, provider, provider_msg_id, payload)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, frame.IMEI, frame.DeviceTime, frame.Provider, providerMsgID, payload,
	)
	if err != nil {
		return false, err
	}
	affected, _ := res.RowsAffected()
	return affected == 0, nil
}

// insertPosition inserts a position row into telemetry_positions.
func (ing *Ingestor) insertPosition(ctx context.Context, id, tenantID, vehicleID string, frame RawFrame, odometer, fuel *float64, rawEventID string, receivedAt time.Time) error {
	db := txOrDB(ctx, ing.deviceStore.db)
	_, err := db.ExecContext(ctx,
		`INSERT INTO telemetry_positions
            (id, tenant_id, imei, device_time, received_at, latitude, longitude,
             speed, heading, ignition, engine_hours, accuracy, fuel_level, odometer,
             satellites, battery_level, external_voltage, gsm_signal, motion,
             valid, fix_time,
             driver_id, trip_id, vehicle_id, provider, raw_event_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, frame.IMEI, frame.DeviceTime, receivedAt,
		frame.Latitude, frame.Longitude, frame.Speed, frame.Heading,
		frame.Ignition, frame.EngineHours, frame.Accuracy, fuel, odometer,
		frame.Satellites, frame.BatteryLevel, frame.ExternalVoltage, frame.GSMSignal,
		boolPtr(frame.Motion), validInt(frame.Valid), frame.FixTime,
		frame.DriverID, frame.TripID, vehicleID,
		frame.Provider, rawEventID,
	)
	return err
}

// boolPtr converts a *bool to an SQL integer (NULL when absent).
func boolPtr(b *bool) any {
	if b == nil {
		return nil
	}
	return *b
}

// validInt normalizes the Valid pointer to a stored 0/1: absent means trusted
// (same contract as the Step-8 gate) so the NOT NULL column never sees NULL.
func validInt(v *bool) any {
	if v == nil {
		return 1
	}
	if *v {
		return 1
	}
	return 0
}

// upsertLatestPosition upserts vehicle_latest_position. The ON CONFLICT
// WHERE clause ensures only newer device_time replaces the existing row
// (Decision D3 / GOTCHA #3).
func (ing *Ingestor) upsertLatestPosition(ctx context.Context, vehicleID, tenantID string, frame RawFrame, odometer, fuel *float64, receivedAt time.Time) error {
	db := txOrDB(ctx, ing.deviceStore.db)
	_, err := db.ExecContext(ctx,
		`INSERT INTO vehicle_latest_position
            (vehicle_id, tenant_id, imei, device_time, received_at, latitude, longitude,
             speed, heading, ignition, engine_hours, accuracy, fuel_level, odometer,
             satellites, battery_level, external_voltage, gsm_signal, motion, valid,
             driver_id, trip_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT(vehicle_id) DO UPDATE SET
            tenant_id     = excluded.tenant_id,
            imei          = excluded.imei,
            device_time   = excluded.device_time,
            received_at   = excluded.received_at,
            latitude      = excluded.latitude,
            longitude     = excluded.longitude,
            speed         = excluded.speed,
            heading       = excluded.heading,
            ignition      = excluded.ignition,
            engine_hours  = excluded.engine_hours,
            accuracy      = excluded.accuracy,
            fuel_level    = excluded.fuel_level,
            odometer      = excluded.odometer,
            satellites    = excluded.satellites,
            battery_level = excluded.battery_level,
            external_voltage = excluded.external_voltage,
            gsm_signal    = excluded.gsm_signal,
            motion        = excluded.motion,
            valid         = excluded.valid,
            driver_id     = excluded.driver_id,
            trip_id       = excluded.trip_id
         WHERE excluded.device_time > vehicle_latest_position.device_time`,
		vehicleID, tenantID, frame.IMEI, frame.DeviceTime, receivedAt,
		frame.Latitude, frame.Longitude, frame.Speed, frame.Heading,
		frame.Ignition, frame.EngineHours, frame.Accuracy, fuel, odometer,
		frame.Satellites, frame.BatteryLevel, frame.ExternalVoltage, frame.GSMSignal,
		boolPtr(frame.Motion), validInt(frame.Valid),
		frame.DriverID, frame.TripID,
	)
	return err
}

// insertSnapshot enriches and inserts a telemetry_snapshots row.
// Decision D5: new rows use UUIDs; old 'snap-*' IDs are not migrated.
func (ing *Ingestor) insertSnapshot(ctx context.Context, frame RawFrame, device *Device, vehicleID string, odometer, fuel *float64, positionID string, receivedAt time.Time) error {
	db := txOrDB(ctx, ing.deviceStore.db)
	snapshotID := uuid.NewString() // UUID per Decision D5
	if vehicleID == "" && device.VehicleID != nil {
		vehicleID = *device.VehicleID
	}
	_, err := db.ExecContext(ctx,
		`INSERT OR REPLACE INTO telemetry_snapshots
            (id, trip_id, vehicle_id, timestamp, latitude, longitude, speed,
             fuel_level, odometer, heading, ignition, engine_hours, accuracy, driver_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshotID, frame.TripID, vehicleID, frame.DeviceTime,
		frame.Latitude, frame.Longitude, frame.Speed,
		fuel, odometer,
		frame.Heading, frame.Ignition, frame.EngineHours, frame.Accuracy, frame.DriverID,
	)
	_ = receivedAt
	return err
}

// quarantineUnknown inserts a quarantine entry for a frame from an IMEI that
// does not exist in telemetry_devices. Used by the REST handler for unknown
// device tokens.
func (ing *Ingestor) quarantineUnknown(ctx context.Context, imei string, frame RawFrame) error {
	rawPayload, _ := json.Marshal(frame)
	tenantID := string(shared.DefaultTenant)
	entry := QuarantineEntry{
		ID:         ing.idGen.GenerateUUID(),
		TenantID:   tenantID,
		IMEI:       imei,
		Source:     frame.Provider,
		RawPayload: string(rawPayload),
		Reason:     QuarantineReasonUnknownDevice,
		Status:     QuarantineStatusOpen,
		CreatedAt:  time.Now().UTC(),
	}
	return ing.quarantine.Quarantine(ctx, entry)
}

// auditSpoof records a device-identity mismatch (spoof attempt) in audit_logs.
func (ing *Ingestor) auditSpoof(ctx context.Context, topicIMEI, payloadIMEI string) error {
	if ing.audit == nil {
		return nil
	}
	return ing.audit.LogAction(ctx, "mqtt_spoof_guard", "telemetry_devices", topicIMEI,
		map[string]interface{}{"topic_imei": topicIMEI},
		map[string]interface{}{"payload_imei": payloadIMEI})
}
