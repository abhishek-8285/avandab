package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/events"
	"transport-app/internal/repository"
	clockpkg "transport-app/internal/shared/clock"
	idpkg "transport-app/internal/shared/id"
	"transport-app/internal/shared/ports"
)

// OutboxWriter writes serialised domain events to the outbox database table.
type OutboxWriter struct {
	q     *db.Queries
	idGen ports.IDGenerator
	clock ports.Clock
}

// NewOutboxWriter constructs a new OutboxWriter.
func NewOutboxWriter(dbConn *sql.DB) *OutboxWriter {
	return &OutboxWriter{
		q:     db.New(dbConn),
		idGen: idpkg.NewUUIDGenerator(),
		clock: clockpkg.NewRealClock(),
	}
}

// SaveEvents serializes and inserts all events within the active transaction context.
func (w *OutboxWriter) SaveEvents(ctx context.Context, aggregateID string, aggregateType string, evts []any) error {
	if len(evts) == 0 {
		return nil
	}

	q := w.q
	if tx := repository.TxFromContext(ctx); tx != nil {
		q = w.q.WithTx(tx)
	}

	for _, ev := range evts {
		payload, err := json.Marshal(ev)
		if err != nil {
			return err
		}

		eventType := getEventTypeName(ev)

		err = q.CreateOutboxEvent(ctx, db.CreateOutboxEventParams{
			ID:            w.idGen.GenerateUUID(),
			AggregateID:   aggregateID,
			AggregateType: aggregateType,
			EventType:     eventType,
			Payload:       string(payload),
			CreatedAt:     w.clock.Now(),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func getEventTypeName(ev any) string {
	// Canonical catalog first: persisted event_type must match what
	// subscribers listen on (Spec 09 §5.1). Keys are matched by dynamic
	// TYPE, not map equality — interface keys compare by VALUE, so a
	// non-zero event (e.g. one carrying a real timestamp) would never
	// equal its zero-value registration key.
	for key, name := range events.EventTypeOf {
		if reflect.TypeOf(key) == reflect.TypeOf(ev) {
			return name
		}
	}
	t := fmt.Sprintf("%T", ev)
	if idx := strings.LastIndex(t, "."); idx != -1 {
		return t[idx+1:]
	}
	return t
}
