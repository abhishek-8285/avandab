package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/agent"
	"transport-app/internal/auth"
)

// Server exposes fleet operations and assistant tools over the Model Context Protocol (MCP).
type Server struct {
	tools           map[string]*agent.RegisteredTool
	toolList        []ToolDescriptor
	approvalSvc     *agent.ApprovalService
	permChecker     agent.PermissionChecker
	requireApproval bool
	sseSessions     sync.Map // sessionId string -> chan []byte
}

// ServerConfig configures the MCP server.
type ServerConfig struct {
	ToolEnv         *agent.ToolEnv
	Tools           []*agent.RegisteredTool
	ApprovalService *agent.ApprovalService
	PermChecker     agent.PermissionChecker
	RequireApproval bool
}

// NewServer builds an MCP server bound to fleet tools and security controls.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		tools:           make(map[string]*agent.RegisteredTool),
		approvalSvc:     cfg.ApprovalService,
		permChecker:     cfg.PermChecker,
		requireApproval: cfg.RequireApproval,
	}

	sourceTools := cfg.Tools
	if sourceTools == nil && cfg.ToolEnv != nil {
		sourceTools = agent.RegisterTools(cfg.ToolEnv)
	}

	mutatingMap := make(map[string]bool)
	for _, m := range agent.MutatingTools() {
		mutatingMap[m] = true
	}

	for _, t := range sourceTools {
		tool := t
		if s.requireApproval && mutatingMap[tool.Name] {
			if s.approvalSvc != nil {
				tool = s.approvalSvc.GatedTool(tool)
			} else {
				// Fail closed: omit mutating tools when approval is required but service is unavailable
				continue
			}
		}

		s.tools[tool.Name] = tool
		schema := tool.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		s.toolList = append(s.toolList, ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}

	return s
}

// Tools returns the list of registered MCP tool descriptors.
func (s *Server) Tools() []ToolDescriptor {
	out := make([]ToolDescriptor, len(s.toolList))
	copy(out, s.toolList)
	return out
}

// RegisterRoutes mounts the MCP endpoints on a chi.Router:
// - /api/v1/mcp (POST JSON-RPC and GET SSE)
// - /api/v1/mcp/sse (GET SSE alias)
// - /api/v1/mcp/message (POST JSON-RPC alias for SSE endpoint callbacks)
func (s *Server) RegisterRoutes(r chi.Router) {
	r.HandleFunc("/api/v1/mcp", s.ServeHTTP)
	r.HandleFunc("/api/v1/mcp/sse", s.ServeHTTP)
	r.HandleFunc("/api/v1/mcp/message", s.ServeHTTP)
}

// ServeHTTP handles both JSON-RPC POST requests and SSE stream connections.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Role check from authenticated context (RequireAPIAuth)
	session, _ := r.Context().Value(auth.ContextUser).(*auth.SessionData)
	if session != nil {
		switch session.Role {
		case "admin", "org_admin", "dispatcher", "accountant":
			// trusted staff roles allowed
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "forbidden",
				"message": "insufficient permissions for MCP",
			})
			return
		}
	}

	ctx := r.Context()
	if session != nil {
		userID := session.UserID
		operatorName := session.Name
		if operatorName == "" {
			operatorName = "API user " + userID
		}
		ctx = agent.WithAgentUser(ctx, userID, operatorName)
	}
	if s.permChecker != nil {
		ctx = agent.WithPermissionChecker(ctx, s.permChecker)
	}

	// Transport dispatch
	isSSE := r.Method == http.MethodGet && (strings.Contains(r.Header.Get("Accept"), "text/event-stream") || strings.HasSuffix(r.URL.Path, "/sse"))
	if isSSE {
		s.handleSSE(w, r, ctx)
		return
	}

	if r.Method == http.MethodPost {
		s.handlePOST(w, r, ctx)
		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request, _ context.Context) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.NewString()
	msgChan := make(chan []byte, 32)
	s.sseSessions.Store(sessionID, msgChan)
	defer s.sseSessions.Delete(sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Send endpoint event for client POST submissions
	endpoint := fmt.Sprintf("/api/v1/mcp?sessionId=%s", sessionID)
	_, _ = fmt.Fprintf(w, "event: endpoint\r\ndata: %s\r\n\r\n", endpoint)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}
			_, _ = fmt.Fprintf(w, "event: message\r\ndata: %s\r\n\r\n", string(msg))
			flusher.Flush()
		}
	}
}

