package telemetry

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"transport-app/internal/shared"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	"transport-app/internal/telemetry/providers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDeviceService builds a DeviceService over a migrated in-memory DB.
func newTestDeviceService(t *testing.T, db *sql.DB) *DeviceService {
	t.Helper()
	return NewDeviceService(
		NewDeviceStore(db),
		NewQuarantineStore(db),
		uow.NewSQLUnitOfWork(db),
		"test-pepper",
		id.NewUUIDGenerator(),
		&testAudit{},
	)
}

func tenantCtx() context.Context {
	return shared.ContextWithTenantID(context.Background(), "1")
}

func TestDeviceService_BulkRegister_AtomicOnExistingDuplicate(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	insertTestDevice(t, db, "EXISTING", DeviceStatusActive, nil)

	cmds := []RegisterDeviceCommand{{IMEI: "NEW1"}, {IMEI: "NEW2"}, {IMEI: "EXISTING"}}
	_, err := svc.BulkRegister(tenantCtx(), cmds)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateDevice)

	// The whole batch rolled back — no partial rows.
	for _, imei := range []string{"NEW1", "NEW2"} {
		n, _ := NewDeviceStore(db).CountByIMEI(tenantCtx(), "1", imei)
		assert.Equal(t, 0, n, "row %s must not exist after rollback", imei)
	}
}

func TestDeviceService_BulkRegister_AtomicOnWithinBatchDuplicate(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)

	cmds := []RegisterDeviceCommand{{IMEI: "A"}, {IMEI: "B"}, {IMEI: "A"}}
	_, err := svc.BulkRegister(tenantCtx(), cmds)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDuplicateDevice)

	for _, imei := range []string{"A", "B"} {
		n, _ := NewDeviceStore(db).CountByIMEI(tenantCtx(), "1", imei)
		assert.Equal(t, 0, n, "row %s must not exist after rollback", imei)
	}
}

func TestDeviceService_BulkRegister_Success(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)

	cmds := []RegisterDeviceCommand{{IMEI: "B1"}, {IMEI: "B2"}}
	results, err := svc.BulkRegister(tenantCtx(), cmds)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, res := range results {
		assert.True(t, res.Success)
		assert.NotEmpty(t, res.DeviceID)
	}
	n, _ := NewDeviceStore(db).CountByTenant(tenantCtx(), "1")
	assert.Equal(t, int64(2), n)
}

func TestDeviceService_ActivateDevice_ProvisionsSecret(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	insertTestVehicle(t, db, "v-1")

	_, err := svc.RegisterDevice(tenantCtx(), RegisterDeviceCommand{IMEI: "ACT1"})
	require.NoError(t, err)
	require.NoError(t, svc.AssignDevice(tenantCtx(), "ACT1", "v-1"))

	res, err := svc.ActivateDevice(tenantCtx(), "ACT1")
	require.NoError(t, err)
	assert.NotEmpty(t, res.RawSecret)

	d, _ := NewDeviceStore(db).GetByIMEI(tenantCtx(), "ACT1")
	require.NotNil(t, d)
	assert.Equal(t, DeviceStatusActive, d.Status)
	require.NotNil(t, d.DeviceSecretHash)
	// The stored hash is the HMAC of the raw secret with the pepper.
	assert.Equal(t, hmacSHA256("test-pepper", res.RawSecret), *d.DeviceSecretHash)
	// The raw secret itself is never persisted.
	assert.NotEqual(t, res.RawSecret, *d.DeviceSecretHash)
}

func TestDeviceService_StateMachineTransitions(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	insertTestVehicle(t, db, "v-1")
	_, _ = svc.RegisterDevice(tenantCtx(), RegisterDeviceCommand{IMEI: "SM1"})

	// inventory → activate is illegal
	_, err := svc.ActivateDevice(tenantCtx(), "SM1")
	assert.ErrorIs(t, err, ErrInvalidTransition)

	// inventory → assigned is legal
	require.NoError(t, svc.AssignDevice(tenantCtx(), "SM1", "v-1"))
	// assigned → assign again is illegal
	assert.ErrorIs(t, svc.AssignDevice(tenantCtx(), "SM1", "v-1"), ErrInvalidTransition)

	// assigned → retired is legal
	require.NoError(t, svc.RetireDevice(tenantCtx(), "SM1"))
	// retired → retire again is illegal
	assert.ErrorIs(t, svc.RetireDevice(tenantCtx(), "SM1"), ErrInvalidTransition)
}

