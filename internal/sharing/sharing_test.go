package sharing

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func newShareWriteDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("sharing_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, driver_id TEXT NOT NULL DEFAULT '');
		CREATE TABLE share_links (
			id TEXT PRIMARY KEY, trip_id TEXT NOT NULL, token_hash TEXT NOT NULL,
			pin_hash TEXT, pin_salt TEXT, created_by TEXT NOT NULL,
			created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL,
			view_count INTEGER NOT NULL DEFAULT 0, last_viewed_at DATETIME,
			failed_pin_attempts INTEGER NOT NULL DEFAULT 0, locked_until DATETIME,
			revoked_at DATETIME);
		CREATE TABLE drivers (id TEXT PRIMARY KEY, driver_id TEXT NOT NULL DEFAULT '', tenant_id TEXT NOT NULL, email TEXT NOT NULL DEFAULT '');
		CREATE TABLE users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT NOT NULL DEFAULT '')`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedShareLink(t *testing.T, db *sql.DB, id, tripID, tokenHash string, expiresAt time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO share_links (id, trip_id, token_hash, created_by, created_at, expires_at)
		VALUES (?, ?, ?, 'user-1', ?, ?)`, id, tripID, tokenHash, time.Now().UTC(), expiresAt)
	require.NoError(t, err)
}

func TestCreate_EnforcesTripTenantAndActiveCap(t *testing.T) {
	db := newShareWriteDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-1', 'tenant-1'), ('trip-2', 'tenant-2')`)
	require.NoError(t, err)
	svc := New(db)
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)

	// Wrong tenant and unknown trip both surface as ErrTripNotFound (the
	// pre-extraction handler answered 404 for either).
	created, err := svc.Create(ctx, "tenant-2", "trip-1", "link-x", "hash-x", nil, nil, "user-1", now, expiresAt, 5)
	assert.False(t, created)
	assert.ErrorIs(t, err, ErrTripNotFound)
	created, err = svc.Create(ctx, "tenant-1", "missing-trip", "link-x", "hash-x", nil, nil, "user-1", now, expiresAt, 5)
	assert.False(t, created)
	assert.ErrorIs(t, err, ErrTripNotFound)
	var stored int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM share_links`).Scan(&stored))
	assert.Zero(t, stored, "rejected creates must not insert rows")

	// First link fills a cap of one.
	pinHash, pinSalt := "pin-hash", "pin-salt"
	created, err = svc.Create(ctx, "tenant-1", "trip-1", "link-1", "hash-1", &pinHash, &pinSalt, "user-1", now, expiresAt, 1)
	require.NoError(t, err)
	assert.True(t, created)

	var gotTokenHash, gotPinHash, gotPinSalt string
	require.NoError(t, db.QueryRow(`SELECT token_hash, pin_hash, pin_salt FROM share_links WHERE id = 'link-1'`).
		Scan(&gotTokenHash, &gotPinHash, &gotPinSalt))
	assert.Equal(t, "hash-1", gotTokenHash, "only the token hash is stored")
	assert.Equal(t, pinHash, gotPinHash)
	assert.Equal(t, pinSalt, gotPinSalt)

	// Second link exceeds the cap -> ErrMaxActive and no extra row.
	created, err = svc.Create(ctx, "tenant-1", "trip-1", "link-2", "hash-2", nil, nil, "user-1", now, expiresAt, 1)
	assert.False(t, created)
	assert.ErrorIs(t, err, ErrMaxActive)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM share_links WHERE trip_id = 'trip-1'`).Scan(&stored))
	assert.Equal(t, 1, stored)

	// A revoked link no longer counts toward the cap.
	_, err = svc.Revoke(ctx, "tenant-1", "link-1")
	require.NoError(t, err)
	created, err = svc.Create(ctx, "tenant-1", "trip-1", "link-2", "hash-2", nil, nil, "user-1", now, expiresAt, 1)
	require.NoError(t, err)
	assert.True(t, created)
}

func TestCreate_InsertFailureWrapsSentinel(t *testing.T) {
	db := newShareWriteDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-1', 'tenant-1')`)
	require.NoError(t, err)
	svc := New(db)
	now := time.Now().UTC()
	expiresAt := now.Add(time.Hour)

	created, err := svc.Create(ctx, "tenant-1", "trip-1", "dup-link", "hash-1", nil, nil, "user-1", now, expiresAt, 10)
	require.NoError(t, err)
	assert.True(t, created)

	created, err = svc.Create(ctx, "tenant-1", "trip-1", "dup-link", "hash-2", nil, nil, "user-1", now, expiresAt, 10)
	assert.False(t, created)
	assert.ErrorIs(t, err, ErrLinkCreate, "insert failures stay distinguishable so the handler keeps its 500 message")
}

