package application

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newConfigSnapshotFixture(t *testing.T) (*sql.DB, *ConfigReader) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	_, err = db.ExecContext(context.Background(), `CREATE TABLE company_config (
		tenant_id TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
		PRIMARY KEY (tenant_id, key))`)
	require.NoError(t, err)
	return db, NewConfigReader(db)
}

func setConfigSnapshotValue(t *testing.T, db *sql.DB, tenant, key, value string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(), `INSERT INTO company_config (tenant_id, key, value)
		VALUES (?, ?, ?) ON CONFLICT (tenant_id, key) DO UPDATE SET value = excluded.value`, tenant, key, value)
	require.NoError(t, err)
}

func TestConfigReader_TTLRefreshReplacesSnapshot(t *testing.T) {
	db, reader := newConfigSnapshotFixture(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	reader.now = func() time.Time { return now }
	ctx := context.Background()
	setConfigSnapshotValue(t, db, "tenant-a", ConfigBufferMetres, "10")
	setConfigSnapshotValue(t, db, "tenant-a", ConfigAutoReachPickup, "true")
	got, err := reader.Get(ctx, "tenant-a", ConfigBufferMetres)
	require.NoError(t, err)
	require.Equal(t, "10", got)
	setConfigSnapshotValue(t, db, "tenant-a", ConfigBufferMetres, "40")
	_, err = db.ExecContext(ctx, `DELETE FROM company_config WHERE tenant_id = ? AND key = ?`, "tenant-a", ConfigAutoReachPickup)
	require.NoError(t, err)
	now = now.Add(cacheTTL - time.Nanosecond)
	got, err = reader.Get(ctx, "tenant-a", ConfigBufferMetres)
	require.NoError(t, err)
	assert.Equal(t, "10", got)
	enabled, err := reader.GetBool(ctx, "tenant-a", ConfigAutoReachPickup, false)
	require.NoError(t, err)
	assert.True(t, enabled)
	now = now.Add(2 * time.Nanosecond)
	got, err = reader.Get(ctx, "tenant-a", ConfigBufferMetres)
	require.NoError(t, err)
	assert.Equal(t, "40", got)
	enabled, err = reader.GetBool(ctx, "tenant-a", ConfigAutoReachPickup, false)
	require.NoError(t, err)
	assert.False(t, enabled)
}

func TestConfigReader_TenantTTLIndependent(t *testing.T) {
	db, reader := newConfigSnapshotFixture(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	reader.now = func() time.Time { return now }
	ctx := context.Background()
	read := func(tenant, want string) {
		t.Helper()
		got, err := reader.Get(ctx, tenant, ConfigBufferMetres)
		require.NoError(t, err)
		assert.Equal(t, want, got, "tenant %s", tenant)
	}
	setConfigSnapshotValue(t, db, "tenant-a", ConfigBufferMetres, "10")
	setConfigSnapshotValue(t, db, "tenant-b", ConfigBufferMetres, "10")
	read("tenant-a", "10")
	now = now.Add(20 * time.Second)
	read("tenant-b", "10")
	setConfigSnapshotValue(t, db, "tenant-a", ConfigBufferMetres, "11")
	setConfigSnapshotValue(t, db, "tenant-b", ConfigBufferMetres, "12")
	now = now.Add(11 * time.Second)
	read("tenant-a", "11")
	read("tenant-b", "10")
	now = now.Add(20 * time.Second)
	read("tenant-b", "12")
	read("tenant-a", "11")
}

func TestConfigReader_TenantSnapshots(t *testing.T) {
	for _, first := range []string{"tenant-a", "tenant-b", "tenant-empty"} {
		t.Run(first, func(t *testing.T) {
			db, reader := newConfigSnapshotFixture(t)
			setConfigSnapshotValue(t, db, "tenant-a", ConfigDwellDebounceSeconds, "2")
			setConfigSnapshotValue(t, db, "tenant-a", ConfigBufferMetres, "10")
			setConfigSnapshotValue(t, db, "tenant-a", ConfigHysteresisMetres, "5")
			setConfigSnapshotValue(t, db, "tenant-b", ConfigDwellDebounceSeconds, "90")
			setConfigSnapshotValue(t, db, "tenant-b", ConfigBufferMetres, "40")
			setConfigSnapshotValue(t, db, "tenant-b", ConfigHysteresisMetres, "35")
			want := map[string]EvaluatorConfig{
				"tenant-a":     {Debounce: 2 * time.Second, BufferMetres: 10, HysteresisMetres: 5, MaxAccuracyMeters: 50, MaxFeasibleSpeedKmh: 180},
				"tenant-b":     {Debounce: 90 * time.Second, BufferMetres: 40, HysteresisMetres: 35, MaxAccuracyMeters: 50, MaxFeasibleSpeedKmh: 180},
				"tenant-empty": DefaultEvaluatorConfig(),
			}
			for _, tenant := range []string{first, "tenant-a", "tenant-b", "tenant-empty", first} {
				assert.Equal(t, want[tenant], LoadEvaluatorConfig(context.Background(), tenant, reader), "tenant %s", tenant)
			}
		})
	}
}
