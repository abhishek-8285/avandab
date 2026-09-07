package accounting

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
)

// SyncStatusResponse represents sync health and metrics.
type SyncStatusResponse struct {
	Adapter       string     `json:"adapter"`
	Enabled       bool       `json:"enabled"`
	PendingEvents int        `json:"pending_events"`
	LastSyncedAt  *time.Time `json:"last_synced_at"`
}

// TriggerResult represents the result of a manual sync flush.
type TriggerResult struct {
	Dispatched        int `json:"dispatched"`
	Failed            int `json:"failed"`
	SkippedDuplicates int `json:"skipped_duplicates"`
}

// ReconcileResult represents the reconciliation summary.
type ReconcileResult struct {
	Total       int      `json:"total"`
	Acked       int      `json:"acked"`
	Unacked     int      `json:"unacked"`
	UnackedRefs []string `json:"unacked_refs"`
}

// Consumer handles accounting event subscriptions, sync logging, GL rules, and reconciliation.
type Consumer struct {
	db     *sql.DB
	client Client
	cfg    Config
}

// NewConsumer creates a new accounting consumer.
func NewConsumer(db *sql.DB, client Client, cfg Config) *Consumer {
	return &Consumer{
		db:     db,
		client: client,
		cfg:    cfg,
	}
}

// SubscribeEvents registers accounting event handlers on the event bus.
func (c *Consumer) SubscribeEvents(bus events.EventBus) {
	if bus == nil {
		return
	}
	// Subscribe to canonical strings and legacy event strings
	eventTypes := []string{
		"DriverPayoutSettled",
		events.DriverPayoutSettled,
		"SettlementGenerated",
		"InvoiceExported",
		events.InvoiceGenerated,
		"TDSRemitted",
	}
	for _, et := range eventTypes {
		bus.Subscribe(et, c.handleEvent)
	}
}

func (c *Consumer) handleEvent(ctx context.Context, evt events.Event) error {
	slog.Default().Info("[accounting:consumer] Processing event", "type", evt.Type)
	_, err := c.ProcessEvent(ctx, evt)
	return err
}

