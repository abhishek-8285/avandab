package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/repository/sqlite"
	"transport-app/internal/shared"
)

func newPODOTPTestService(t *testing.T) (*TripService, *sql.DB) {
	t.Helper()
	name := fmt.Sprintf("test_podotp_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	dbConn, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	require.NoError(t, goose.SetDialect("sqlite"))
	require.NoError(t, goose.Up(dbConn, "../../db/migrations"))
	_, err = dbConn.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('t-otp','OTP Tenant','otp')`)
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('rt-otp', 'Mumbai', 'Pune', 150, 4, 5000, 't-otp')`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	svc := &TripService{baseService: baseService{store: sqlite.NewRepository(dbConn)}}
	return svc, dbConn
}

func seedOTPTrip(t *testing.T, db *sql.DB, id, otp, expires string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, pod_otp, pod_otp_expires_at)
		VALUES (?, ?, 'rt-otp', datetime('now'), 'scheduled', 't-otp', ?, ?)`, id, "TRP-"+id, otp, expires)
	require.NoError(t, err)
}

func TestVerifyPODOTP_FailClosed(t *testing.T) {
	svc, db := newPODOTPTestService(t)
	ctx := shared.ContextWithTenantID(context.Background(), "t-otp")
	futureRFC := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	// Zone-less walls are interpreted as UTC (matches datetime('now')
	// producers); generate them from UTC, never local wall time.
	futureSQL := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	pastRFC := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	pastSQL := time.Now().UTC().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")

	seedOTPTrip(t, db, "trip-sqlite", "123456", futureSQL)
	seedOTPTrip(t, db, "trip-rfc", "123456", futureRFC)
	seedOTPTrip(t, db, "trip-exp-rfc", "123456", pastRFC)
	seedOTPTrip(t, db, "trip-exp-sql", "123456", pastSQL)
	seedOTPTrip(t, db, "trip-garbage", "123456", "not-a-timestamp")
	seedOTPTrip(t, db, "trip-legacy", "", "")

	// Wrong code rejected in both stored formats.
	var verified bool
	require.ErrorIs(t, svc.verifyPODOTP(ctx, "trip-sqlite", "000000", &verified), ErrPODOTPRequired)
	require.ErrorIs(t, svc.verifyPODOTP(ctx, "trip-rfc", "000000", &verified), ErrPODOTPRequired)

	// Correct code in the validity window verifies (both formats).
	verified = false
	require.NoError(t, svc.verifyPODOTP(ctx, "trip-sqlite", "123456", &verified))
	assert.True(t, verified, "sqlite-format expiry must verify, not bypass")
	verified = false
	require.NoError(t, svc.verifyPODOTP(ctx, "trip-rfc", "123456", &verified))
	assert.True(t, verified)

	// Expired codes fail closed even with the right code (re-issue required).
	verified = false
	require.ErrorIs(t, svc.verifyPODOTP(ctx, "trip-exp-rfc", "123456", &verified), ErrPODOTPRequired)
	assert.False(t, verified)
	require.ErrorIs(t, svc.verifyPODOTP(ctx, "trip-exp-sql", "123456", &verified), ErrPODOTPRequired)

	// Corrupt expiry with an active code enforces (fail closed, never bypass).
	require.ErrorIs(t, svc.verifyPODOTP(ctx, "trip-garbage", "000000", &verified), ErrPODOTPRequired)

	// Legacy trip without a code delivers unverified.
	verified = false
	require.NoError(t, svc.verifyPODOTP(ctx, "trip-legacy", "", &verified))
	assert.False(t, verified)
}

func TestEnsurePODOTP_KeepsValidSQLiteFormatCode(t *testing.T) {
	svc, db := newPODOTPTestService(t)
	ctx := shared.ContextWithTenantID(context.Background(), "t-otp")
	futureSQL := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	seedOTPTrip(t, db, "trip-keep", "654321", futureSQL)

	code, err := svc.EnsurePODOTP(ctx, "trip-keep")
	require.NoError(t, err)
	assert.Equal(t, "654321", code, "valid sqlite-format code must not be regenerated")
}

// The operator sees trips.pod_otp, while the stop POD flow verifies
// trip_stops.otp_code. Both must carry the same code, including when a stale
// stop code predates the trip-level one.
func TestEnsurePODOTP_SyncsStopCode(t *testing.T) {
	svc, db := newPODOTPTestService(t)
	ctx := shared.ContextWithTenantID(context.Background(), "t-otp")
	futureSQL := time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	seedOTPTrip(t, db, "trip-sync", "123456", futureSQL)
	_, err := db.Exec(`INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, address, otp_required, otp_code)
		VALUES ('stop-sync', 'trip-sync', 't-otp', 2, 'drop', 'Consignee', 'Pune', 1, '999999')`)
	require.NoError(t, err)

	code, err := svc.EnsurePODOTP(ctx, "trip-sync")
	require.NoError(t, err)
	require.Equal(t, "123456", code)

	var stopCode string
	require.NoError(t, db.QueryRow(`SELECT otp_code FROM trip_stops WHERE id='stop-sync'`).Scan(&stopCode))
	require.Equal(t, code, stopCode, "stop OTP must match the code shown to the operator")
}
