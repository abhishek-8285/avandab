package facility_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/facility"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_fac_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)

	migrationsDir := "../../db/migrations"
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		for _, cand := range []string{"db/migrations", "../db/migrations", "../../db/migrations"} {
			if _, err := os.Stat(cand); err == nil {
				migrationsDir = cand
				break
			}
		}
	}

	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, migrationsDir))
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestFacilityRepository_CRUDAndTenantIsolation(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "facility" {
		t.Chdir("../..")
	}

	database := newTestDB(t)
	repo := facility.NewSQLRepository(database)
	ctx := context.Background()

	// Seed tenants
	_, err := database.Exec(`
		INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-alpha', 'Alpha Corp', 'alpha');
		INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('tenant-beta', 'Beta Corp', 'beta');
	`)
	require.NoError(t, err)

	lat := 12.9716
	lon := 77.5946

	// 1. Create Facility in tenant-alpha
	facAlpha, err := repo.Create(ctx, "tenant-alpha", facility.CreateFacilityInput{
		FacilityCode: "MM21000000757",
		Name:         "MMS Office - Bangalore",
		FacilityType: facility.FacilityTypeOffice,
		Plant:        "PLANT-BLR",
		Circle:       "Karnataka",
		ProfitCenter: "PC-100",
		CostCenter:   "CC-200",
		Address:      "Museum Road",
		City:         "Bangalore",
		State:        "Karnataka",
		Pincode:      "560001",
		Latitude:     &lat,
		Longitude:    &lon,
	})
	require.NoError(t, err)
	require.NotEmpty(t, facAlpha.ID)
	assert.Equal(t, "MM21000000757", facAlpha.FacilityCode)
	assert.Equal(t, "MMS Office - Bangalore", facAlpha.Name)
	assert.Equal(t, facility.FacilityTypeOffice, facAlpha.FacilityType)
	assert.True(t, facAlpha.IsActive)

	// 2. Duplicate FacilityCode in same tenant MUST fail
	_, err = repo.Create(ctx, "tenant-alpha", facility.CreateFacilityInput{
		FacilityCode: "MM21000000757",
		Name:         "Duplicate Bangalore",
		FacilityType: facility.FacilityTypeDepot,
	})
	require.Error(t, err, "unique constraint per tenant must trigger")

	// 3. Same FacilityCode in DIFFERENT tenant MUST succeed (multi-tenant boundary)
	facBeta, err := repo.Create(ctx, "tenant-beta", facility.CreateFacilityInput{
		FacilityCode: "MM21000000757",
		Name:         "Beta Branch Bangalore",
		FacilityType: facility.FacilityTypeBranch,
	})
	require.NoError(t, err)
	assert.Equal(t, "tenant-beta", facBeta.TenantID)

	// 4. GetByID - Tenant Isolation
	// Alpha can get Alpha's facility
	gotAlpha, err := repo.GetByID(ctx, "tenant-alpha", facAlpha.ID)
	require.NoError(t, err)
	require.NotNil(t, gotAlpha)
	assert.Equal(t, facAlpha.ID, gotAlpha.ID)

	// Beta CANNOT get Alpha's facility
	gotForbidden, err := repo.GetByID(ctx, "tenant-beta", facAlpha.ID)
	require.NoError(t, err)
	assert.Nil(t, gotForbidden, "cross-tenant access must return nil/not found")

	// 5. GetByCode
	gotByCode, err := repo.GetByCode(ctx, "tenant-alpha", "MM21000000757")
	require.NoError(t, err)
	require.NotNil(t, gotByCode)
	assert.Equal(t, "MMS Office - Bangalore", gotByCode.Name)

	// 6. Update Facility
	activeFalse := false
	updated, err := repo.Update(ctx, "tenant-alpha", facAlpha.ID, facility.UpdateFacilityInput{
		Name:         "Bangalore Central Hub",
		FacilityType: facility.FacilityTypeHub,
		Plant:        "PLANT-BLR-02",
		Circle:       "Karnataka South",
		City:         "Bengaluru",
		IsActive:     &activeFalse,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "Bangalore Central Hub", updated.Name)
	assert.Equal(t, facility.FacilityTypeHub, updated.FacilityType)
	assert.False(t, updated.IsActive)

	// 7. List Facilities with filters & pagination
	// Create second facility for tenant-alpha
	_, err = repo.Create(ctx, "tenant-alpha", facility.CreateFacilityInput{
		FacilityCode: "PUN411001",
		Name:         "Pune North Depot",
		FacilityType: facility.FacilityTypeDepot,
		City:         "Pune",
	})
	require.NoError(t, err)

	// List all alpha facilities
	list, total, err := repo.List(ctx, "tenant-alpha", facility.FacilityFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, list, 2)

	// Search filter by city or name
	listSearch, totalSearch, err := repo.List(ctx, "tenant-alpha", facility.FacilityFilter{
		Search: "Pune",
		Limit:  10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalSearch)
	require.Len(t, listSearch, 1)
	assert.Equal(t, "PUN411001", listSearch[0].FacilityCode)

	// Type filter
	listDepot, totalDepot, err := repo.List(ctx, "tenant-alpha", facility.FacilityFilter{
		FacilityType: "depot",
		Limit:        10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalDepot)
	assert.Equal(t, "PUN411001", listDepot[0].FacilityCode)

	// Active filter (activeOnly = true should exclude the inactive hub)
	activeOnly := true
	listActive, totalActive, err := repo.List(ctx, "tenant-alpha", facility.FacilityFilter{
		ActiveOnly: &activeOnly,
		Limit:      10,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalActive)
	assert.Equal(t, "PUN411001", listActive[0].FacilityCode)

	// Beta tenant only sees Beta's 1 facility
	listBeta, totalBeta, err := repo.List(ctx, "tenant-beta", facility.FacilityFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, int64(1), totalBeta)
	assert.Equal(t, "Beta Branch Bangalore", listBeta[0].Name)

	// 8. Delete Facility
	err = repo.Delete(ctx, "tenant-alpha", facAlpha.ID)
	require.NoError(t, err)
	deletedCheck, err := repo.GetByID(ctx, "tenant-alpha", facAlpha.ID)
	require.NoError(t, err)
	assert.Nil(t, deletedCheck)
}