// ProcessEvent logs and dispatches an event to the configured accounting adapter.
func (c *Consumer) ProcessEvent(ctx context.Context, evt events.Event) (bool, error) {
	if c.db == nil {
		return false, nil
	}

	pMap := payloadToMap(evt.Payload)
	aggID := extractEntityID(pMap)
	if aggID == "" {
		aggID = uuid.New().String()
	}
	idempotencyKey := fmt.Sprintf("%s:%s", evt.Type, aggID)
	entityType := inferEntityType(evt.Type)

	payloadBytes, _ := json.Marshal(pMap)
	adapterName := strings.ToLower(c.cfg.Provider)
	if adapterName == "" {
		adapterName = "mock"
	}

	logID := "acc-" + uuid.New().String()

	// 1. Insert into accounting_sync_log (UNIQUE idempotency_key prevents duplicate processing)
	res, err := c.db.ExecContext(ctx, `
		INSERT INTO accounting_sync_log (
			id, idempotency_key, direction, entity_type, entity_id, adapter,
			payload_json, status, attempts, created_at, updated_at
		) VALUES ($1, $2, 'out', $3, $4, $5, $6, 'pending', 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (idempotency_key) DO NOTHING
	`, logID, idempotencyKey, entityType, aggID, adapterName, string(payloadBytes))
	if err != nil {
		return false, fmt.Errorf("insert accounting_sync_log failed: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		slog.Default().Info("[accounting:consumer] Skipping duplicate event", "idempotency_key", idempotencyKey)
		return false, nil // Duplicate skipped
	}

	// 2. Fetch GL rule if applicable
	var debitAcc, creditAcc string
	_ = c.db.QueryRowContext(ctx, `
		SELECT debit_account, credit_account FROM accounting_gl_rule
		WHERE event_type = $1 ORDER BY priority DESC LIMIT 1
	`, evt.Type).Scan(&debitAcc, &creditAcc)

	if debitAcc == "" {
		debitAcc = "General Expense"
	}
	if creditAcc == "" {
		creditAcc = "Bank - Current"
	}

	// 3. Dispatch to adapter
	var extID string
	var dispatchErr error

	if strings.Contains(strings.ToLower(evt.Type), "invoice") {
		inv := parseInvoiceFromPayload(pMap, aggID)
		expRes, err := c.client.ExportInvoice(ctx, inv)
		if err != nil {
			dispatchErr = err
		} else {
			extID = expRes.ExternalID
		}
	} else {
		// Journal entry for payouts / TDS / settlements
		amount := extractAmountFromPayload(pMap)
		ref := fmt.Sprintf("%s-%s", evt.Type, aggID)
		entry := JournalEntry{
			EntryDate: time.Now(),
			Reference: ref,
			Narration: fmt.Sprintf("Event %s for entity %s", evt.Type, aggID),
			Lines: []JournalLine{
				{Account: debitAcc, Debit: amount, Credit: 0},
				{Account: creditAcc, Debit: 0, Credit: amount},
			},
		}
		jeRes, err := c.client.PushJournalEntry(ctx, entry)
		if err != nil {
			dispatchErr = err
		} else {
			extID = jeRes.EntryID
		}
	}

	// 4. Update sync log & mapping
	if dispatchErr != nil {
		_, _ = c.db.ExecContext(ctx, `
			UPDATE accounting_sync_log
			SET status = 'failed', attempts = attempts + 1, last_error = $1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
		`, dispatchErr.Error(), logID)
		return true, dispatchErr
	}

	_, _ = c.db.ExecContext(ctx, `
		UPDATE accounting_sync_log
		SET status = 'acked', external_id = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, extID, logID)

	if extID != "" {
		mapID := "map-" + uuid.New().String()
		_, _ = c.db.ExecContext(ctx, `
			INSERT INTO accounting_mapping (id, entity_type, entity_id, adapter, external_id, created_at)
			VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
			ON CONFLICT (entity_type, entity_id, adapter) DO UPDATE SET external_id = excluded.external_id
		`, mapID, entityType, aggID, adapterName, extID)
	}

	return true, nil
}

// TriggerSync flushes pending or failed accounting sync items.
func (c *Consumer) TriggerSync(ctx context.Context, sinceMinutes int) (TriggerResult, error) {
	var res TriggerResult
	if c.db == nil {
		return res, nil
	}

	timeClause := ""
	var timeArgs []any
	if sinceMinutes > 0 {
		timeClause = "AND created_at >= $1"
		timeArgs = append(timeArgs, time.Now().UTC().Add(-time.Duration(sinceMinutes)*time.Minute))
	}

	query := fmt.Sprintf(`
		SELECT id, idempotency_key, entity_type, entity_id, payload_json
		FROM accounting_sync_log
		WHERE status IN ('pending', 'failed') %s
		ORDER BY created_at ASC
	`, timeClause)

	rows, err := c.db.QueryContext(ctx, query, timeArgs...)
	if err != nil {
		return res, fmt.Errorf("query sync log failed: %w", err)
	}
	defer rows.Close()

	type item struct {
		id, idemKey, entityType, entityID, payloadJSON string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.idemKey, &it.entityType, &it.entityID, &it.payloadJSON); err == nil {
			items = append(items, it)
		}
	}

	for _, it := range items {
		// Split idempotency key to recover event type
		parts := strings.SplitN(it.idemKey, ":", 2)
		evtType := parts[0]

		var payload map[string]interface{}
		_ = json.Unmarshal([]byte(it.payloadJSON), &payload)

		var extID string
		var dispatchErr error

		if strings.Contains(strings.ToLower(evtType), "invoice") {
			inv := parseInvoiceFromPayload(payload, it.entityID)
			expRes, err := c.client.ExportInvoice(ctx, inv)
			if err != nil {
				dispatchErr = err
			} else {
				extID = expRes.ExternalID
			}
		} else {
			amount := extractAmountFromPayload(payload)
			entry := JournalEntry{
				EntryDate: time.Now(),
				Reference: fmt.Sprintf("%s-%s", evtType, it.entityID),
				Narration: fmt.Sprintf("Retried sync for %s", it.entityID),
				Lines: []JournalLine{
					{Account: "General Payable", Debit: amount, Credit: 0},
					{Account: "Bank - Current", Debit: 0, Credit: amount},
				},
			}
			jeRes, err := c.client.PushJournalEntry(ctx, entry)
			if err != nil {
				dispatchErr = err
			} else {
				extID = jeRes.EntryID
			}
		}

		if dispatchErr != nil {
			res.Failed++
			_, _ = c.db.ExecContext(ctx, `
				UPDATE accounting_sync_log
				SET attempts = attempts + 1, last_error = $1, updated_at = CURRENT_TIMESTAMP
				WHERE id = $2
			`, dispatchErr.Error(), it.id)
		} else {
			res.Dispatched++
			_, _ = c.db.ExecContext(ctx, `
				UPDATE accounting_sync_log
				SET status = 'acked', external_id = $1, updated_at = CURRENT_TIMESTAMP
				WHERE id = $2
			`, extID, it.id)

			if extID != "" {
				adapterName := strings.ToLower(c.cfg.Provider)
				if adapterName == "" {
					adapterName = "mock"
				}
				mapID := "map-" + uuid.New().String()
				_, _ = c.db.ExecContext(ctx, `
					INSERT INTO accounting_mapping (id, entity_type, entity_id, adapter, external_id, created_at)
					VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
					ON CONFLICT (entity_type, entity_id, adapter) DO UPDATE SET external_id = excluded.external_id
				`, mapID, it.entityType, it.entityID, adapterName, extID)
			}
		}
	}

	return res, nil
}

// SyncContacts loads customer and driver records and pushes them to the accounting adapter.
func (c *Consumer) SyncContacts(ctx context.Context) (SyncResult, error) {
	if c.db == nil {
		return SyncResult{}, nil
	}

	var contacts []Contact

	// Customers
	cRows, err := c.db.QueryContext(ctx, `SELECT id, name, COALESCE(email,''), phone, COALESCE(gst,''), COALESCE(address,'') FROM customers`)
	if err == nil {
		defer cRows.Close()
		for cRows.Next() {
			var id, name, email, phone, gst, addr string
			if err := cRows.Scan(&id, &name, &email, &phone, &gst, &addr); err == nil {
				contacts = append(contacts, Contact{
					ExternalID:  id,
					Name:        name,
					Email:       email,
					Phone:       phone,
					GSTIN:       gst,
					Address:     addr,
					ContactType: "customer",
				})
			}
		}
	}

	// Drivers
	dRows, err := c.db.QueryContext(ctx, `SELECT id, first_name || ' ' || COALESCE(last_name,''), phone FROM drivers`)
	if err == nil {
		defer dRows.Close()
		for dRows.Next() {
			var id, name, phone string
			if err := dRows.Scan(&id, &name, &phone); err == nil {
				contacts = append(contacts, Contact{
					ExternalID:  id,
					Name:        strings.TrimSpace(name),
					Phone:       phone,
					ContactType: "vendor",
				})
			}
		}
	}

	res, err := c.client.SyncContacts(ctx, contacts)
	if err != nil {
		return res, err
	}

	adapterName := strings.ToLower(c.cfg.Provider)
	if adapterName == "" {
		adapterName = "mock"
	}
	for _, ct := range contacts {
		mapID := "map-" + uuid.New().String()
		_, _ = c.db.ExecContext(ctx, `
			INSERT INTO accounting_mapping (id, entity_type, entity_id, adapter, external_id, created_at)
			VALUES ($1, $2, $3, $4, $5, CURRENT_TIMESTAMP)
			ON CONFLICT (entity_type, entity_id, adapter) DO UPDATE SET external_id = excluded.external_id
		`, mapID, ct.ContactType, ct.ExternalID, adapterName, "EXT-"+ct.ExternalID)
	}

	return res, nil
}

// Reconcile compares local sync logs against acknowledged external IDs.
func (c *Consumer) Reconcile(ctx context.Context) (ReconcileResult, error) {
	var res ReconcileResult
	if c.db == nil {
		return res, nil
	}

	var total, acked, unacked int
	_ = c.db.QueryRowContext(ctx, `SELECT count(*) FROM accounting_sync_log`).Scan(&total)
	_ = c.db.QueryRowContext(ctx, `SELECT count(*) FROM accounting_sync_log WHERE status = 'acked'`).Scan(&acked)
	_ = c.db.QueryRowContext(ctx, `SELECT count(*) FROM accounting_sync_log WHERE status != 'acked'`).Scan(&unacked)

	res.Total = total
	res.Acked = acked
	res.Unacked = unacked

	rows, err := c.db.QueryContext(ctx, `
		SELECT COALESCE(external_id, idempotency_key)
		FROM accounting_sync_log
		WHERE status != 'acked'
		LIMIT 50
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ref string
			if err := rows.Scan(&ref); err == nil && ref != "" {
				res.UnackedRefs = append(res.UnackedRefs, ref)
			}
		}
	}

	return res, nil
}

