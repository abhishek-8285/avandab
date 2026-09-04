package maintenance

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/events"
	"transport-app/internal/maintenance/domain"
	maintsql "transport-app/internal/maintenance/infrastructure/sql"
)

// Worker evaluates maintenance schedules, ingests DTC alerts, and manages the vehicle maintenance due flag (Spec 04 §6, §12).
type Worker struct {
	db              *sql.DB
	repo            *maintsql.MaintenanceRepository
	bus             events.EventBus
	logger          *slog.Logger
	checkInterval   time.Duration
	fallbackCritDTC string
}

// NewWorker constructs a new PM Worker instance.
func NewWorker(db *sql.DB, bus events.EventBus, logger *slog.Logger, intervalMin int, fallbackCritDTC string) *Worker {
	if intervalMin <= 0 {
		intervalMin = 15
	}
	if fallbackCritDTC == "" {
		fallbackCritDTC = "P0A0F,P1602"
	}
	if logger == nil {
		logger = slog.Default()
	}
	w := &Worker{
		db:              db,
		repo:            maintsql.NewMaintenanceRepository(db),
		bus:             bus,
		logger:          logger,
		checkInterval:   time.Duration(intervalMin) * time.Minute,
		fallbackCritDTC: fallbackCritDTC,
	}

	if bus != nil {
		w.subscribeDTC(bus)
	}

	return w
}

// Run starts the background evaluation loop.
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("preventive maintenance worker started", "interval", w.checkInterval)

	// Run initial evaluation on startup
	w.EvaluateSchedules(ctx)

	ticker := time.NewTicker(w.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("preventive maintenance worker stopped")
			return
		case <-ticker.C:
			w.EvaluateSchedules(ctx)
		}
	}
}

// EvaluateSchedules checks odometer and date thresholds for all active schedules (Spec 04 §6).
func (w *Worker) EvaluateSchedules(ctx context.Context) {
	schedules, err := w.repo.ListActiveSchedules(ctx, "")
	if err != nil {
		w.logger.Error("failed to list active maintenance schedules", "error", err)
		return
	}
	tenantByVehicle := w.resolveVehicleTenants(ctx, schedules)

	now := time.Now().UTC()

	for _, s := range schedules {
		tenant := tenantByVehicle[s.VehicleID]
		if tenant == "" {
			w.logger.Warn("maintenance: schedule skipped (unknown vehicle org)", "vehicle_id", s.VehicleID)
			continue
		}
		isDue := false
		var reason string
		var dueDate *time.Time

		// 1. Odometer-based evaluation
		latestOdo, err := w.repo.GetLatestOdometer(ctx, s.VehicleID)
		if err == nil && latestOdo > 0 {
			if s.DueKM != nil && latestOdo >= *s.DueKM {
				isDue = true
				reason = fmt.Sprintf("odometer %.0f km >= due threshold %.0f km", latestOdo, *s.DueKM)
			} else if s.IntervalKM != nil && *s.IntervalKM > 0 {
				lastKM := 0.0
				if s.LastDoneKM != nil {
					lastKM = *s.LastDoneKM
				}
				if latestOdo >= lastKM+*s.IntervalKM {
					isDue = true
					reason = fmt.Sprintf("odometer %.0f km exceeded interval %.0f km from last service (%.0f km)", latestOdo, *s.IntervalKM, lastKM)
				}
			}
		}

		// 2. Date-based evaluation
		if !isDue {
			if s.DueAt != nil && (now.After(*s.DueAt) || now.Equal(*s.DueAt)) {
				isDue = true
				reason = fmt.Sprintf("service date %s reached", s.DueAt.Format("2006-01-02"))
			} else if s.IntervalDays != nil && *s.IntervalDays > 0 {
				lastAt := s.CreatedAt
				if s.LastDoneAt != nil {
					lastAt = *s.LastDoneAt
				}
				due := lastAt.Add(time.Duration(*s.IntervalDays) * 24 * time.Hour)
				dueDate = &due
				if now.After(due) || now.Equal(due) {
					isDue = true
					reason = fmt.Sprintf("interval %d days exceeded since %s", *s.IntervalDays, lastAt.Format("2006-01-02"))
				}
			} else if s.DueAt != nil {
				dueDate = s.DueAt
			}
		}

		if isDue {
			w.markVehicleDue(ctx, s.VehicleID, tenant, s.ID, s.ServiceType, reason, now)
		} else if dueDate != nil {
			// Advance reminder: due soon but not yet. Fires once per window
			// per vehicle+service (deduped on the notifications table).
			if daysLeft := int(dueDate.Sub(now).Hours() / 24); daysLeft >= 0 && daysLeft <= w.reminderWindowDays(ctx, tenant) {
				w.sendDueSoonReminder(ctx, tenant, s.VehicleID, s.ServiceType, *dueDate, daysLeft)
			}
		}
	}
}