func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, NewErrorResponse(nil, CodeParseError, "failed to read request body"))
		return
	}
	defer func() { _ = r.Body.Close() }()

	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		s.writeJSON(w, http.StatusBadRequest, NewErrorResponse(nil, CodeParseError, "empty request body"))
		return
	}

	// Batch JSON-RPC request: array starting with '['
	if trimmed[0] == '[' {
		var reqs []Request
		if err := json.Unmarshal(trimmed, &reqs); err != nil {
			s.writeJSON(w, http.StatusOK, NewErrorResponse(nil, CodeParseError, "invalid JSON batch"))
			return
		}
		var resps []Response
		for _, req := range reqs {
			resp := s.dispatch(ctx, req)
			if resp != nil {
				resps = append(resps, *resp)
			}
		}
		s.deliver(w, r, resps)
		return
	}

	// Single JSON-RPC request
	var req Request
	if err := json.Unmarshal(trimmed, &req); err != nil {
		s.writeJSON(w, http.StatusOK, NewErrorResponse(nil, CodeParseError, "invalid JSON-RPC request"))
		return
	}
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		s.writeJSON(w, http.StatusOK, NewErrorResponse(req.ID, CodeInvalidRequest, "invalid jsonrpc version (must be 2.0)"))
		return
	}

	resp := s.dispatch(ctx, req)
	if resp == nil {
		// Notification has no response
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.deliver(w, r, resp)
}

func (s *Server) deliver(w http.ResponseWriter, r *http.Request, data any) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID != "" {
		if chVal, ok := s.sseSessions.Load(sessionID); ok {
			ch, isChan := chVal.(chan []byte)
			if isChan {
				b, err := json.Marshal(data)
				if err == nil {
					select {
					case ch <- b:
						w.WriteHeader(http.StatusAccepted)
						return
					default:
						// channel full
					}
				}
			}
		}
	}

	// Direct HTTP response
	s.writeJSON(w, http.StatusOK, data)
}

func (s *Server) dispatch(ctx context.Context, req Request) *Response {
	switch req.Method {
	case "initialize":
		res := InitializeResult{
			ProtocolVersion: ProtocolVersion,
			Capabilities: ServerCapabilities{
				Tools: ToolCapabilities{ListChanged: false},
			},
			ServerInfo: ServerInfo{
				Name:    "avandab-fleet-mcp",
				Version: "1.0.0",
			},
			Instructions: "Avandab Fleet Operations MCP server. Provides real-time fleet queries (routes, bookings, trips, drivers, vehicles, invoices, kharcha, maintenance) and approval-gated mutations.",
		}
		r := NewResponse(req.ID, res)
		return &r

	case "notifications/initialized":
		return nil

	case "ping":
		r := NewResponse(req.ID, map[string]any{})
		return &r

	case "tools/list":
		r := NewResponse(req.ID, ToolsListResult{Tools: s.toolList})
		return &r

	case "tools/call":
		return s.handleToolCall(ctx, req)

	default:
		r := NewErrorResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
		return &r
	}
}

func (s *Server) handleToolCall(ctx context.Context, req Request) *Response {
	var params CallToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		r := NewResponse(req.ID, NewErrorResult(fmt.Sprintf("invalid tool params: %v", err)))
		return &r
	}

	tool, ok := s.tools[params.Name]
	if !ok {
		r := NewResponse(req.ID, NewErrorResult(fmt.Sprintf("tool %q not found", params.Name)))
		return &r
	}

	// Attach an episode ID for audit logging and approval workflow
	ctx = agent.WithEpisodeID(ctx, "mcp-"+uuid.NewString())

	args := params.Arguments
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}

	output, err := tool.Execute(ctx, args)
	if err != nil {
		slog.WarnContext(ctx, "mcp: tool execution error", "tool", params.Name, "error", err)
		r := NewResponse(req.ID, NewErrorResult(err.Error()))
		return &r
	}

	r := NewResponse(req.ID, NewTextResult(output))
	return &r
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
