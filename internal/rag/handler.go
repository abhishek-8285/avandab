package rag

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service     *Service
	allowedDirs []string
	readGuard   func(http.Handler) http.Handler
	writeGuard  func(http.Handler) http.Handler
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// WithPermissionGuards attaches authorization middleware to API routes:
// readGuard protects search/stats, writeGuard protects index/reindex/
// teach/upload. Passing nil for either leaves those routes unguarded
// (useful for tests); production wiring must supply both.
func (h *Handler) WithPermissionGuards(read, write func(http.Handler) http.Handler) *Handler {
	h.readGuard = read
	h.writeGuard = write
	return h
}

// WithAllowedDirs restricts which directories /api/rag/index and
// /api/rag/reindex may index. An empty list disables directory indexing
// entirely (fail-closed): callers must use the configured allow-list.
func (h *Handler) WithAllowedDirs(dirs []string) *Handler {
	clean := make([]string, 0, len(dirs))
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		abs, err := filepath.Abs(d)
		if err == nil {
			d = abs
		}
		clean = append(clean, filepath.Clean(d))
	}
	h.allowedDirs = clean
	return h
}

// allowedDirectory reports whether dir is an absolute prefix of an allowed directory.
// Path-traversal and symlink escapes are rejected by resolving to absolute paths.
func (h *Handler) allowedDirectory(dir string) bool {
	if len(h.allowedDirs) == 0 {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	for _, allowed := range h.allowedDirs {
		if abs == allowed || strings.HasPrefix(abs, allowed+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// Service exposes the underlying RAG service (for the agent support tool).
func (h *Handler) Service() *Service {
	if h == nil {
		return nil
	}
	return h.service
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	guard := func(g func(http.Handler) http.Handler, fn http.HandlerFunc) http.HandlerFunc {
		if g == nil {
			return fn
		}
		wrapped := g(fn)
		return wrapped.ServeHTTP
	}
	r.Post("/api/rag/search", guard(h.readGuard, h.handleSearch))
	r.Get("/api/rag/stats", guard(h.readGuard, h.handleStats))
	r.Post("/api/rag/index", guard(h.writeGuard, h.handleIndex))
	r.Post("/api/rag/reindex", guard(h.writeGuard, h.handleReindex))
	r.Post("/api/rag/teach", guard(h.writeGuard, h.handleTeach))
	r.Post("/api/rag/upload", guard(h.writeGuard, h.handleUpload))
}

type searchRequest struct {
	Query  string `json:"query"`
	TopK   int    `json:"top_k"`
	Source string `json:"source"`
}

type indexRequest struct {
	Directory string `json:"directory"`
}

type APIError struct {
	Error string `json:"error"`
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req searchRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "failed to read request body"})
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "invalid JSON"})
		return
	}

	if req.Query == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "query is required"})
		return
	}

	if req.TopK <= 0 {
		req.TopK = 5
	}

	result, err := h.service.Query(req.Query, req.TopK)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: err.Error()})
		return
	}

	// Filter by source if specified
	if req.Source != "" {
		var filtered []VectorEntry
		var filteredScores []float64
		for i, c := range result.Chunks {
			if c.Source == req.Source {
				filtered = append(filtered, c)
				filteredScores = append(filteredScores, result.Scores[i])
			}
		}
		result.Chunks = filtered
		result.Scores = filteredScores
		result.Total = len(filtered)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req indexRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "failed to read request body"})
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "invalid JSON"})
		return
	}

	if req.Directory == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "directory is required"})
		return
	}

	if !h.allowedDirectory(req.Directory) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(APIError{Error: "directory is not in the configured allow-list"})
		return
	}

	count, err := h.service.IndexDirectory(req.Directory)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message":   "indexed successfully",
		"chunks":    count,
		"directory": req.Directory,
	})
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	count, err := h.service.Stats()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"total_chunks": count,
	})
}

func (h *Handler) handleReindex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req indexRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "failed to read request body"})
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "invalid JSON"})
		return
	}

	if req.Directory == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "directory is required"})
		return
	}

	if !h.allowedDirectory(req.Directory) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(APIError{Error: "directory is not in the configured allow-list"})
		return
	}

	count, err := h.service.Reindex(req.Directory)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message":   "reindexed successfully",
		"chunks":    count,
		"directory": req.Directory,
	})
}

type teachRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (h *Handler) handleTeach(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req teachRequest
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "failed to read request body"})
		return
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "invalid JSON"})
		return
	}

	if req.Content == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "content is required"})
		return
	}

	count, err := h.service.Teach(req.Name, req.Content)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "taught successfully",
		"chunks":  count,
		"name":    req.Name,
	})
}

func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "file is required"})
		return
	}
	defer file.Close()

	if header.Size > 10<<20 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "file too large (max 10MB)"})
		return
	}

	// Sanitize the client-supplied filename: strip any directory
	// components so the write target always stays inside uploadDir.
	base := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if base == "" || base == "." || base == string(os.PathSeparator) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "invalid file name"})
		return
	}
	tmpPath := filepath.Join(h.service.uploadDir, "rag_upload_"+base)
	if !strings.HasPrefix(tmpPath, filepath.Clean(h.service.uploadDir)+string(os.PathSeparator)) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: "invalid file name"})
		return
	}
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: "failed to save file"})
		return
	}
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmpFile, file); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(APIError{Error: "failed to save file"})
		return
	}
	tmpFile.Close()

	count, err := h.service.UploadFile(tmpPath)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "uploaded and indexed successfully",
		"chunks":  count,
		"file":    header.Filename,
	})
}
