// Package retention enforces the published business-data retention schedule
// (GET /api/v1/privacy/retention-schedule). Telemetry/sessions/outbox already
// have dedicated purgers; this worker covers the business tables that had no
// automated disposal: video telematics, audit logs, breach ledger, consents,
// and DSAR requests.
package retention

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"transport-app/internal/storage"
)

// Target is one table + time column with its maximum age.
type Target struct {
	Label      string
	Table      string
	TimeColumn string
	MaxAge     time.Duration
}

// DefaultTargets mirror the published DPDP retention schedule. Tax invoices /
// financial ledgers are deliberately absent: 8-year statutory retention is
// enforced by the DSAR layer, never by a blanket purge.
func DefaultTargets() []Target {
	return []Target{
		{Label: "video_safety_events", Table: "video_safety_events", TimeColumn: "event_time", MaxAge: 90 * 24 * time.Hour},
		{Label: "video_livestream_sessions", Table: "video_livestream_sessions", TimeColumn: "started_at", MaxAge: 90 * 24 * time.Hour},
		{Label: "audit_logs", Table: "audit_logs", TimeColumn: "created_at", MaxAge: 3 * 365 * 24 * time.Hour},
		{Label: "breach_incidents", Table: "breach_incidents", TimeColumn: "detected_at", MaxAge: 3 * 365 * 24 * time.Hour},
		{Label: "user_consents", Table: "user_consents", TimeColumn: "created_at", MaxAge: 5 * 365 * 24 * time.Hour},
		{Label: "privacy_dsar_requests", Table: "privacy_dsar_requests", TimeColumn: "created_at", MaxAge: 5 * 365 * 24 * time.Hour},
	}
}

// Worker purges expired business rows in bounded batches.
type Worker struct {
	db       *sql.DB
	logger   *slog.Logger
	interval time.Duration
	batch    int
	targets  []Target
	now      func() time.Time
	// Driver KYC post-tenure purge: drivers deactivated longer than kycAfter
	// have KYC fields cleared and driver_documents removed (published DPDP
	// schedule: "Driver KYC & Licences — Employment Duration + 3 Years").
	kycAfter time.Duration
	kycBatch int
	blobs    storage.Store
}

// NewWorker builds the retention worker (24h interval, 5,000-row batches,
// 3-year post-tenure KYC retention). blobs is optional and only used for
// best-effort KYC document blob deletion after the DB purge commits.
func NewWorker(db *sql.DB, logger *slog.Logger, interval time.Duration, blobs ...storage.Store) *Worker {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	w := &Worker{
		db: db, logger: logger, interval: interval, batch: 5000,
		targets: DefaultTargets(), now: time.Now,
		kycAfter: 3 * 365 * 24 * time.Hour, kycBatch: 100,
	}
	if len(blobs) > 0 {
		w.blobs = blobs[0]
	}
	return w
}

// SetKYCAfter overrides the post-tenure KYC retention period.
func (w *Worker) SetKYCAfter(d time.Duration) {
	if d > 0 {
		w.kycAfter = d
	}
}

// Run purges immediately then once per interval until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.db == nil {
		return
	}
	w.purgeLogged(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.purgeLogged(ctx)
		}
	}
}

func (w *Worker) purgeLogged(ctx context.Context) {
	counts, err := w.PurgeOnce(ctx)
	if err != nil {
		w.logger.Warn("business retention purge failed", "error", err)
		return
	}
	for label, n := range counts {
		if n > 0 {
			w.logger.Info("business retention purged expired rows", "table", label, "rows", n)
		}
	}
}

// PurgeOnce deletes every expired row across all targets and returns per-table
// counts. Safe to call repeatedly (idempotent).
func (w *Worker) PurgeOnce(ctx context.Context) (map[string]int64, error) {
	counts := make(map[string]int64, len(w.targets))
	now := w.now().UTC()
	for _, t := range w.targets {
		cutoff := now.Add(-t.MaxAge)
		n, err := w.purgeTable(ctx, t, cutoff)
		if err != nil {
			return counts, fmt.Errorf("purge %s: %w", t.Table, err)
		}
		counts[t.Label] = n
	}
	kycPurged, err := w.purgeExpiredKYC(ctx, now)
	if err != nil {
		return counts, fmt.Errorf("purge driver KYC: %w", err)
	}
	counts["driver_kyc"] = kycPurged
	return counts, nil
}

