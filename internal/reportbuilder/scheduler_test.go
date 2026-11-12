package reportbuilder

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDelivery struct {
	configured bool
	calls      int
	tenants    []string
	err        error
}

func (f *fakeDelivery) Configured() bool { return f.configured }

func (f *fakeDelivery) Deliver(ctx context.Context, tenantID string, sched *ReportSchedule, artifact ReportArtifact) error {
	f.calls++
	f.tenants = append(f.tenants, tenantID)
	return f.err
}

func newSchedule(t *testing.T, repo *Repository, id, tenantID string, active bool, nextRun time.Time) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, repo.CreateSchedule(context.Background(), &ReportSchedule{
		ID:         id,
		TenantID:   tenantID,
		ReportID:   "sys-rep-trip-profitability",
		Name:       id,
		Frequency:  "DAILY",
		Format:     "CSV",
		Channel:    "EMAIL",
		Recipients: []string{"ops@fleet.local"},
		IsActive:   active,
		NextRunAt:  &nextRun,
		CreatedBy:  "u1",
		CreatedAt:  now,
		UpdatedAt:  now,
	}))
}

func testSchedulerLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// One tick must run the single due+enabled schedule, and leave the not-yet-due
// and the disabled rows untouched.
func TestScheduler_RunOnce_RunsOnlyDueEnabledSchedules(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewRepository(db)
	delivery := &fakeDelivery{configured: true}
	svc := NewServiceWithDelivery(repo, delivery)
	sched := NewScheduler(svc, testSchedulerLogger())

	now := time.Now().UTC()
	newSchedule(t, repo, "due", "tenant-1", true, now.Add(-time.Minute))
	newSchedule(t, repo, "not-due", "tenant-1", true, now.Add(time.Hour))
	newSchedule(t, repo, "disabled", "tenant-1", false, now.Add(-time.Minute))

	processed, err := sched.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, processed)
	assert.Equal(t, 1, delivery.calls, "exactly one delivery per due schedule")
	assert.Equal(t, []string{"tenant-1"}, delivery.tenants)

	due, err := repo.GetScheduleByID(context.Background(), "tenant-1", "due")
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", due.LastStatus)
	assert.NotNil(t, due.LastRunAt)
	assert.True(t, due.NextRunAt.After(now), "next run advanced")

	notDue, err := repo.GetScheduleByID(context.Background(), "tenant-1", "not-due")
	require.NoError(t, err)
	assert.Empty(t, notDue.LastStatus)

	disabled, err := repo.GetScheduleByID(context.Background(), "tenant-1", "disabled")
	require.NoError(t, err)
	assert.Empty(t, disabled.LastStatus)
}

func TestScheduler_RunOnce_DeliveryErrorMarksFailed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewRepository(db)
	delivery := &fakeDelivery{configured: true, err: errors.New("smtp down")}
	svc := NewServiceWithDelivery(repo, delivery)
	sched := NewScheduler(svc, testSchedulerLogger())

	newSchedule(t, repo, "due", "tenant-1", true, time.Now().UTC().Add(-time.Minute))

	_, err := sched.RunOnce(context.Background())
	require.Error(t, err)

	due, err := repo.GetScheduleByID(context.Background(), "tenant-1", "due")
	require.NoError(t, err)
	assert.Equal(t, "FAILED", due.LastStatus)
	assert.Contains(t, due.LastError, "smtp down")
}