// reminderWindowDays reads the per-org advance-warning window
// (company_config maintenance.reminder_days, default 7, clamped 1..30).
func (w *Worker) reminderWindowDays(ctx context.Context, tenant string) int {
	var raw string
	if err := w.db.QueryRowContext(ctx,
		`SELECT value FROM company_config WHERE tenant_id = ? AND key = 'maintenance.reminder_days'`, tenant).Scan(&raw); err == nil {
		var days int
		if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &days); err == nil {
			if days < 1 {
				return 1
			}
			if days > 30 {
				return 30
			}
			return days
		}
	}
	return 7
}

// sendDueSoonReminder notifies the org's admins once per window that a
// service date is approaching. No bus event: reminders are inbox-only so
// alert pipelines stay reserved for actual dues.
func (w *Worker) sendDueSoonReminder(ctx context.Context, tenant, vehicleID, serviceType string, due time.Time, daysLeft int) {
	title := fmt.Sprintf("Maintenance Due Soon: %s", serviceType)
	var recent int
	_ = w.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM notifications
		WHERE title = ? AND message LIKE '%' || ? || '%'
		  AND created_at > datetime('now', '-7 days')`, title, vehicleID).Scan(&recent)
	if recent > 0 {
		return
	}
	msg := fmt.Sprintf("Vehicle %s needs %s maintenance on %s (%d days left)", vehicleID, serviceType, due.Format("2006-01-02"), daysLeft)
	w.notifyOrgAdmins(ctx, tenant, title, msg)
}

// resolveVehicleTenants attributes vehicles to their orgs in one query.
// Unknown vehicles resolve to "" and are skipped by callers.
func (w *Worker) resolveVehicleTenants(ctx context.Context, schedules []domain.Schedule) map[string]string {
	out := make(map[string]string, len(schedules))
	ids := make([]string, 0, len(schedules))
	seen := map[string]bool{}
	for _, s := range schedules {
		if !seen[s.VehicleID] {
			seen[s.VehicleID] = true
			ids = append(ids, s.VehicleID)
		}
	}
	if len(ids) == 0 {
		return out
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = strings.TrimSuffix(placeholders, ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := w.db.QueryContext(ctx,
		`SELECT id, tenant_id FROM vehicles WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var vehID, tenant string
		if err := rows.Scan(&vehID, &tenant); err == nil {
			out[vehID] = tenant
		}
	}
	return out
}

// vehicleTenant resolves one vehicle's org ("" when unknown).
func (w *Worker) vehicleTenant(ctx context.Context, vehicleID string) string {
	var tenant string
	_ = w.db.QueryRowContext(ctx, `SELECT tenant_id FROM vehicles WHERE id = ?`, vehicleID).Scan(&tenant)
	return tenant
}

// notifyOrgAdmins delivers an in-app notification to every active admin and
// org_admin of the tenant (they own the vehicles). Falls back to the legacy
// 'system' recipient only when the org has no admin users at all.
func (w *Worker) notifyOrgAdmins(ctx context.Context, tenant, title, msg string) {
	rows, err := w.db.QueryContext(ctx,
		`SELECT id FROM users WHERE tenant_id = ? AND status = 'active' AND role_id IN (1, 6)`, tenant)
	recipients := []string{}
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err == nil && uid != "" {
				recipients = append(recipients, uid)
			}
		}
	}
	if len(recipients) == 0 {
		recipients = []string{"system"}
	}
	for _, uid := range recipients {
		_, _ = w.db.ExecContext(ctx, `
			INSERT INTO notifications (id, user_id, title, message, channel, status, created_at)
			VALUES (?, ?, ?, ?, 'in_app', 'unread', CURRENT_TIMESTAMP)`,
			uuid.NewString(), uid, title, msg)
	}
}

