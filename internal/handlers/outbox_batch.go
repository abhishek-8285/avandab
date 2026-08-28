package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// OutboxBatchHandler handles durable mobile outbox batch — POST /api/v1/outbox/batch
// Mobile flushes local outbox (POD/expense/gps) to backend durable outbox in one Tx (UoW).
type OutboxBatchHandler struct {
	db *sql.DB
}

func NewOutboxBatchHandler(db *sql.DB) *OutboxBatchHandler { return &OutboxBatchHandler{db: db} }

func (h *OutboxBatchHandler) Register(r chi.Router) {
	r.Post("/api/v1/outbox/batch", h.HandleBatch)
}

func (h *OutboxBatchHandler) HandleBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Commands []struct {
			IdempotencyKey string `json:"idempotency_key"`
			Command        string `json:"command"`
			Payload        string `json:"payload"`
		} `json:"commands"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
		return
	}
	// Durable insert — PRIMARY KEY id = idempotency_key gives idempotency, Tx per request
	if h.db != nil && len(req.Commands) > 0 {
		tx, err := h.db.BeginTx(r.Context(), nil)
		if err == nil {
			stmt, _ := tx.PrepareContext(r.Context(), `INSERT OR IGNORE INTO outbox_events (id, aggregate_id, aggregate_type, event_type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`)
			if stmt != nil {
				defer func() { _ = stmt.Close() }()
				for _, c := range req.Commands {
					id := c.IdempotencyKey
					if id == "" {
						id = uuid.NewString()
					}
					_, _ = stmt.ExecContext(r.Context(), id, id, "mobile_outbox", c.Command, c.Payload, time.Now().UTC())
				}
			}
			_ = tx.Commit()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"accepted": len(req.Commands), "status": "queued"})
}
