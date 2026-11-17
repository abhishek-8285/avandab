package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/domain"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
)

// maxFileUploadBytes caps any single Files API upload (25 MB).
const maxFileUploadBytes = 25 << 20

// allowedFilesAPITypes mirrors the files.uploadable_type CHECK constraint
// widened by migration 00077. Generic polymorphic uploads, Fleetbase-style.
var allowedFilesAPITypes = map[string]bool{
	"driver_license":    true,
	"vehicle_insurance": true,
	"vehicle_permit":    true,
	"company_logo":      true,
	"vehicle_rc":        true,
	"vehicle_fitness":   true,
	"vehicle_puc":       true,
	"trip_pod":          true,
	"expense_receipt":   true,
	"logo":              true,
	"general":           true,
}

// FilesAPIHandlers exposes the REST file/image API:
//
//	POST   /api/v1/files                multipart upload (file, uploadable_type, uploadable_id)
//	GET    /api/v1/files                list by uploadable_type + uploadable_id
//	GET    /api/v1/files/{id}           file metadata
//	DELETE /api/v1/files/{id}           delete file record + blob
type FilesAPIHandlers struct {
	*App
	files   *service.FileService
	authSrv auth.AuthorizationService
}

// NewFilesAPIHandlers constructs a FilesAPIHandlers instance.
func NewFilesAPIHandlers(app *App, files *service.FileService, authSrv auth.AuthorizationService) *FilesAPIHandlers {
	return &FilesAPIHandlers{App: app, files: files, authSrv: authSrv}
}

// Mount registers the Files API routes under both /api/documents-style
// prefixes used elsewhere in this codebase.
func (h *FilesAPIHandlers) Mount(r chi.Router) {
	setupRoutes := func(sub chi.Router) {
		sub.With(middleware.RequirePermission(h.authSrv, "files", "create")).Post("/", h.Upload)
		sub.With(middleware.RequirePermission(h.authSrv, "files", "read")).Get("/", h.List)
		sub.With(middleware.RequirePermission(h.authSrv, "files", "read")).Get("/{id}", h.Get)
		sub.With(middleware.RequirePermission(h.authSrv, "files", "delete")).Delete("/{id}", h.Delete)
	}

	r.Route("/api/files", setupRoutes)
	r.Route("/api/v1/files", setupRoutes)
}

// fileResource is the JSON representation returned by the Files API.
type fileResource struct {
	ID             string `json:"id"`
	Filename       string `json:"filename"`
	OriginalName   string `json:"original_name"`
	Size           int64  `json:"size"`
	ContentType    string `json:"content_type"`
	URL            string `json:"url"`
	UploadableType string `json:"uploadable_type"`
	UploadableID   string `json:"uploadable_id,omitempty"`
	CreatedAt      string `json:"created_at"`
}

func toFileResource(f domain.File) fileResource {
	res := fileResource{
		ID:             string(f.ID),
		Filename:       f.Filename,
		OriginalName:   f.OriginalName,
		Size:           f.Size,
		ContentType:    f.MimeType,
		URL:            "/files/" + string(f.ID),
		UploadableType: f.UploadableType,
		CreatedAt:      f.CreatedAt.Format(time.RFC3339),
	}
	if f.UploadableID != nil {
		res.UploadableID = *f.UploadableID
	}
	return res
}

// Upload handles multipart file upload with polymorphic attachment.
func (h *FilesAPIHandlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadBytes+1<<20)
	if err := r.ParseMultipartForm(maxFileUploadBytes); err != nil {
		slog.Warn("files api: multipart parse failed", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_multipart", "message": "multipart form could not be parsed"})
		return
	}

	uploadableType := r.FormValue("uploadable_type")
	if !allowedFilesAPITypes[uploadableType] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_uploadable_type", "message": "uploadable_type must be one of the supported entity types"})
		return
	}
	uploadableID := r.FormValue("uploadable_id")

	_, fh, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_file", "message": "multipart field \"file\" is required"})
		return
	}

	fileRec, err := h.files.UploadFile(r.Context(), fh, uploadableType, uploadableID)
	if err != nil {
		slog.Warn("files api: upload failed", "error", err)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload_failed", "message": "upload failed"})
		return
	}

	writeJSON(w, http.StatusCreated, toFileResource(fileRec))
}

// List returns files attached to an entity.
func (h *FilesAPIHandlers) List(w http.ResponseWriter, r *http.Request) {
	uploadableType := r.URL.Query().Get("uploadable_type")
	uploadableID := r.URL.Query().Get("uploadable_id")
	if uploadableType == "" || uploadableID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_params", "message": "uploadable_type and uploadable_id query params are required"})
		return
	}

	files, err := h.files.GetFilesByEntity(r.Context(), uploadableType, uploadableID)
	if err != nil {
		slog.Warn("files api: list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "list_failed", "message": "could not list files"})
		return
	}

	resources := make([]fileResource, 0, len(files))
	for _, f := range files {
		resources = append(resources, toFileResource(f))
	}
	writeJSON(w, http.StatusOK, resources)
}

// Get returns file metadata by ID.
func (h *FilesAPIHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	file, err := h.files.GetFile(r.Context(), domain.FileID(id))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found", "message": "file does not exist"})
		return
	}
	writeJSON(w, http.StatusOK, toFileResource(file))
}

// Delete removes a file record and its stored blob.
func (h *FilesAPIHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.files.GetFile(r.Context(), domain.FileID(id)); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found", "message": "file does not exist"})
		return
	}
	if err := h.files.DeleteFile(r.Context(), domain.FileID(id)); err != nil {
		slog.Warn("files api: delete failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete_failed", "message": "could not delete file"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
