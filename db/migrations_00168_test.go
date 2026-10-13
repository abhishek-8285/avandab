package db_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// The broker credential must survive as a hash only: the plaintext password is
// handed to the device once and never stored, and tenant scope is enforced.
func TestMigration00168_DriverMQTTCredentials_UpAndDown(t *testing.T) {
	name := fmt.Sprintf("test_mig_00168_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
	require.NoError(t, err)
	defer db.Close()

	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.UpTo(db, "migrations", 167))
	require.NoError(t, goose.UpTo(db, "migrations", 168))

	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('t-mqtt', 'MQTT Tenant', 'mqtt-tenant')`)
	require.NoError(t, err)

	hash := "$7$1000$PX/RcVfHzTXxhblp8QOJZOLY9C7yM2eGpSxUKv8SogIWbGBctHYeaT/DjbQsnt6Je4irdTsrgp4CSCQnZQmyhQ==$0wi8zFqX76oh9yGSVSScj1P6TVyNCIzpoHT7jXabBXnjcJt8oWufhOLhBLvSpWxQUSkDD/gbdx6CxBV8YOIQNw=="
	_, err = db.Exec(`INSERT INTO driver_mqtt_credentials (driver_key, tenant_id, username, password_hash)
		VALUES ('drv-42', 't-mqtt', 'drv-42', ?)`, hash)
	require.NoError(t, err)

	var stored string
	require.NoError(t, db.QueryRow(
		`SELECT password_hash FROM driver_mqtt_credentials WHERE driver_key = 'drv-42'`).Scan(&stored))
	require.Equal(t, hash, stored, "broker password hash must round-trip verbatim for the password file")

	// A second tenant cannot claim the same broker username: one broker, one
	// namespace, and a shared username would let either org publish as the other.
	_, err = db.Exec(`INSERT INTO tenants (id, name, slug) VALUES ('t-other', 'Other', 'other')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO driver_mqtt_credentials (driver_key, tenant_id, username, password_hash)
		VALUES ('DRV-42', 't-other', 'drv-42', ?)`, hash)
	require.Error(t, err, "duplicate broker username across tenants must be rejected")

	// Unknown tenant is rejected by the FK trigger, not silently stored.
	_, err = db.Exec(`INSERT INTO driver_mqtt_credentials (driver_key, tenant_id, username, password_hash)
		VALUES ('drv-99', 't-ghost', 'drv-99', ?)`, hash)
	require.Error(t, err, "unknown tenant must be rejected")

	require.NoError(t, goose.DownTo(db, "migrations", 167))
	var count int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('driver_mqtt_credentials')`).Scan(&count))
	require.Equal(t, 0, count, "rollback must drop driver_mqtt_credentials")
	require.NoError(t, goose.UpTo(db, "migrations", 168))
}
