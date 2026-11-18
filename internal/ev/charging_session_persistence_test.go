package ev_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/ev"
)

type failingSessionRepo struct {
	ev.Repository
	profile         *ev.EVProfile
	active          *ev.ChargingSession
	createErr       error
	updateErr       error
	telemetryStored bool
}

func (r *failingSessionRepo) GetProfile(context.Context, string, string) (*ev.EVProfile, error) {
	return r.profile, nil
}

func (r *failingSessionRepo) GetActiveChargingSession(context.Context, string, string) (*ev.ChargingSession, error) {
	return r.active, nil
}

func (r *failingSessionRepo) CreateChargingSession(context.Context, *ev.ChargingSession) error {
	return r.createErr
}

func (r *failingSessionRepo) UpdateChargingSession(context.Context, *ev.ChargingSession) error {
	return r.updateErr
}

func (r *failingSessionRepo) UpsertTelemetry(context.Context, *ev.EVTelemetry) error {
	r.telemetryStored = true
	return nil
}

func evProfile() *ev.EVProfile {
	return &ev.EVProfile{VehicleID: "ev-1", TenantID: "tenant-ev", BatteryCapacityKWh: 60, MaxRangeKM: 300}
}

// Charging sessions carry cost and energy records. Dropping a failed write
// would silently corrupt billing and carbon accounting, so the ingest fails
// closed instead of storing an orphan telemetry frame.
func TestIngestTelemetry_ChargingSessionWriteFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	charging := ev.IngestEVTelemetryInput{
		VehicleID:      "ev-1",
		SoCPercent:     80,
		SoHPercent:     95,
		ChargingStatus: ev.ChargingStatusCharging,
	}

	t.Run("new session create error surfaces and telemetry is not stored", func(t *testing.T) {
		repo := &failingSessionRepo{profile: evProfile(), createErr: errors.New("db offline")}
		svc := ev.NewService(repo)

		got, err := svc.IngestTelemetry(ctx, "tenant-ev", charging)
		require.Error(t, err)
		assert.ErrorContains(t, err, "db offline")
		assert.Nil(t, got)
		assert.False(t, repo.telemetryStored)
	})

	t.Run("active session update error surfaces and telemetry is not stored", func(t *testing.T) {
		active := &ev.ChargingSession{
			ID: "sess-1", TenantID: "tenant-ev", VehicleID: "ev-1",
			StartSoC: 50, PeakPowerKW: 7, Status: ev.SessionStatusInProgress,
		}
		repo := &failingSessionRepo{profile: evProfile(), active: active, updateErr: errors.New("lock timeout")}
		svc := ev.NewService(repo)
		frame := charging
		frame.ChargingPowerKW = 50 // forces PeakPowerKW change -> update path

		got, err := svc.IngestTelemetry(ctx, "tenant-ev", frame)
		require.Error(t, err)
		assert.ErrorContains(t, err, "lock timeout")
		assert.Nil(t, got)
		assert.False(t, repo.telemetryStored)
	})

	t.Run("healthy repository still stores telemetry", func(t *testing.T) {
		repo := &failingSessionRepo{profile: evProfile()}
		svc := ev.NewService(repo)

		got, err := svc.IngestTelemetry(ctx, "tenant-ev", charging)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, repo.telemetryStored)
	})
}
