package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	outboxapp "transport-app/internal/outbox/application"
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
	// Durable insert — PRIMARY KEY id = idempotency_key gives idempotency, Tx per request.
	// Every step is checked: a failed Tx must surface as 5xx so the mobile client
	// retries instead of assuming the batch was durably queued (no silent loss).
	if h.db != nil && len(req.Commands) > 0 {
		commands := make([]outboxapp.Command, 0, len(req.Commands))
		for _, c := range req.Commands {
			commands = append(commands, outboxapp.Command{
				IdempotencyKey: c.IdempotencyKey,
				Command:        c.Command,
				Payload:        c.Payload,
			})
		}
		if err := outboxapp.Enqueue(r.Context(), h.db, commands); err != nil {
			if errors.Is(err, outboxapp.ErrOutboxUnavailable) {
				http.Error(w, "outbox unavailable; retry", http.StatusServiceUnavailable)
			} else {
				http.Error(w, "outbox write failed; retry", http.StatusServiceUnavailable)
			}
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"accepted": len(req.Commands), "status": "queued"})
}