func (w *Worker) purgeTable(ctx context.Context, t Target, cutoff time.Time) (int64, error) {
	var total int64
	for {
		res, err := w.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE id IN (
				SELECT id FROM %s WHERE %s < $1 LIMIT $2)`, t.Table, t.Table, t.TimeColumn),
			cutoff, w.batch)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(w.batch) {
			return total, nil
		}
	}
}

// purgeExpiredKYC anonymizes drivers whose tenure ended longer than kycAfter
// ago. Idempotent via kyc_purged_at. KYC-specific fields and document rows are
// removed; financial ledger rows keep the driver id (statutory retention).
func (w *Worker) purgeExpiredKYC(ctx context.Context, now time.Time) (int64, error) {
	if w == nil || w.db == nil {
		return 0, nil
	}
	cutoff := now.Add(-w.kycAfter)
	var total int64
	for {
		ids, err := w.listExpiredKYCDriverIDs(ctx, cutoff, w.kycBatch)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		for _, id := range ids {
			urls, err := w.purgeDriverKYC(ctx, id, now)
			if err != nil {
				return total, fmt.Errorf("driver %s: %w", id, err)
			}
			deleted, pending := w.deleteKYCBlobs(ctx, urls)
			total++
			w.logger.Info("post-tenure KYC purged",
				"driver_id", id, "blobs_deleted", deleted, "blobs_pending", pending)
		}
		if len(ids) < w.kycBatch {
			return total, nil
		}
	}
}

// purgeDriverKYC clears KYC fields and deletes document rows in one tx; returns
// the file URLs so blobs can be removed after the commit.
func (w *Worker) purgeDriverKYC(ctx context.Context, driverID string, now time.Time) ([]string, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	urls, err := driverDocumentURLs(ctx, tx, driverID)
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM driver_documents WHERE driver_id = $1`, driverID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE drivers
		SET license_number = NULL,
		    license_expiry = NULL,
		    aadhaar = NULL,
		    pan = NULL,
		    bank_details = '',
		    emergency_contact_name = NULL,
		    emergency_contact_phone = NULL,
		    address = NULL,
		    email = NULL,
		    kyc_purged_at = $2,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 AND kyc_purged_at IS NULL`, driverID, now.UTC()); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return urls, nil
}

// blobStoreKey maps a driver_documents.file_url onto a storage key. Only the
// local "/uploads/<key>" form is understood (same rule as DSAR erasure); full
// or S3 URLs are reported as unknown rather than guessed.
func blobStoreKey(fileURL string) (string, bool) {
	const uploadsPrefix = "/uploads/"
	if !strings.HasPrefix(fileURL, uploadsPrefix) {
		return "", false
	}
	key := strings.TrimPrefix(fileURL, uploadsPrefix)
	if key == "" {
		return "", false
	}
	return key, true
}

// deleteKYCBlobs removes KYC blobs best-effort after the DB purge commits.
func (w *Worker) deleteKYCBlobs(ctx context.Context, fileURLs []string) (deleted, pending int64) {
	if len(fileURLs) == 0 {
		return 0, 0
	}
	if w.blobs == nil {
		w.logger.Warn("retention: no blob store configured; KYC blobs left for manual removal", "count", len(fileURLs))
		return 0, int64(len(fileURLs))
	}
	for _, fileURL := range fileURLs {
		key, ok := blobStoreKey(fileURL)
		if !ok {
			w.logger.Warn("retention: unrecognised KYC file_url; blob left for manual review", "file_url", fileURL)
			pending++
			continue
		}
		f, err := w.blobs.Open(ctx, key)
		if err == nil {
			_ = f.Close()
			err = w.blobs.Delete(ctx, key)
		}
		if err != nil {
			w.logger.Warn("retention: KYC blob not deleted; left for manual review", "key", key, "error", err)
			pending++
			continue
		}
		deleted++
	}
	return deleted, pending
}

// listExpiredKYCDriverIDs returns one batch of drivers whose KYC retention has
// elapsed and has not been purged yet.
func (w *Worker) listExpiredKYCDriverIDs(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	rows, err := w.db.QueryContext(ctx, `
		SELECT id FROM drivers
		WHERE deactivated_at IS NOT NULL
		  AND deactivated_at < $1
		  AND kyc_purged_at IS NULL
		ORDER BY deactivated_at ASC
		LIMIT $2`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// driverDocumentURLs returns the file URLs of a driver KYC documents inside
// the purge transaction.
func driverDocumentURLs(ctx context.Context, tx *sql.Tx, driverID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT file_url FROM driver_documents WHERE driver_id = $1`, driverID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var urls []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		urls = append(urls, u)
	}
	return urls, rows.Err()
}
