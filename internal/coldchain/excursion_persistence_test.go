package coldchain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/coldchain"
)

// failingExcursionRepo embeds the interface so only the methods this service
// path touches need overriding. Any unexpected call panics, which keeps the
// test honest about the service's real dependencies.
type failingExcursionRepo struct {
	coldchain.Repository
	profile         *coldchain.ReeferProfile
	active          *coldchain.TemperatureExcursion
	createErr       error
	updateErr       error
	telemetryStored bool
}

func (r *failingExcursionRepo) GetProfile(context.Context, string, string) (*coldchain.ReeferProfile, error) {
	return r.profile, nil
}

func (r *failingExcursionRepo) GetActiveExcursion(context.Context, string, string, int) (*coldchain.TemperatureExcursion, error) {
	return r.active, nil
}

func (r *failingExcursionRepo) CreateExcursion(context.Context, *coldchain.TemperatureExcursion) error {
	return r.createErr
}

func (r *failingExcursionRepo) UpdateExcursion(context.Context, *coldchain.TemperatureExcursion) error {
	return r.updateErr
}

func (r *failingExcursionRepo) UpsertTelemetry(context.Context, *coldchain.ReeferTelemetry) error {
	r.telemetryStored = true
	return nil
}

func chilledProfile() *coldchain.ReeferProfile {
	return &coldchain.ReeferProfile{
		VehicleID:        "veh-ref-1",
		TenantID:         "tenant-cc-a",
		Zone1MinTempC:    2.0,
		Zone1MaxTempC:    8.0,
		Zone1TargetTempC: 4.0,
	}
}

// A cold-chain excursion is a regulated GDP/HACCP record. If the database
// refuses to persist it, the ingest must fail closed rather than store a
// temperature reading whose breach has no audit trail.
func TestIngestTelemetry_ExcursionWriteFailureFailsClosed(t *testing.T) {
	ctx := context.Background()
	hot := coldchain.IngestReeferTelemetryInput{
		VehicleID:  "veh-ref-1",
		Zone1TempC: 12.5, // above 8C max + 3C -> critical over-temp
	}

	t.Run("new excursion create error surfaces and telemetry is not stored", func(t *testing.T) {
		repo := &failingExcursionRepo{profile: chilledProfile(), createErr: errors.New("disk full")}
		svc := coldchain.NewService(repo)

		got, err := svc.IngestTelemetry(ctx, "tenant-cc-a", hot)
		require.Error(t, err, "failed excursion persistence must not be swallowed")
		assert.ErrorContains(t, err, "disk full")
		assert.Nil(t, got)
		assert.False(t, repo.telemetryStored, "must not store telemetry without its breach record")
	})

	t.Run("active excursion update error surfaces and telemetry is not stored", func(t *testing.T) {
		active := &coldchain.TemperatureExcursion{
			ID:            "exc-1",
			TenantID:      "tenant-cc-a",
			VehicleID:     "veh-ref-1",
			ZoneNumber:    1,
			ExcursionType: coldchain.ExcursionOverTemp,
			Status:        coldchain.ExcursionOpen,
			PeakTempC:     11.0,
		}
		repo := &failingExcursionRepo{profile: chilledProfile(), active: active, updateErr: errors.New("write conflict")}
		svc := coldchain.NewService(repo)

		got, err := svc.IngestTelemetry(ctx, "tenant-cc-a", hot)
		require.Error(t, err)
		assert.ErrorContains(t, err, "write conflict")
		assert.Nil(t, got)
		assert.False(t, repo.telemetryStored)
	})

	t.Run("healthy repository still stores telemetry", func(t *testing.T) {
		repo := &failingExcursionRepo{profile: chilledProfile()}
		svc := coldchain.NewService(repo)

		got, err := svc.IngestTelemetry(ctx, "tenant-cc-a", hot)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.True(t, got.HasActiveExcursion)
		assert.True(t, repo.telemetryStored)
	})
}
