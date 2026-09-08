package handlers

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"transport-app/internal/domain"
)

func linkTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, ddl := range []string{
		`CREATE TABLE vehicles (id TEXT PRIMARY KEY, registration_number TEXT NOT NULL UNIQUE, vehicle_number TEXT, vehicle_type TEXT, capacity INTEGER, fuel_type TEXT, insurance_expiry TEXT, fitness_expiry TEXT, permit_expiry TEXT, status TEXT, tenant_id TEXT, updated_at TEXT)`,
		`CREATE TABLE drivers (id TEXT PRIMARY KEY, driver_id TEXT UNIQUE, first_name TEXT, last_name TEXT, phone TEXT, email TEXT, license_number TEXT, license_expiry TEXT, status TEXT, notes TEXT, tenant_id TEXT, updated_at TEXT)`,
		`CREATE TABLE telemetry_devices (id TEXT PRIMARY KEY, tenant_id TEXT, imei TEXT UNIQUE, device_type TEXT, status TEXT, vehicle_id TEXT, activated_at TEXT)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func linkUser(id string) domain.User {
	return domain.User{ID: domain.UserID(id)}
}

// No fabricated placeholder: driver signup without a vehicle number creates
// the driver + device rows and NO vehicle.
func TestLinkDriverProfile_NoNumberNoVehicle(t *testing.T) {
	db := linkTestDB(t)
	h := NewAPIAuthHandler(nil, nil, []byte("test-secret"), db)
	if err := h.linkDriverProfile(context.Background(), linkUser("u1"), "Asha Kumar", "999", "a@x.com", "", "tenant_A"); err != nil {
		t.Fatalf("link = %v, want nil", err)
	}
	var vehicles, drivers, devices int
	_ = db.QueryRow(`SELECT COUNT(*) FROM vehicles`).Scan(&vehicles)
	_ = db.QueryRow(`SELECT COUNT(*) FROM drivers`).Scan(&drivers)
	_ = db.QueryRow(`SELECT COUNT(*) FROM telemetry_devices`).Scan(&devices)
	if vehicles != 0 || drivers != 1 || devices != 1 {
		t.Fatalf("vehicles=%d drivers=%d devices=%d, want 0/1/1", vehicles, drivers, devices)
	}
	var lic sql.NullString
	var vid sql.NullString
	_ = db.QueryRow(`SELECT license_number, license_expiry FROM drivers`).Scan(&lic, &vid)
	if lic.Valid {
		t.Fatalf("license_number = %q, want NULL", lic.String)
	}
	_ = db.QueryRow(`SELECT vehicle_id FROM telemetry_devices`).Scan(&vid)
	if vid.Valid {
		t.Fatalf("device vehicle_id = %q, want NULL", vid.String)
	}
}

// Explicit number provisions all three rows bound to the caller's tenant.
func TestLinkDriverProfile_ExplicitNumberBindsOwnTenant(t *testing.T) {
	db := linkTestDB(t)
	h := NewAPIAuthHandler(nil, nil, []byte("test-secret"), db)
	if err := h.linkDriverProfile(context.Background(), linkUser("u2"), "Ben Shah", "888", "b@x.com", "MH01AB1234", "tenant_B"); err != nil {
		t.Fatalf("link = %v, want nil", err)
	}
	var tenant, reg string
	if err := db.QueryRow(`SELECT tenant_id, registration_number FROM vehicles`).Scan(&tenant, &reg); err != nil {
		t.Fatalf("vehicle missing: %v", err)
	}
	if tenant != "tenant_B" || reg != "MH01AB1234" {
		t.Fatalf("vehicle = %s/%s, want tenant_B/MH01AB1234", tenant, reg)
	}
	var devTenant, devVehicle string
	if err := db.QueryRow(`SELECT tenant_id, vehicle_id FROM telemetry_devices`).Scan(&devTenant, &devVehicle); err != nil {
		t.Fatalf("device missing: %v", err)
	}
	var vehicleID string
	_ = db.QueryRow(`SELECT id FROM vehicles`).Scan(&vehicleID)
	if devTenant != "tenant_B" || devVehicle != vehicleID {
		t.Fatalf("device bound to %s/%s, want tenant_B/%s", devTenant, devVehicle, vehicleID)
	}
}

// Fail closed: a number owned by another tenant provisions NOTHING for the
// new tenant (global UNIQUE would otherwise collide or latch cross-tenant).
func TestLinkDriverProfile_ForeignNumberFailsClosed(t *testing.T) {
	db := linkTestDB(t)
	if _, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, tenant_id) VALUES ('v9','KA05ZZ9999','KA05ZZ9999','tenant_X')`); err != nil {
		t.Fatal(err)
	}
	h := NewAPIAuthHandler(nil, nil, []byte("test-secret"), db)
	if err := h.linkDriverProfile(context.Background(), linkUser("u3"), "Cara Rao", "777", "c@x.com", "KA05ZZ9999", "tenant_Y"); err == nil {
		t.Fatal("link = nil, want error for foreign-owned number")
	}
	for tbl, want := range map[string]int{"drivers": 0, "telemetry_devices": 0} {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM ` + tbl + ` WHERE tenant_id = 'tenant_Y'`).Scan(&n)
		if n != want {
			t.Fatalf("%s rows for tenant_Y = %d, want %d (rollback broken)", tbl, n, want)
		}
	}
}
