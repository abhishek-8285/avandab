package test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/service"
	"transport-app/internal/shared"
)

func itoa(i int) string { return strconv.Itoa(i) }

func ptrTime(t time.Time) *time.Time { return &t }

func addDays(d int) time.Time { return time.Now().AddDate(0, 0, d) }

// newRunningExperiment creates + starts an experiment with the given traffic split.
func newRunningExperiment(t *testing.T, svc *service.ExperimentsService, tenant, name string, split float64) string {
	t.Helper()
	id, err := svc.CreateExperiment(shared.ContextWithTenantID(context.Background(), "1"), service.Experiment{
		TenantID:     tenant,
		Name:         name,
		TrafficSplit: split,
		MetricName:   "conversion_rate",
		CreatedBy:    "tester",
	})
	require.NoError(t, err)
	require.NoError(t, svc.StartExperiment(shared.ContextWithTenantID(context.Background(), "1"), id))
	return id
}

func TestExperiment_CreateAndLifecycle(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	tenant := "1"

	id, err := svc.CreateExperiment(ctx, service.Experiment{TenantID: tenant, Name: "layout_test", TrafficSplit: 50})
	require.NoError(t, err)

	exp, err := svc.GetExperiment(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, service.ExperimentStatusDraft, exp.Status)

	require.NoError(t, svc.StartExperiment(ctx, id))
	exp, _ = svc.GetExperiment(ctx, id)
	assert.Equal(t, service.ExperimentStatusRunning, exp.Status)

	require.NoError(t, svc.PauseExperiment(ctx, id))
	exp, _ = svc.GetExperiment(ctx, id)
	assert.Equal(t, service.ExperimentStatusPaused, exp.Status)

	require.NoError(t, svc.ResumeExperiment(ctx, id))
	exp, _ = svc.GetExperiment(ctx, id)
	assert.Equal(t, service.ExperimentStatusRunning, exp.Status)

	require.NoError(t, svc.CompleteExperiment(ctx, id))
	exp, _ = svc.GetExperiment(ctx, id)
	assert.Equal(t, service.ExperimentStatusCompleted, exp.Status)

	require.NoError(t, svc.ArchiveExperiment(ctx, id))
	exp, _ = svc.GetExperiment(ctx, id)
	assert.Equal(t, service.ExperimentStatusArchived, exp.Status)
}

func TestExperiment_InvalidTransitions(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Cannot start a running experiment.
	running := newRunningExperiment(t, svc, "1", "x1", 50)
	require.Error(t, svc.StartExperiment(ctx, running))

	// Cannot pause a draft experiment.
	draftID, _ := svc.CreateExperiment(ctx, service.Experiment{TenantID: "1", Name: "x2", TrafficSplit: 50})
	require.Error(t, svc.PauseExperiment(ctx, draftID))

	// Cannot complete a draft experiment.
	require.Error(t, svc.CompleteExperiment(ctx, draftID))

	// Cannot archive a running experiment.
	require.Error(t, svc.ArchiveExperiment(ctx, running))
}

func TestExperiment_DeterministicAssignment(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	id := newRunningExperiment(t, svc, "1", "det", 50)

	// Same subject → same variant across repeated calls.
	v1, err := svc.AssignVariant(ctx, "1", id, service.SubjectTypeUser, "u-1")
	require.NoError(t, err)
	v2, err := svc.AssignVariant(ctx, "1", id, service.SubjectTypeUser, "u-1")
	require.NoError(t, err)
	assert.Equal(t, v1, v2, "assignment must be idempotent/deterministic")

	// GetAssignment returns the persisted variant.
	stored, err := svc.GetAssignment(ctx, id, service.SubjectTypeUser, "u-1")
	require.NoError(t, err)
	assert.Equal(t, v1, stored)

	// Distribution sanity: 200 distinct subjects should land on both variants.
	a, b := 0, 0
	for i := 0; i < 200; i++ {
		v, err := svc.AssignVariant(ctx, "1", id, service.SubjectTypeUser, "dist-"+itoa(i))
		require.NoError(t, err)
		if v == service.VariantA {
			a++
		} else {
			b++
		}
	}
	assert.Greater(t, a, 0, "expected some control assignments")
	assert.Greater(t, b, 0, "expected some treatment assignments")
}

func TestExperiment_TrafficSplit(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// traffic_split=100 → all variant B.
	allB := newRunningExperiment(t, svc, "1", "split100", 100)
	for i := 0; i < 10; i++ {
		v, err := svc.AssignVariant(ctx, "1", allB, service.SubjectTypeUser, "b"+itoa(i))
		require.NoError(t, err)
		assert.Equal(t, service.VariantB, v)
	}

	// traffic_split=0 → all variant A.
	allA := newRunningExperiment(t, svc, "1", "split0", 0)
	for i := 0; i < 10; i++ {
		v, err := svc.AssignVariant(ctx, "1", allA, service.SubjectTypeUser, "a"+itoa(i))
		require.NoError(t, err)
		assert.Equal(t, service.VariantA, v)
	}
}