// GetStatus returns the current sync status.
func (c *Consumer) GetStatus(ctx context.Context) (SyncStatusResponse, error) {
	adapterName := strings.ToLower(c.cfg.Provider)
	if adapterName == "" {
		adapterName = "mock"
	}

	resp := SyncStatusResponse{
		Adapter: adapterName,
		Enabled: c.cfg.Enabled,
	}

	if c.db != nil {
		_ = c.db.QueryRowContext(ctx, `SELECT count(*) FROM accounting_sync_log WHERE status IN ('pending', 'failed')`).Scan(&resp.PendingEvents)

		var lastSync sql.NullTime
		_ = c.db.QueryRowContext(ctx, `SELECT MAX(updated_at) FROM accounting_sync_log WHERE status = 'acked'`).Scan(&lastSync)
		if lastSync.Valid {
			resp.LastSyncedAt = &lastSync.Time
		}
	}

	return resp, nil
}

func payloadToMap(p any) map[string]interface{} {
	if p == nil {
		return nil
	}
	if m, ok := p.(map[string]interface{}); ok {
		return m
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	return m
}

func extractEntityID(p map[string]interface{}) string {
	if p == nil {
		return ""
	}
	for _, key := range []string{"SettlementID", "settlement_id", "InvoiceID", "invoice_id", "TripID", "trip_id", "ID", "id"} {
		if val, ok := p[key]; ok && val != nil {
			if s, ok := val.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func inferEntityType(evtType string) string {
	switch {
	case strings.Contains(strings.ToLower(evtType), "settlement") || strings.Contains(strings.ToLower(evtType), "payout"):
		return "payout"
	case strings.Contains(strings.ToLower(evtType), "invoice"):
		return "invoice"
	case strings.Contains(strings.ToLower(evtType), "tds"):
		return "tds"
	default:
		return "journal"
	}
}

func extractAmountFromPayload(p map[string]interface{}) float64 {
	if p == nil {
		return 0
	}
	for _, k := range []string{"NetPayout", "net_payout", "GrossFare", "gross_fare", "TotalAmount", "total_amount", "Amount", "amount", "TDSAmount", "tds_amount"} {
		if v, ok := p[k]; ok {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case int64:
				return float64(val)
			}
		}
	}
	return 0
}

func parseInvoiceFromPayload(p map[string]interface{}, defaultID string) ExportedInvoice {
	inv := ExportedInvoice{
		ExternalID:    defaultID,
		InvoiceNumber: defaultID,
		InvoiceDate:   time.Now(),
		DueDate:       time.Now().AddDate(0, 0, 30),
	}
	if p == nil {
		return inv
	}
	if num, ok := p["InvoiceNumber"].(string); ok && num != "" {
		inv.InvoiceNumber = num
	}
	if cust, ok := p["CustomerName"].(string); ok && cust != "" {
		inv.CustomerName = cust
	}
	if gst, ok := p["CustomerGSTIN"].(string); ok && gst != "" {
		inv.CustomerGSTIN = gst
	}
	inv.TotalAmount = extractAmountFromPayload(p)
	return inv
}