func (w *Worker) markVehicleDue(ctx context.Context, vehicleID, tenant, scheduleID, serviceType, reason string, dueDate time.Time) {
	err := w.repo.SetMaintenanceDue(ctx, vehicleID, dueDate)
	if err != nil {
		w.logger.Error("failed to set maintenance_due on vehicle", "vehicle_id", vehicleID, "error", err)
		return
	}

	// Notify the owning org's admins (not a dead 'system' recipient).
	title := fmt.Sprintf("Maintenance Due: %s", serviceType)
	msg := fmt.Sprintf("Vehicle %s requires %s maintenance: %s", vehicleID, serviceType, reason)
	w.notifyOrgAdmins(ctx, tenant, title, msg)

	// Open exactly one job card per cause so detection flows into tracked
	// work; repeat sweeps and DTC storms dedupe on the open card.
	w.ensureJobCard(ctx, tenant, vehicleID, scheduleID, serviceType, reason, dueDate)

	// Publish maintenance.due event on bus
	if w.bus != nil {
		evt := events.Event{
			Type: "maintenance.due",
			Payload: map[string]interface{}{
				"vehicle_id":   vehicleID,
				"tenant_id":    tenant,
				"service_type": serviceType,
				"reason":       reason,
				"due_date":     dueDate.Format("2006-01-02"),
			},
		}
		w.bus.Publish(ctx, evt)
	}
}

// ensureJobCard opens one 'open' job card per cause (schedule or DTC storm)
// and silently keeps the existing open card on repeat sweeps. Tenant and
// vehicle are required — unknown orgs never get cards (fail-closed).
func (w *Worker) ensureJobCard(ctx context.Context, tenant, vehicleID, scheduleID, serviceType, reason string, due time.Time) {
	if tenant == "" || vehicleID == "" {
		w.logger.Warn("maintenance: job card skipped (unknown vehicle org)", "vehicle_id", vehicleID)
		return
	}
	open, err := w.repo.FindOpenWorkOrder(ctx, tenant, vehicleID, scheduleID)
	if err != nil {
		w.logger.Error("failed to check open job cards", "vehicle_id", vehicleID, "error", err)
		return
	}
	if open != nil {
		return
	}
	wo := domain.WorkOrder{
		ID:          uuid.NewString(),
		TenantID:    tenant,
		VehicleID:   vehicleID,
		Title:       fmt.Sprintf("Maintenance due: %s", serviceType),
		Description: reason,
		Status:      domain.WorkOrderOpen,
		DueAt:       &due,
	}
	if scheduleID != "" {
		wo.ScheduleID = &scheduleID
	}
	if err := w.repo.CreateWorkOrder(ctx, wo); err != nil {
		w.logger.Error("failed to open job card", "vehicle_id", vehicleID, "error", err)
		return
	}
	w.logger.Info("maintenance: job card opened", "vehicle_id", vehicleID, "service_type", serviceType, "tenant_id", tenant)
}

// EvaluateResolution checks if all due conditions for a vehicle have been cleared.
func (w *Worker) EvaluateResolution(ctx context.Context, vehicleID string) {
	// 1. Check unresolved critical DTCs
	critDTCs, err := w.repo.ListUnresolvedCriticalDtc(ctx, vehicleID)
	if err == nil && len(critDTCs) > 0 {
		return // Critical DTCs still unresolved
	}

	// 2. Check active schedules
	schedules, err := w.repo.ListActiveSchedules(ctx, vehicleID)
	if err == nil {
		now := time.Now().UTC()
		latestOdo, _ := w.repo.GetLatestOdometer(ctx, vehicleID)
		for _, s := range schedules {
			if s.DueKM != nil && latestOdo >= *s.DueKM {
				return
			}
			if s.IntervalKM != nil && *s.IntervalKM > 0 {
				lastKM := 0.0
				if s.LastDoneKM != nil {
					lastKM = *s.LastDoneKM
				}
				if latestOdo >= lastKM+*s.IntervalKM {
					return
				}
			}
			if s.DueAt != nil && (now.After(*s.DueAt) || now.Equal(*s.DueAt)) {
				return
			}
			if s.IntervalDays != nil && *s.IntervalDays > 0 {
				lastAt := s.CreatedAt
				if s.LastDoneAt != nil {
					lastAt = *s.LastDoneAt
				}
				if now.After(lastAt.Add(time.Duration(*s.IntervalDays) * 24 * time.Hour)) {
					return
				}
			}
		}
	}

	// Clear maintenance due
	if err := w.repo.ClearMaintenanceDue(ctx, vehicleID); err == nil {
		if w.bus != nil {
			evt := events.Event{
				Type: "maintenance.cleared",
				Payload: map[string]interface{}{
					"vehicle_id": vehicleID,
					"tenant_id":  w.vehicleTenant(ctx, vehicleID),
					"cleared_at": time.Now().UTC().Format(time.RFC3339),
				},
			}
			w.bus.Publish(ctx, evt)
		}
	}
}

