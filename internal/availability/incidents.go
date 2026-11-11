package availability

// Public status-page incident feed, extracted from internal/handlers/
// failpage.go (handlers-end-state ADR, Phase 2). The handler keeps the
// status-page view model/formatting; this file keeps the alerts query and the
// 10-minute de-duplication.

import (
	"context"
	"database/sql"
	"time"
)

// Incident is one availability SLA-breach alert surfaced on the status page.
type Incident struct {
	Title     string
	Message   string
	Status    string
	FirstSeen time.Time
	Resolved  sql.NullTime
}

// LoadIncidents returns up to 10 de-duplicated availability incidents, newest
// first. The legacy handler skipped the whole section on any query/scan/rows
// error, so this returns nil instead of an error.
func LoadIncidents(ctx context.Context, db *sql.DB) []Incident {
	if db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT title, message, status, first_seen_at, resolved_at
		FROM alerts
		WHERE source = 'availability' AND alert_type = 'sla_breach'
		ORDER BY first_seen_at DESC
		LIMIT 50`)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()

	var found []Incident
	for rows.Next() {
		var r Incident
		if err := rows.Scan(&r.Title, &r.Message, &r.Status, &r.FirstSeen, &r.Resolved); err != nil {
			return nil
		}
		found = append(found, r)
	}
	if err := rows.Err(); err != nil {
		return nil
	}

	seen := map[string]bool{}
	out := make([]Incident, 0, len(found))
	for _, r := range found {
		bucket := r.FirstSeen.UTC().Truncate(10 * time.Minute).Format("2006-01-02T15:04")
		if seen[bucket] {
			continue
		}
		seen[bucket] = true
		out = append(out, r)
		if len(out) >= 10 {
			break
		}
	}
	return out
}
