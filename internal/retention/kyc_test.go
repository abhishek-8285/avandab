package retention

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type fakeBlobStore struct {
	mu      sync.Mutex
	deleted []string
}

func (f *fakeBlobStore) Save(context.Context, string, io.Reader, string) (string, error) {
	return "", nil
}
func (f *fakeBlobStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (f *fakeBlobStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, key)
	return nil
}
func (f *fakeBlobStore) deletedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.deleted...)
}

func newKYCTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("kyc_retention_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE drivers (
			id TEXT PRIMARY KEY,
			first_name TEXT NOT NULL DEFAULT '',
			last_name TEXT NOT NULL DEFAULT '',
			phone TEXT NOT NULL DEFAULT '',
			email TEXT,
			address TEXT,
			license_number TEXT,
			license_expiry DATE,
			status TEXT NOT NULL DEFAULT 'available',
			emergency_contact_name TEXT,
			emergency_contact_phone TEXT,
			aadhaar TEXT,
			pan TEXT,
			bank_details TEXT,
			deactivated_at DATETIME,
			kyc_purged_at DATETIME,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE driver_documents (
			id TEXT PRIMARY KEY,
			driver_id TEXT NOT NULL,
			doc_type TEXT NOT NULL DEFAULT 'dl',
			file_url TEXT NOT NULL
		)`)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPurgeExpiredKYC_AnonymizesOnlyEligibleDrivers(t *testing.T) {
	db := newKYCTestDB(t)
	blobs := &fakeBlobStore{}
	now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	old := now.Add(-4 * 365 * 24 * time.Hour)
	fresh := now.Add(-1 * 365 * 24 * time.Hour)

	insertDriver := func(id string, deactivatedAt *time.Time, purgedAt *time.Time) {
		t.Helper()
		_, err := db.Exec(`
			INSERT INTO drivers (id, first_name, last_name, phone, email, address,
				license_number, license_expiry, status, emergency_contact_name, emergency_contact_phone,
				aadhaar, pan, bank_details, deactivated_at, kyc_purged_at)
			VALUES (?, 'Kyc', 'Driver', '9000000000', ?, 'Somewhere',
				'DL-0420110012345', '2030-01-01', 'inactive', 'Kin', '9000000001',
				'123412341234', 'ABCDE1234F', '{"account":"1"}', ?, ?)`,
			id, id+"@example.test", deactivatedAt, purgedAt)
		require.NoError(t, err)
	}
	insertDriver("drv-eligible", &old, nil)
	insertDriver("drv-fresh", &fresh, nil)
	insertDriver("drv-active", nil, nil)
	insertDriver("drv-already-purged", &old, &old)

	_, err := db.Exec(`
		INSERT INTO driver_documents (id, driver_id, doc_type, file_url) VALUES
		 ('doc-1', 'drv-eligible', 'dl', '/uploads/kyc/drivers/drv-eligible/dl.pdf'),
		 ('doc-2', 'drv-eligible', 'pan', 'https://s3.example.test/pan.pdf'),
		 ('doc-3', 'drv-fresh', 'dl', '/uploads/kyc/drivers/drv-fresh/dl.pdf')`)
	require.NoError(t, err)

	worker := NewWorker(db, nil, time.Hour, blobs)
	worker.now = func() time.Time { return now }
	worker.targets = nil // this test exercises the KYC purge only
	counts, err := worker.PurgeOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), counts["driver_kyc"])

	var license, aadhaar, pan, bank, emergency, address, email sql.NullString
	var purged sql.NullTime
	require.NoError(t, db.QueryRow(`
		SELECT license_number, aadhaar, pan, bank_details, emergency_contact_name, address, email, kyc_purged_at
		FROM drivers WHERE id = 'drv-eligible'`).Scan(
		&license, &aadhaar, &pan, &bank, &emergency, &address, &email, &purged))
	assert.False(t, license.Valid, "license must be cleared")
	assert.False(t, aadhaar.Valid, "aadhaar must be cleared")
	assert.False(t, pan.Valid, "pan must be cleared")
	assert.False(t, bank.Valid && bank.String != "", "bank details must be cleared")
	assert.False(t, emergency.Valid, "emergency contact must be cleared")
	assert.False(t, address.Valid, "address must be cleared")
	assert.False(t, email.Valid, "email must be cleared")
	assert.True(t, purged.Valid, "kyc_purged_at must be stamped")

	var docs int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_documents WHERE driver_id = 'drv-eligible'`).Scan(&docs))
	assert.Zero(t, docs, "eligible driver documents must be deleted")
	assert.Equal(t, []string{"kyc/drivers/drv-eligible/dl.pdf"}, blobs.deletedKeys(),
		"only the recognised local blob is deleted; S3 URL stays pending")

	// Fresh/active/already-purged drivers untouched.
	for _, id := range []string{"drv-fresh", "drv-active", "drv-already-purged"} {
		var lic sql.NullString
		require.NoError(t, db.QueryRow(`SELECT license_number FROM drivers WHERE id = ?`, id).Scan(&lic))
		assert.True(t, lic.Valid, "%s must keep KYC data", id)
	}
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_documents WHERE driver_id = 'drv-fresh'`).Scan(&docs))
	assert.Equal(t, 1, docs)

	// Idempotent: second pass purges nothing.
	counts, err = worker.PurgeOnce(context.Background())
	require.NoError(t, err)
	assert.Zero(t, counts["driver_kyc"])
}