func TestDeviceService_RetireThenIngest_Quarantined(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	insertTestVehicle(t, db, "v-1")
	_, _ = svc.RegisterDevice(tenantCtx(), RegisterDeviceCommand{IMEI: "RET1"})
	require.NoError(t, svc.AssignDevice(tenantCtx(), "RET1", "v-1"))
	_, _ = svc.ActivateDevice(tenantCtx(), "RET1")
	require.NoError(t, svc.RetireDevice(tenantCtx(), "RET1"))

	ing := newTestIngestor(t, db, nil)
	_, err := ing.IngestRawFrame(tenantCtx(), providers.RawFrame{
		IMEI: "RET1", Latitude: 12.97, Longitude: 77.59, Provider: "own",
		ProviderMsgID: "ret1-1", RawPayload: []byte(`{}`), DeviceTime: time.Now(),
	})
	require.NoError(t, err)

	entries, err := NewQuarantineStore(db).ListOpen(tenantCtx(), "1", 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "RET1", entries[0].IMEI)
	assert.Equal(t, QuarantineReasonRetiredDevice, entries[0].Reason)
}

func TestDeviceService_ResolveQuarantine_RegisterNew(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	ing := newTestIngestor(t, db, nil)
	_, _ = ing.IngestRawFrame(tenantCtx(), providers.RawFrame{
		IMEI: "UNK1", Latitude: 12.97, Longitude: 77.59, Provider: "own",
		ProviderMsgID: "unk1-1", RawPayload: []byte(`{"imei":"UNK1"}`), DeviceTime: time.Now(),
	})
	entries, _ := NewQuarantineStore(db).ListOpen(tenantCtx(), "1", 10)
	require.Len(t, entries, 1)

	err := svc.ResolveQuarantine(tenantCtx(), ResolveQuarantineCommand{
		EntryID: entries[0].ID, Action: "register_new", UserID: "admin-1",
	})
	require.NoError(t, err)

	d, _ := NewDeviceStore(db).GetByIMEI(tenantCtx(), "UNK1")
	require.NotNil(t, d)
	assert.Equal(t, DeviceStatusInventory, d.Status)

	e, _ := NewQuarantineStore(db).GetByID(tenantCtx(), entries[0].ID)
	require.NotNil(t, e)
	assert.Equal(t, QuarantineStatusResolved, e.Status)
	require.NotNil(t, e.ResolvedBy)
	assert.Equal(t, "admin-1", *e.ResolvedBy)
}

func TestDeviceService_ResolveQuarantine_Reject(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	ing := newTestIngestor(t, db, nil)
	_, _ = ing.IngestRawFrame(tenantCtx(), providers.RawFrame{
		IMEI: "UNK2", Latitude: 12.97, Longitude: 77.59, Provider: "own",
		ProviderMsgID: "unk2-1", RawPayload: []byte(`{"imei":"UNK2"}`), DeviceTime: time.Now(),
	})
	entries, _ := NewQuarantineStore(db).ListOpen(tenantCtx(), "1", 10)
	require.Len(t, entries, 1)

	require.NoError(t, svc.ResolveQuarantine(tenantCtx(), ResolveQuarantineCommand{
		EntryID: entries[0].ID, Action: "reject", UserID: "admin-1",
	}))
	e, _ := NewQuarantineStore(db).GetByID(tenantCtx(), entries[0].ID)
	require.NotNil(t, e)
	assert.Equal(t, QuarantineStatusRejected, e.Status)
}

func TestDeviceService_ResolveQuarantine_AssignExisting(t *testing.T) {
	db := newTestIngestorDB(t)
	svc := newTestDeviceService(t, db)
	ctx := tenantCtx()

	_, err := svc.RegisterDevice(ctx, RegisterDeviceCommand{IMEI: "AE1"})
	require.NoError(t, err)
	insertTestVehicleReg(t, db, "v-ae", "REG-AE")

	// Inventory device frames → quarantined (non-active).
	ing := newTestIngestor(t, db, nil)
	_, _ = ing.IngestRawFrame(ctx, providers.RawFrame{
		IMEI: "AE1", Latitude: 12.97, Longitude: 77.59, Provider: "own",
		ProviderMsgID: "ae-1", RawPayload: []byte(`{"imei":"AE1"}`), DeviceTime: time.Now(),
	})
	entries, _ := NewQuarantineStore(db).ListOpen(ctx, "1", 10)
	require.Len(t, entries, 1)

	vid := "v-ae"
	require.NoError(t, svc.ResolveQuarantine(ctx, ResolveQuarantineCommand{
		EntryID: entries[0].ID, Action: "assign_existing", VehicleID: &vid, UserID: "admin-1",
	}))

	d, _ := NewDeviceStore(db).GetByIMEI(ctx, "AE1")
	require.NotNil(t, d)
	assert.Equal(t, DeviceStatusAssigned, d.Status)
	require.NotNil(t, d.VehicleID)
	assert.Equal(t, "v-ae", *d.VehicleID)

	e, _ := NewQuarantineStore(db).GetByID(ctx, entries[0].ID)
	require.NotNil(t, e)
	assert.Equal(t, QuarantineStatusResolved, e.Status)
}
