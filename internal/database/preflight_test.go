package database_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	appdb "transport-app/internal/database"
)

func TestCountOrphanTenantUsers(t *testing.T) {
	name := fmt.Sprintf("preflight_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE tenants (id TEXT PRIMARY KEY, status TEXT NOT NULL DEFAULT 'active');
		CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL DEFAULT '');
		INSERT INTO tenants (id) VALUES ('1');
		INSERT INTO users (id, tenant_id) VALUES ('u-valid', '1');
		INSERT INTO users (id, tenant_id) VALUES ('u-orphan-1', 'ws_deadbeef');
		INSERT INTO users (id, tenant_id) VALUES ('u-orphan-2', 'tenant_missing');
		INSERT INTO users (id, tenant_id) VALUES ('u-empty', '');
	`)
	require.NoError(t, err)

	n, err := appdb.CountOrphanTenantUsers(context.Background(), db)
	require.NoError(t, err)
	assert.Equal(t, 2, n, "only users with a non-empty tenant_id and no tenants row count")
}