func TestExperiment_OnlyRunningAssigns(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	draftID, _ := svc.CreateExperiment(ctx, service.Experiment{TenantID: "1", Name: "onlyrun", TrafficSplit: 50})
	_, err := svc.AssignVariant(ctx, "1", draftID, service.SubjectTypeUser, "u")
	assert.Error(t, err)

	pausedID := newRunningExperiment(t, svc, "1", "p", 50)
	require.NoError(t, svc.PauseExperiment(ctx, pausedID))
	_, err = svc.AssignVariant(ctx, "1", pausedID, service.SubjectTypeUser, "u")
	assert.Error(t, err)

	completedID := newRunningExperiment(t, svc, "1", "c", 50)
	require.NoError(t, svc.CompleteExperiment(ctx, completedID))
	_, err = svc.AssignVariant(ctx, "1", completedID, service.SubjectTypeUser, "u")
	assert.Error(t, err)

	// Running experiment assigns.
	runningID := newRunningExperiment(t, svc, "1", "r", 50)
	v, err := svc.AssignVariant(ctx, "1", runningID, service.SubjectTypeUser, "u")
	require.NoError(t, err)
	assert.Contains(t, []string{service.VariantA, service.VariantB}, v)
}

func TestExperiment_FeatureFlagEvaluation(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Non-existent experiment → control.
	assert.Equal(t, service.VariantA, svc.EvaluateFeatureFlag(ctx, "1", "does_not_exist", service.SubjectTypeUser, "u"))
	assert.False(t, svc.IsInTreatment(ctx, "1", "does_not_exist", service.SubjectTypeUser, "u"))

	// Non-running experiment → control.
	draftID, _ := svc.CreateExperiment(ctx, service.Experiment{TenantID: "1", Name: "ff_draft", TrafficSplit: 100})
	assert.Equal(t, service.VariantA, svc.EvaluateFeatureFlag(ctx, "1", "ff_draft", service.SubjectTypeUser, "u"))

	// Running experiment → consistent variant.
	runningID := newRunningExperiment(t, svc, "1", "ff_run", 100)
	v1 := svc.EvaluateFeatureFlag(ctx, "1", "ff_run", service.SubjectTypeUser, "u")
	v2 := svc.EvaluateFeatureFlag(ctx, "1", "ff_run", service.SubjectTypeUser, "u")
	assert.Equal(t, v1, v2)
	assert.Equal(t, service.VariantB, v1)
	assert.True(t, svc.IsInTreatment(ctx, "1", "ff_run", service.SubjectTypeUser, "u"))
	_ = draftID
	_ = runningID
}

func TestExperiment_DateBounds(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Future start_date → assignment fails.
	futureID, _ := svc.CreateExperiment(ctx, service.Experiment{
		TenantID: "1", Name: "future", TrafficSplit: 50,
		StartDate: ptrTime(addDays(1)),
	})
	require.NoError(t, svc.StartExperiment(ctx, futureID))
	_, err := svc.AssignVariant(ctx, "1", futureID, service.SubjectTypeUser, "u")
	assert.Error(t, err)

	// Past end_date → assignment fails.
	pastID, _ := svc.CreateExperiment(ctx, service.Experiment{
		TenantID: "1", Name: "past", TrafficSplit: 50,
		EndDate: ptrTime(addDays(-1)),
	})
	require.NoError(t, svc.StartExperiment(ctx, pastID))
	_, err = svc.AssignVariant(ctx, "1", pastID, service.SubjectTypeUser, "u")
	assert.Error(t, err)

	// Within range → assignment succeeds.
	inRangeID, _ := svc.CreateExperiment(ctx, service.Experiment{
		TenantID: "1", Name: "inrange", TrafficSplit: 50,
		StartDate: ptrTime(addDays(-1)),
		EndDate:   ptrTime(addDays(1)),
	})
	require.NoError(t, svc.StartExperiment(ctx, inRangeID))
	v, err := svc.AssignVariant(ctx, "1", inRangeID, service.SubjectTypeUser, "u")
	require.NoError(t, err)
	assert.Contains(t, []string{service.VariantA, service.VariantB}, v)
}

func TestExperiment_MetricRecording(t *testing.T) {
	svc := NewTestServices(t, NewTestDB(t)).Experiments
	ctx := shared.ContextWithTenantID(context.Background(), "1")
	id := newRunningExperiment(t, svc, "1", "metrics", 50)

	// Assign a subject, then record a metric.
	_, err := svc.AssignVariant(ctx, "1", id, service.SubjectTypeUser, "m-1")
	require.NoError(t, err)
	require.NoError(t, svc.RecordMetric(ctx, "1", id, service.SubjectTypeUser, "m-1", 0.87))

	// Unassigned subject cannot record.
	err = svc.RecordMetric(ctx, "1", id, service.SubjectTypeUser, "m-2", 0.5)
	assert.Error(t, err)

	// Record a second metric in the same variant for aggregation.
	_, err = svc.AssignVariant(ctx, "1", id, service.SubjectTypeUser, "m-3")
	require.NoError(t, err)
	require.NoError(t, svc.RecordMetric(ctx, "1", id, service.SubjectTypeUser, "m-3", 0.13))

	results, err := svc.GetExperimentResults(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, 2, results.TotalSubjects)
	assert.GreaterOrEqual(t, results.VariantA.Count+results.VariantB.Count, 2)
}
