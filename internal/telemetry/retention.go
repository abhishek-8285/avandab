package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// TelemetryCleaner prunes stale telemetry positions, raw events, snapshots,
// and published outbox events to prevent disk exhaustion on resource-constrained servers.
type TelemetryCleaner struct {
	db            *sql.DB
	retentionDays int
	interval      time.Duration
	logger        *slog.Logger
}

// NewTelemetryCleaner creates a cleaner configured to run once every 24 hours.
func NewTelemetryCleaner(db *sql.DB, retentionDays int, logger *slog.Logger) *TelemetryCleaner {
	if retentionDays <= 0 {
		retentionDays = 30 // default 30 days retention
	}
	return &TelemetryCleaner{
		db:            db,
		retentionDays: retentionDays,
		interval:      24 * time.Hour,
		logger:        logger,
	}
}

// Start launches the background pruner loop.
func (c *TelemetryCleaner) Start(ctx context.Context) {
	if c.db == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()

		// Initial sweep 1 minute after boot
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
			c.Purge(ctx)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.Purge(ctx)
			}
		}
	}()
}

// Purge executes chunked deletions for all telemetry tables older than retentionDays.
func (c *TelemetryCleaner) Purge(ctx context.Context) {
	cutoff := time.Now().UTC().AddDate(0, 0, -c.retentionDays)
	if c.logger != nil {
		c.logger.Info("Starting telemetry retention purge", "cutoff", cutoff.Format(time.RFC3339), "retention_days", c.retentionDays)
	}

	// 1. Purge telemetry_snapshots
	if n, err := c.purgeBatch(ctx, "telemetry_snapshots", "timestamp < $1", cutoff); err != nil {
		if c.logger != nil {
			c.logger.Warn("Failed to purge telemetry_snapshots", "error", err)
		}
	} else if n > 0 && c.logger != nil {
		c.logger.Info("Purged stale telemetry_snapshots", "count", n)
	}

	// 2. Purge telemetry_positions (FK child of telemetry_raw_events)
	if n, err := c.purgeBatch(ctx, "telemetry_positions", "device_time < $1", cutoff); err != nil {
		if c.logger != nil {
			c.logger.Warn("Failed to purge telemetry_positions", "error", err)
		}
	} else if n > 0 && c.logger != nil {
		c.logger.Info("Purged stale telemetry_positions", "count", n)
	}

	// 3. Purge telemetry_raw_events (parent table)
	if n, err := c.purgeBatch(ctx, "telemetry_raw_events", "device_time < $1", cutoff); err != nil {
		if c.logger != nil {
			c.logger.Warn("Failed to purge telemetry_raw_events", "error", err)
		}
	} else if n > 0 && c.logger != nil {
		c.logger.Info("Purged stale telemetry_raw_events", "count", n)
	}

	// 4. Purge published outbox_events older than 7 days
	outboxCutoff := time.Now().UTC().AddDate(0, 0, -7)
	if n, err := c.purgeBatch(ctx, "outbox_events", "published_at IS NOT NULL AND created_at < $1", outboxCutoff); err != nil {
		if c.logger != nil {
			c.logger.Warn("Failed to purge outbox_events", "error", err)
		}
	} else if n > 0 && c.logger != nil {
		c.logger.Info("Purged published outbox_events", "count", n)
	}

	// 5. Purge expired auth sessions
	now := time.Now().UTC()
	if n, err := c.purgeBatch(ctx, "sessions", "expires_at < $1", now); err != nil {
		if c.logger != nil {
			c.logger.Warn("Failed to purge expired sessions", "error", err)
		}
	} else if n > 0 && c.logger != nil {
		c.logger.Info("Purged expired auth sessions", "count", n)
	}

	// 6. SQLite WAL maintenance: truncate WAL log and reclaim pages
	var isSQLite bool
	if err := c.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(new(string)); err == nil {
		isSQLite = true
	}
	if isSQLite {
		if _, err := c.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE);"); err != nil && c.logger != nil {
			c.logger.Warn("Failed to execute wal_checkpoint", "error", err)
		} else if c.logger != nil {
			c.logger.Info("SQLite wal_checkpoint(TRUNCATE) completed")
		}
		if _, err := c.db.ExecContext(ctx, "PRAGMA incremental_vacuum;"); err != nil && c.logger != nil {
			c.logger.Warn("Failed to execute incremental_vacuum", "error", err)
		}
	}
}

// purgeBatch deletes up to 1000 records per iteration to avoid holding long SQLite write locks.
func (c *TelemetryCleaner) purgeBatch(ctx context.Context, table, condition string, cutoff time.Time) (int64, error) {
	var totalDeleted int64
	for {
		select {
		case <-ctx.Done():
			return totalDeleted, ctx.Err()
		default:
		}

		query := fmt.Sprintf(`DELETE FROM %s WHERE id IN (SELECT id FROM %s WHERE %s LIMIT 1000)`, table, table, condition)
		res, err := c.db.ExecContext(ctx, query, cutoff)
		if err != nil {
			return totalDeleted, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return totalDeleted, err
		}
		totalDeleted += affected
		if affected < 1000 {
			break
		}
		time.Sleep(50 * time.Millisecond) // Yield to ongoing concurrent read/write queries
	}
	return totalDeleted, nil
}
