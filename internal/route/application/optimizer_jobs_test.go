package application

// Characterization + extraction tests for the route-optimization job
// persistence/read model moved out of internal/handlers/routes.go
// (handlers-end-state ADR, Phase 2): tenant isolation of vehicles/jobs,
// pending->processing->completed/failed lifecycle writes, recent-job list
// ordering/limit, and the legacy NullString absent-vs-empty semantics.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const routeOptimizationTestSchema = `
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    registration_number TEXT NOT NULL,
    vehicle_number TEXT NOT NULL,
    model TEXT,
    capacity INTEGER,
    status TEXT NOT NULL DEFAULT 'available'
);
CREATE TABLE route_optimization_jobs (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    input_json TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    provider TEXT,
    created_by TEXT,
    result_json TEXT,
    error_message TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);
`

func newRouteOptimizationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("route_opt_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(routeOptimizationTestSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecRouteOpt(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func TestRouteOptimizationService_VehiclesForOptimization(t *testing.T) {
	db := newRouteOptimizationTestDB(t)
	ctx := context.Background()
	mustExecRouteOpt(t, db, `INSERT INTO vehicles (id, tenant_id, registration_number, vehicle_number, model, capacity, status) VALUES
		('v-b', 'tenant-1', 'REG-B', 'MH-02-B', NULL, 20, 'available'),
		('v-a', 'tenant-1', 'REG-A', 'MH-01-A', 'Tata 407', 10, 'available'),
		('v-blocked', 'tenant-1', 'REG-0', 'MH-00-X', NULL, 5, 'blocked'),
		('v-t2', 'tenant-2', 'REG-C', 'KA-03-C', 'Eicher', 15, 'available')`)

	svc := NewRouteOptimizationService(db)
	vehicles := svc.VehiclesForOptimization(ctx, "tenant-1")
	require.Len(t, vehicles, 2)
	assert.Equal(t, "v-a", vehicles[0].ID, "ordered by registration_number ASC")
	assert.Equal(t, "REG-A", vehicles[0].RegistrationNumber)
	assert.Equal(t, "Tata 407", vehicles[0].Model)
	assert.Equal(t, int64(10), vehicles[0].Capacity)
	assert.Equal(t, "v-b", vehicles[1].ID)
	assert.Empty(t, vehicles[1].Model, "NULL model coalesces to an empty string")

	// Blocked vehicles and other tenants never appear.
	for _, v := range vehicles {
		assert.NotEqual(t, "v-blocked", v.ID)
		assert.NotEqual(t, "v-t2", v.ID)
	}

	assert.Empty(t, svc.VehiclesForOptimization(ctx, "tenant-unknown"))
	assert.Empty(t, NewRouteOptimizationService(nil).VehiclesForOptimization(ctx, "tenant-1"))
}

func TestRouteOptimizationService_CreateJobAndLifecycle(t *testing.T) {
	db := newRouteOptimizationTestDB(t)
	ctx := context.Background()
	svc := NewRouteOptimizationService(db)

	jobID, err := svc.CreateJob(ctx, "tenant-1", `{"shipments":[]}`, "mock", "u-admin")
	require.NoError(t, err)
	require.NotEmpty(t, jobID)

	var tenantID, input, status, provider, createdBy string
	require.NoError(t, db.QueryRow(`SELECT tenant_id, input_json, status, provider, created_by FROM route_optimization_jobs WHERE id = ?`, jobID).
		Scan(&tenantID, &input, &status, &provider, &createdBy))
	assert.Equal(t, "tenant-1", tenantID)
	assert.Equal(t, `{"shipments":[]}`, input)
	assert.Equal(t, "pending", status)
	assert.Equal(t, "mock", provider)
	assert.Equal(t, "u-admin", createdBy)

	// Fresh job: no result/error/completed timestamp yet.
	detail, err := svc.Job(ctx, jobID, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, jobID, detail.ID)
	assert.Equal(t, "pending", detail.Status)
	assert.Equal(t, "mock", detail.Provider)
	assert.False(t, detail.ResultJSON.Valid)
	assert.False(t, detail.ErrorMessage.Valid)
	assert.False(t, detail.CompletedAt.Valid)

	// Foreign tenant id is fail-closed.
	_, err = svc.Job(ctx, jobID, "tenant-2")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	// processing -> completed with result + completion timestamp.
	require.NoError(t, svc.MarkProcessing(ctx, jobID))
	detail, err = svc.Job(ctx, jobID, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "processing", detail.Status)
	assert.False(t, detail.CompletedAt.Valid)

	require.NoError(t, svc.MarkCompleted(ctx, jobID, `{"provider":"mock"}`))
	detail, err = svc.Job(ctx, jobID, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "completed", detail.Status)
	assert.True(t, detail.ResultJSON.Valid)
	assert.JSONEq(t, `{"provider":"mock"}`, detail.ResultJSON.String)
	assert.True(t, detail.CompletedAt.Valid)

	// failed path records the solver error and completion timestamp.
	failedID, err := svc.CreateJob(ctx, "tenant-1", `{}`, "osrm-public", "u-admin")
	require.NoError(t, err)
	require.NoError(t, svc.MarkProcessing(ctx, failedID))
	require.NoError(t, svc.MarkFailed(ctx, failedID, "solver exploded"))
	failedDetail, err := svc.Job(ctx, failedID, "tenant-1")
	require.NoError(t, err)
	assert.Equal(t, "failed", failedDetail.Status)
	assert.True(t, failedDetail.ErrorMessage.Valid)
	assert.Equal(t, "solver exploded", failedDetail.ErrorMessage.String)
	assert.True(t, failedDetail.CompletedAt.Valid)
}

func TestRouteOptimizationService_RecentJobsTenantScopedOrdering(t *testing.T) {
	db := newRouteOptimizationTestDB(t)
	ctx := context.Background()
	// 21 jobs for the tenant (one over the 20-row cap) plus another tenant.
	for i := 1; i <= 21; i++ {
		mustExecRouteOpt(t, db, `INSERT INTO route_optimization_jobs (id, tenant_id, status, provider, created_at)
			VALUES (?, 'tenant-1', 'completed', 'mock', ?)`,
			fmt.Sprintf("j-%02d", i), fmt.Sprintf("2026-01-%02d 00:00:00", i))
	}
	mustExecRouteOpt(t, db, `INSERT INTO route_optimization_jobs (id, tenant_id, status, provider, created_at, completed_at)
		VALUES ('j-other', 'tenant-2', 'completed', 'mock', '2026-02-01 00:00:00', '2026-02-01 01:00:00')`)
	mustExecRouteOpt(t, db, `UPDATE route_optimization_jobs SET completed_at = '2026-01-21 01:00:00' WHERE id = 'j-21'`)

	svc := NewRouteOptimizationService(db)
	jobs, err := svc.RecentJobs(ctx, "tenant-1")
	require.NoError(t, err)
	require.Len(t, jobs, 20, "legacy limit is 20")
	assert.Equal(t, "j-21", jobs[0].ID, "newest first")
	assert.Equal(t, "j-02", jobs[19].ID)
	assert.Contains(t, jobs[0].CreatedAt, "2026-01-21")
	require.NotNil(t, jobs[0].CompletedAt)
	assert.Contains(t, *jobs[0].CompletedAt, "2026-01-21")
	assert.Nil(t, jobs[1].CompletedAt, "NULL completed_at stays omitted")
	for _, job := range jobs {
		assert.NotEqual(t, "j-other", job.ID, "other tenant's jobs never appear")
	}

	otherTenant, err := svc.RecentJobs(ctx, "tenant-2")
	require.NoError(t, err)
	require.Len(t, otherTenant, 1)
	assert.Equal(t, "j-other", otherTenant[0].ID)
}

func TestRouteOptimizationService_JobDetailNullValidity(t *testing.T) {
	db := newRouteOptimizationTestDB(t)
	ctx := context.Background()
	// Legacy validity semantics: NULL columns are absent, while an empty
	// string written to a nullable text column is still "present".
	mustExecRouteOpt(t, db, `INSERT INTO route_optimization_jobs (id, tenant_id, status, provider, result_json, error_message, created_at)
		VALUES ('j-null', 'tenant-1', 'completed', 'mock', NULL, NULL, '2026-01-01 00:00:00')`)
	mustExecRouteOpt(t, db, `INSERT INTO route_optimization_jobs (id, tenant_id, status, provider, result_json, error_message, created_at)
		VALUES ('j-empty', 'tenant-1', 'failed', 'mock', '{}', '', '2026-01-01 00:00:00')`)

	svc := NewRouteOptimizationService(db)
	nullDetail, err := svc.Job(ctx, "j-null", "tenant-1")
	require.NoError(t, err)
	assert.False(t, nullDetail.ResultJSON.Valid)
	assert.False(t, nullDetail.ErrorMessage.Valid)

	emptyDetail, err := svc.Job(ctx, "j-empty", "tenant-1")
	require.NoError(t, err)
	assert.True(t, emptyDetail.ResultJSON.Valid)
	assert.True(t, emptyDetail.ErrorMessage.Valid, "empty string is a valid (present) error_message")
	assert.Empty(t, emptyDetail.ErrorMessage.String)

	// Unknown job id surfaces sql.ErrNoRows (handler maps to 404).
	_, err = svc.Job(ctx, "j-missing", "tenant-1")
	assert.ErrorIs(t, err, sql.ErrNoRows)
}
