package eta

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RecordHistory stores a completed trip segment for future ETA prediction.
// Called after trip arrival; tenant-scoped.
func (s *EtaService) RecordHistory(ctx context.Context, tenantID, tripID, segmentStart, segmentEnd string, actualMinutes int, trafficTag string) error {
	if s.db == nil {
		return fmt.Errorf("eta: no db")
	}
	if actualMinutes <= 0 {
		return fmt.Errorf("eta: invalid minutes")
	}
	now := time.Now().UTC()
	dayOfWeek := int(now.Weekday())
	hourOfDay := now.Hour()
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO eta_history (id, tenant_id, trip_id, segment_start, segment_end, actual_minutes, traffic_tag, day_of_week, hour_of_day, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, tenantID, tripID, segmentStart, segmentEnd, actualMinutes, trafficTag, dayOfWeek, hourOfDay, now)
	return err
}

// PredictFromHistory returns historical average for a segment (90-day window).
// Returns avgMinutes and sampleCount.
func (s *EtaService) PredictFromHistory(ctx context.Context, tenantID, segmentStart, segmentEnd string) (float64, int, error) {
	var avg sql.NullFloat64
	var cnt int
	err := s.db.QueryRowContext(ctx,
		`SELECT AVG(actual_minutes), COUNT(*) FROM eta_history
		 WHERE tenant_id=$1 AND segment_start=$2 AND segment_end=$3 AND created_at > $4`,
		tenantID, segmentStart, segmentEnd, time.Now().UTC().AddDate(0, 0, -90)).Scan(&avg, &cnt)
	if err != nil {
		return 0, 0, err
	}
	if !avg.Valid || cnt == 0 {
		return 0, 0, fmt.Errorf("no history")
	}
	return avg.Float64, cnt, nil
}

// CleanupOldHistory deletes raw rows older than 90 days. Run daily via cron.
func (s *EtaService) CleanupOldHistory(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM eta_history WHERE created_at < $1`, time.Now().UTC().AddDate(0, 0, -90))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AggregateMonthly rolls up raw history into eta_history_monthly (call after cleanup).
func (s *EtaService) AggregateMonthly(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO eta_history_monthly (tenant_id, segment_start, segment_end, month, avg_minutes, sample_count)
		SELECT tenant_id, segment_start, segment_end,
		       substr(CAST(created_at AS TEXT), 1, 7) || '-01' as month,
		       AVG(actual_minutes), COUNT(*)
		FROM eta_history
		WHERE created_at < $1
		GROUP BY tenant_id, segment_start, segment_end, month
		ON CONFLICT (tenant_id, segment_start, segment_end, month) DO UPDATE SET
		       avg_minutes = excluded.avg_minutes, sample_count = excluded.sample_count
	`, time.Now().UTC().AddDate(0, 0, -90))
	return err
}
