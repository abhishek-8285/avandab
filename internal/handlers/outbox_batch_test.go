package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "modernc.org/sqlite"
)

func newOutboxTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Schema mirrors migration 00020 outbox_events.
	_, err = db.Exec(`CREATE TABLE outbox_events (
		id TEXT PRIMARY KEY,
		aggregate_id TEXT NOT NULL,
		aggregate_type TEXT NOT NULL,
		event_type TEXT NOT NULL,
		payload TEXT,
		created_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create outbox_events: %v", err)
	}
	return db
}

func TestOutboxBatch_SuccessPersistsAllCommands(t *testing.T) {
	db := newOutboxTestDB(t)
	h := NewOutboxBatchHandler(db)

	body, _ := json.Marshal(map[string]interface{}{
		"commands": []map[string]string{
			{"idempotency_key": "cmd-1", "command": "pod.upload", "payload": `{"trip":"t1"}`},
			{"idempotency_key": "", "command": "expense.log", "payload": `{"amount":100}`},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/outbox/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleBatch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 persisted events, got %d", n)
	}
}

func TestOutboxBatch_IdempotentReplay(t *testing.T) {
	db := newOutboxTestDB(t)
	h := NewOutboxBatchHandler(db)

	body, _ := json.Marshal(map[string]interface{}{
		"commands": []map[string]string{
			{"idempotency_key": "cmd-dup", "command": "pod.upload", "payload": `{}`},
		},
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/outbox/batch", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.HandleBatch(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("replay %d: expected 200, got %d", i, rec.Code)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected idempotent replay to keep 1 event, got %d", n)
	}
}

// TestOutboxBatch_BrokenDBReturns503 pins the no-silent-loss fix: when the
// transaction fails, the handler must return 5xx so the mobile client retries
// instead of believing the batch was durably queued.
func TestOutboxBatch_BrokenDBReturns503(t *testing.T) {
	db := newOutboxTestDB(t)
	h := NewOutboxBatchHandler(db)
	// Kill the connection pool so BeginTx fails.
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"commands": []map[string]string{
			{"idempotency_key": "cmd-x", "command": "pod.upload", "payload": `{}`},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/outbox/batch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleBatch(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on failed transaction, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestOutboxBatch_InvalidBodyReturns400(t *testing.T) {
	h := NewOutboxBatchHandler(newOutboxTestDB(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/outbox/batch", bytes.NewReader([]byte("not-json")))
	rec := httptest.NewRecorder()
	h.HandleBatch(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