// subscribeDTC listens for DTC alerts from the event bus.
func (w *Worker) subscribeDTC(bus events.EventBus) {
	bus.Subscribe("alert.dtc", func(ctx context.Context, evt events.Event) error {
		return w.HandleDtcEvent(ctx, evt.Payload)
	})

	bus.Subscribe("AlertEvent", func(ctx context.Context, evt events.Event) error {
		return w.HandleDtcEvent(ctx, evt.Payload)
	})
}

// HandleDtcEvent processes a DTC payload and inserts it with minute-granularity dedup (Spec 04 §6).
func (w *Worker) HandleDtcEvent(ctx context.Context, payload interface{}) error {
	var m map[string]interface{}
	switch p := payload.(type) {
	case map[string]interface{}:
		m = p
	case []byte:
		_ = json.Unmarshal(p, &m)
	case string:
		_ = json.Unmarshal([]byte(p), &m)
	default:
		data, err := json.Marshal(payload)
		if err != nil {
			return nil
		}
		_ = json.Unmarshal(data, &m)
	}

	if m == nil {
		return nil
	}

	vehicleID, _ := m["vehicle_id"].(string)
	if vehicleID == "" {
		return nil
	}

	tripID, _ := m["trip_id"].(string)
	var tripIDPtr *string
	if tripID != "" {
		tripIDPtr = &tripID
	}

	dtcCode, _ := m["dtc_code"].(string)
	if dtcCode == "" {
		if codes, ok := m["dtc_codes"].([]interface{}); ok && len(codes) > 0 {
			dtcCode = fmt.Sprint(codes[0])
		}
	}
	if dtcCode == "" {
		return nil
	}
	dtcCode = strings.ToUpper(strings.TrimSpace(dtcCode))

	severity, _ := m["severity"].(string)
	if severity == "" {
		severity = "info"
	}
	severity = strings.ToLower(severity)
	if severity != "info" && severity != "warning" && severity != "critical" {
		severity = "info"
	}

	desc, _ := m["description"].(string)
	var descPtr *string
	if desc != "" {
		descPtr = &desc
	}

	rawBytes, _ := json.Marshal(m)
	rawStr := string(rawBytes)

	occurredAt := time.Now().UTC()
	if occStr, ok := m["occurred_at"].(string); ok && occStr != "" {
		if t, err := time.Parse(time.RFC3339, occStr); err == nil {
			occurredAt = t.UTC()
		}
	}

	// Minute dedup insert
	dtc := domain.DtcEvent{
		ID:          uuid.NewString(),
		VehicleID:   vehicleID,
		TripID:      tripIDPtr,
		DtcCode:     dtcCode,
		Severity:    severity,
		Description: descPtr,
		RawPayload:  &rawStr,
		OccurredAt:  occurredAt,
	}

	inserted, err := w.repo.InsertDtcEvent(ctx, dtc)
	if err != nil {
		w.logger.Error("failed to insert DTC event", "vehicle_id", vehicleID, "dtc_code", dtcCode, "error", err)
		return err
	}

	// Check if this DTC is critical
	critCodes := w.repo.GetCriticalDtcCodes(ctx, w.fallbackCritDTC)
	isCritical := severity == "critical"
	if !isCritical {
		for _, c := range critCodes {
			if dtcCode == c {
				isCritical = true
				break
			}
		}
	}

	if inserted && isCritical {
		w.markVehicleDue(ctx, vehicleID, w.vehicleTenant(ctx, vehicleID), "", "engine", fmt.Sprintf("Critical DTC %s detected", dtcCode), occurredAt)
	}

	return nil
}
