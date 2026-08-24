package ewaybill

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newCompanyConfigDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestIsAutoGenerateEnabled_ReadsCompanyConfig(t *testing.T) {
	db := newCompanyConfigDB(t)

	_, err := db.Exec(`CREATE TABLE company_config (
		tenant_id TEXT NOT NULL DEFAULT '1',
		key TEXT NOT NULL,
		value TEXT,
		updated_at DATETIME,
		PRIMARY KEY (tenant_id, key))`)
	require.NoError(t, err)

	svc := NewEWayBillService(db, nil, nil, nil, Config{})
	ctx := context.Background()

	assert.True(t, svc.isAutoGenerateEnabled(ctx), "missing config row must default to true")

	_, err = db.Exec(`INSERT INTO company_config (tenant_id, key, value) VALUES ('1', 'ewaybill_auto_generate', 'false')`)
	require.NoError(t, err)
	assert.False(t, svc.isAutoGenerateEnabled(ctx), "config value 'false' must disable auto-generate")

	_, err = db.Exec(`UPDATE company_config SET value = 'true' WHERE key = 'ewaybill_auto_generate'`)
	require.NoError(t, err)
	assert.True(t, svc.isAutoGenerateEnabled(ctx))
}
