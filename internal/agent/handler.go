package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
)

// Handler exposes the agent over HTTP.
type Handler struct {
	orch *Orchestrator
	env  *ToolEnv
}

func NewHandler(orch *Orchestrator, env *ToolEnv) *Handler {
	return &Handler{orch: orch, env: env}
}

// RegisterRoutes mounts the agent chat endpoint on a chi router.
// Callers choose the auth middleware: session (web) or bearer (API).
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/assistant/chat", h.handleChat)
}

// RegisterAPIRoute mounts only the API endpoint (for bearer-token groups).
func (h *Handler) RegisterAPIRoute(r chi.Router) {
	r.Post("/api/agent/chat", h.handleChat)
}

type agentChatRequest struct {
	Messages []Message `json:"messages"`
}

func (h *Handler) handleChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	session, ok := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if !ok || session == nil {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	// Viewers can read dashboards via the UI; the agent's read tools expose
	// customer contact details, so require at least a dispatcher role.
	if session.Role == "viewer" {
		writeErr(w, http.StatusForbidden, "insufficient permissions for the assistant")
		return
	}

	userID, operatorName := session.UserID, session.Name
	if operatorName == "" {
		operatorName = "API user " + userID
	}
	// Identity travels per-request via context, never via shared state.
	ctx := context.WithValue(r.Context(), userIDCtxKey, userID)
	ctx = context.WithValue(ctx, userNameCtxKey, operatorName)

	var req agentChatRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "messages are required")
		return
	}

	answer, episodeID, err := h.orch.Handle(ctx, req.Messages, userID, operatorName)
	if err != nil {
		// Never leak provider/DB internals to the client.
		slog.ErrorContext(ctx, "assistant: request failed",
			slog.String("episode_id", episodeID),
			slog.String("user_id", userID),
			slog.Any("error", err))
		writeErr(w, http.StatusInternalServerError, "assistant request failed, please try again")
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"reply":      answer,
		"operator":   operatorName,
		"episode_id": episodeID,
	})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