func TestRecordView_ExtendsExpiryAndCountsViews(t *testing.T) {
	db := newShareWriteDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-1', 'tenant-1')`)
	require.NoError(t, err)
	svc := New(db)
	now := time.Now().UTC()
	seedShareLink(t, db, "link-view", "trip-1", "hash-view", now.Add(time.Hour))

	newExpiry := now.Add(48 * time.Hour)
	require.NoError(t, svc.RecordView(ctx, "link-view", newExpiry, now))
	require.NoError(t, svc.RecordView(ctx, "link-view", newExpiry, now))

	var gotExpiry time.Time
	var viewCount int
	var lastViewed sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT expires_at, view_count, last_viewed_at FROM share_links WHERE id = 'link-view'`).
		Scan(&gotExpiry, &viewCount, &lastViewed))
	assert.Equal(t, 2, viewCount)
	assert.True(t, lastViewed.Valid)
	assert.Equal(t, newExpiry.Unix(), gotExpiry.UTC().Unix())
}

func TestRecordPINFailure_And_Success(t *testing.T) {
	db := newShareWriteDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-1', 'tenant-1')`)
	require.NoError(t, err)
	svc := New(db)
	now := time.Now().UTC()
	seedShareLink(t, db, "link-pin", "trip-1", "hash-pin", now.Add(time.Hour))

	lockedUntil := now.Add(15 * time.Minute)
	require.NoError(t, svc.RecordPINFailure(ctx, "link-pin", 5, &lockedUntil))
	var attempts int
	var lock sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT failed_pin_attempts, locked_until FROM share_links WHERE id = 'link-pin'`).
		Scan(&attempts, &lock))
	assert.Equal(t, 5, attempts)
	require.True(t, lock.Valid)
	assert.Equal(t, lockedUntil.Unix(), lock.Time.UTC().Unix())

	newExpiry := now.Add(72 * time.Hour)
	require.NoError(t, svc.RecordPINSuccess(ctx, "link-pin", newExpiry))
	var gotExpiry time.Time
	require.NoError(t, db.QueryRow(`SELECT failed_pin_attempts, locked_until, expires_at FROM share_links WHERE id = 'link-pin'`).
		Scan(&attempts, &lock, &gotExpiry))
	assert.Zero(t, attempts, "success clears the brute-force counter")
	assert.False(t, lock.Valid, "success clears the lockout")
	assert.Equal(t, newExpiry.Unix(), gotExpiry.UTC().Unix(), "success keeps the sliding-expiry behavior")
}

func TestRevoke_TenantScoped(t *testing.T) {
	db := newShareWriteDB(t)
	ctx := context.Background()
	_, err := db.Exec(`INSERT INTO trips (id, tenant_id) VALUES ('trip-1', 'tenant-1'), ('trip-2', 'tenant-2')`)
	require.NoError(t, err)
	svc := New(db)
	now := time.Now().UTC()
	seedShareLink(t, db, "link-a", "trip-1", "hash-a", now.Add(time.Hour))
	seedShareLink(t, db, "link-b", "trip-2", "hash-b", now.Add(time.Hour))

	revoked, err := svc.Revoke(ctx, "tenant-2", "link-a")
	require.NoError(t, err)
	assert.False(t, revoked, "cross-tenant revoke matches no row")
	var revokedAt sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT revoked_at FROM share_links WHERE id = 'link-a'`).Scan(&revokedAt))
	assert.False(t, revokedAt.Valid)

	revoked, err = svc.Revoke(ctx, "tenant-1", "link-a")
	require.NoError(t, err)
	assert.True(t, revoked)
	require.NoError(t, db.QueryRow(`SELECT revoked_at FROM share_links WHERE id = 'link-a'`).Scan(&revokedAt))
	assert.True(t, revokedAt.Valid)

	revoked, err = svc.Revoke(ctx, "tenant-1", "missing-link")
	require.NoError(t, err)
	assert.False(t, revoked)
}

func TestIsDriverOnTrip(t *testing.T) {
	db := newShareWriteDB(t)
	ctx := context.Background()
	_, err := db.Exec(`
		INSERT INTO trips (id, tenant_id, driver_id) VALUES
			('trip-direct', 'tenant-1', 'drv-1'),
			('trip-code', 'tenant-1', 'DRV-CODE'),
			('trip-empty', 'tenant-1', ''),
			('trip-other-tenant', 'tenant-2', 'drv-1');
		INSERT INTO drivers (id, driver_id, tenant_id, email) VALUES
			('drv-1', 'DRV-CODE', 'tenant-1', 'driver@test.com');
		INSERT INTO users (id, tenant_id, email) VALUES
			('user-driver', 'tenant-1', 'driver@test.com')`)
	require.NoError(t, err)
	svc := New(db)

	cases := []struct {
		name     string
		tenantID string
		tripID   string
		userID   string
		want     bool
	}{
		{"row id matches driver_id column", "tenant-1", "trip-direct", "drv-1", true},
		{"driver row id matches human code", "tenant-1", "trip-code", "drv-1", true},
		{"driver email matches auth user", "tenant-1", "trip-code", "user-driver", true},
		{"unrelated user", "tenant-1", "trip-code", "user-other", false},
		{"cross-tenant trip hidden", "tenant-2", "trip-direct", "drv-1", false},
		{"trip without driver", "tenant-1", "trip-empty", "drv-1", false},
		{"unknown trip", "tenant-1", "missing-trip", "drv-1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := svc.IsDriverOnTrip(ctx, tc.tenantID, tc.tripID, tc.userID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNilDBIsSafe(t *testing.T) {
	svc := New(nil)
	ctx := context.Background()
	now := time.Now().UTC()
	created, err := svc.Create(ctx, "tenant-1", "trip-1", "link-1", "hash-1", nil, nil, "user-1", now, now.Add(time.Hour), 5)
	assert.False(t, created)
	assert.ErrorIs(t, err, errNoDB)
	assert.ErrorIs(t, svc.RecordView(ctx, "link-1", now, now), errNoDB)
	assert.ErrorIs(t, svc.RecordPINFailure(ctx, "link-1", 1, nil), errNoDB)
	assert.ErrorIs(t, svc.RecordPINSuccess(ctx, "link-1", now), errNoDB)
	revoked, err := svc.Revoke(ctx, "tenant-1", "link-1")
	assert.False(t, revoked)
	assert.ErrorIs(t, err, errNoDB)
	onTrip, err := svc.IsDriverOnTrip(ctx, "tenant-1", "trip-1", "user-1")
	assert.False(t, onTrip)
	assert.ErrorIs(t, err, errNoDB)
}
