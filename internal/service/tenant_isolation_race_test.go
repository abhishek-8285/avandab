package service

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"

	"transport-app/internal/events"
	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
	vehiclerepo "transport-app/internal/vehicle/infrastructure/persistence/sql"
)

// TestTenantIsolation_ParallelReads hammers the exact E2E shape that once
// showed cross-tenant rows on CI (fresh tenant seeing sibling specs'
// vehicles): many tenants list concurrently against one shared SQLite file.
// Tenant provisioning stays sequential (parallel writers on one SQLite file
// just deadlock each other — covered by TestRegisterSelfServiceAccount_*
// instead); the parallel phase is pure reads, which must never cross tenant
// boundaries no matter the load. Any foreign row here is a product bug.
func TestTenantIsolation_ParallelReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rwc&cache=shared&_foreign_keys=on&_journal_mode=WAL")
	assert.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	goose.SetLogger(goose.NopLogger())
	assert.NoError(t, goose.SetDialect("sqlite"))
	assert.NoError(t, goose.Up(db, "../../db/migrations"))

	repo := sqliterepo.NewRepository(db)
	svc := NewServices(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())
	vrepo := vehiclerepo.NewVehicleRepository(db)
	ctx := context.Background()

	const workers = 24
	const readsPerWorker = 50

	tenants := make([]string, workers)
	regs := make([]string, workers)
	for i := 0; i < workers; i++ {
		email := fmt.Sprintf("prace-%d-%d@fleet.test", time.Now().UnixNano(), i)
		u, _, err := svc.Users.RegisterSelfServiceAccount(ctx, email,
			fmt.Sprintf("PRacer %d", i), fmt.Sprintf("990199%04d", i),
			"strong-pass-9", "")
		assert.NoError(t, err)
		assert.NotEqual(t, string(shared.DefaultTenant), u.TenantID)
		tenants[i] = u.TenantID
		regs[i] = fmt.Sprintf("PRACE%d%02d", time.Now().UnixNano()%100000, i)
		_, err = db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, tenant_id)
			VALUES (?, ?, ?, 'truck', 18000, ?)`, fmt.Sprintf("prace-veh-%d", i), regs[i], regs[i], tenants[i])
		assert.NoError(t, err)
	}
	seen := map[string]bool{}
	for _, tn := range tenants {
		assert.False(t, seen[tn], "duplicate tenant %q", tn)
		seen[tn] = true
	}

	errCh := make(chan error, workers*readsPerWorker)
	var wg sync.WaitGroup
	// Release all readers at once for maximum overlap.
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for r := 0; r < readsPerWorker; r++ {
				rows, _, err := vrepo.SearchReadModels(ctx, shared.TenantID(tenants[i]), "", "", 50, 0)
				if err != nil {
					errCh <- fmt.Errorf("worker %d read %d: %w", i, r, err)
					return
				}
				if len(rows) != 1 || rows[0].RegistrationNumber != regs[i] {
					got := make([]string, len(rows))
					for k, v := range rows {
						got[k] = v.RegistrationNumber
					}
					errCh <- fmt.Errorf("worker %d (tenant %s) read %d: want exactly [%s], got %v",
						i, tenants[i], r, regs[i], got)
					return
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}
