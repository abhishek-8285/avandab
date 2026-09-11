package domain

import (
	"context"
	"time"
)

// Dwell engine states (Spec 02 DDL CHECK constraint).
const (
	StateOutside  = "outside"
	StateEntering = "entering"
	StateInside   = "inside"
	StateLeaving  = "leaving"
)

// Geofence event types (Spec 02 DDL CHECK constraint).
const (
	EventEntering = "entering"
	EventInside   = "inside"
	EventLeaving  = "leaving"
	EventOutside  = "outside"
	EventBreach   = "breach"
	EventAlert    = "alert"
)

// Detention statuses (Spec 02 DDL CHECK constraint).
const (
	DetentionOpen     = "open"
	DetentionClosed   = "closed"
	DetentionAttached = "attached"
	DetentionInvoiced = "invoiced"
	DetentionWaived   = "waived"
)

// EngineState is the durable per-vehicle dwell state machine row (Spec 02 §4).
type EngineState struct {
	VehicleID     string
	TenantID      string
	State         string // outside | entering | inside | leaving
	TripID        *string
	GeofenceID    *string
	ZoneKind      *string
	ZoneEnteredAt *time.Time
	ConfirmedAt   *time.Time
	ExitStartedAt *time.Time
	LastFixAt     time.Time
	LastLat       float64
	LastLng       float64
	UpdatedAt     time.Time
}

// GeofenceEvent is a durable log row for zone transitions and alerts.
type GeofenceEvent struct {
	ID         string
	TenantID   string
	VehicleID  *string
	TripID     *string
	GeofenceID *string
	ZoneKind   *string
	EventType  string // entering | leaving | breach | ...
	AlertType  *string
	Severity   *string
	Latitude   *float64
	Longitude  *float64
	Details    *string
	CreatedAt  time.Time
}

// Detention is a pickup/drop dwell window (Spec 02 §5).
type Detention struct {
	ID              string
	TenantID        string
	TripID          string
	VehicleID       *string
	GeofenceID      *string
	ZoneKind        string // pickup | drop
	ZoneName        string // resolved geofence name (billing description)
	EnteredAt       time.Time
	ExitedAt        *time.Time
	DwellSeconds    int64
	FreeSeconds     int64
	BillableSeconds int64
	RatePerHour     float64
	Amount          float64
	Status          string
}

// GeofenceRepository reads geofence definitions. All methods must honour
// repository.TxFromContext so they join the active UnitOfWork transaction.
type GeofenceRepository interface {
	ListActiveByTenant(ctx context.Context, tenantID string) ([]Geofence, error)
	ListBoundForVehicle(ctx context.Context, tenantID, vehicleID string) ([]Geofence, error)
}

// GeofenceAdminRepository is the write side of the zone registry used by the
// web CRUD (Spec 02 §8). Must honour repository.TxFromContext.
type GeofenceAdminRepository interface {
	Create(ctx context.Context, z Geofence) error
	Update(ctx context.Context, z Geofence) error
	SoftDelete(ctx context.Context, tenantID, id string) error
	ListAll(ctx context.Context, tenantID string) ([]Geofence, error)
	Find(ctx context.Context, tenantID, id string) (*Geofence, error)
}

// EngineStateRepository persists the per-vehicle dwell state machine.
type EngineStateRepository interface {
	GetByVehicle(ctx context.Context, tenantID, vehicleID string) (*EngineState, error)
	Upsert(ctx context.Context, s EngineState) error
}

// EventLogRepository persists zone events, alerts and detention windows.
type EventLogRepository interface {
	InsertEvent(ctx context.Context, e GeofenceEvent) (inserted bool, err error)
	OpenDetention(ctx context.Context, d Detention) error
	FindOpenDetention(ctx context.Context, tenantID, tripID, zoneKind string) (*Detention, error)
	CloseDetention(ctx context.Context, id string, exitedAt time.Time, dwellSeconds, freeSeconds int64, ratePerHour, amount float64) error
	Find(ctx context.Context, tenantID, id string) (*Detention, error)
	// ListClosedForTrip returns closed, billable detentions for a trip that
	// are not yet attached to an invoice (Spec 02 §6).
	ListClosedForTrip(ctx context.Context, tenantID, tripID string) ([]Detention, error)
	MarkAttached(ctx context.Context, id string) error
	Waive(ctx context.Context, id string) error
}

// FixRepository supplies snapshots the worker has not yet consumed
// (newer than engine_state.last_fix_at per vehicle).
type FixRepository interface {
	LoadNewFixes(ctx context.Context, limit int) ([]Fix, error)
}
