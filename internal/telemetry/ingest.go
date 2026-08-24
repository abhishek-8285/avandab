package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
}

// IngestConfig holds pipeline configuration.
type IngestConfig struct {
	OdometerMaxRegressionKM float64
	FuelClampDeltaPct       float64
	RawRetentionDays        int
	BatchSize               int
	FlushInterval           time.Duration
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

// IngestRawFrame processes a single RawFrame through the canonical pipeline.
// Steps 1–11 (all DB writes) happen in a single transaction via uow.Execute.
// Step 12 (in-memory bus publish) happens after the transaction commits.
// Spec 17 §5.3
func (ing *Ingestor) IngestRawFrame(ctx context.Context, frame RawFrame) (IngestResult, error) {
	var result IngestResult
	var positionEvent *PositionEvent
	var sosEvent *SOSEvent

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

		// Step 7: Insert position
		positionID := ing.idGen.GenerateUUID()
		receivedAt := time.Now().UTC()
		if err := ing.insertPosition(txCtx, positionID, device.TenantID, vehicleID, frame, adjustedOdometer, adjustedFuel, rawEventID, receivedAt); err != nil {
			return fmt.Errorf("position insert: %w", err)
		}

		// Step 8: Upsert vehicle_latest_position (only newer device_time wins)
		if device.VehicleID != nil {
			if err := ing.upsertLatestPosition(txCtx, vehicleID, device.TenantID, frame, adjustedOdometer, adjustedFuel, receivedAt); err != nil {
				return fmt.Errorf("latest position upsert: %w", err)
			}
		}

		// Step 9: Ignition trip boundary (placeholder — cross-spec concern)
		// TODO: Detect ignition transitions and emit TripStartEvent/TripStopEvent.
		// This requires coordination with the booking spec. Deferred to Phase 2.

		// Step 10: Enrich + INSERT telemetry_snapshots
		if err := ing.insertSnapshot(txCtx, frame, device, adjustedOdometer, adjustedFuel, positionID, receivedAt); err != nil {
			return fmt.Errorf("snapshot insert: %w", err)
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
		eventsToSave := []any{positionEvent}
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
		if err := ing.outbox.SaveEvents(txCtx, aggregateID, "Vehicle", eventsToSave); err != nil {
			return fmt.Errorf("outbox write: %w", err)
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

	return result, nil
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
             driver_id, trip_id, vehicle_id, provider, raw_event_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, frame.IMEI, frame.DeviceTime, receivedAt,
		frame.Latitude, frame.Longitude, frame.Speed, frame.Heading,
		frame.Ignition, frame.EngineHours, frame.Accuracy, fuel, odometer,
		frame.DriverID, frame.TripID, vehicleID,
		frame.Provider, rawEventID,
	)
	return err
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
             driver_id, trip_id)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
            driver_id     = excluded.driver_id,
            trip_id       = excluded.trip_id
         WHERE excluded.device_time > vehicle_latest_position.device_time`,
		vehicleID, tenantID, frame.IMEI, frame.DeviceTime, receivedAt,
		frame.Latitude, frame.Longitude, frame.Speed, frame.Heading,
		frame.Ignition, frame.EngineHours, frame.Accuracy, fuel, odometer,
		frame.DriverID, frame.TripID,
	)
	return err
}

// insertSnapshot enriches and inserts a telemetry_snapshots row.
// Decision D5: new rows use UUIDs; old 'snap-*' IDs are not migrated.
func (ing *Ingestor) insertSnapshot(ctx context.Context, frame RawFrame, device *Device, odometer, fuel *float64, positionID string, receivedAt time.Time) error {
	db := txOrDB(ctx, ing.deviceStore.db)
	snapshotID := uuid.NewString() // UUID per Decision D5
	vehicleID := ""
	if device.VehicleID != nil {
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
