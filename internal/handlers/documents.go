package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"transport-app/internal/auth"
	"transport-app/internal/middleware"
	"transport-app/internal/service"
)

// DocumentHandlers provides endpoints for document vault uploads, listing, and verification.
type DocumentHandlers struct {
	*App
	docSvc  *service.DocumentService
	authSrv auth.AuthorizationService
}

// NewDocumentHandlers constructs a DocumentHandlers instance.
func NewDocumentHandlers(app *App, docSvc *service.DocumentService, authSrv auth.AuthorizationService) *DocumentHandlers {
	return &DocumentHandlers{
		App:     app,
		docSvc:  docSvc,
		authSrv: authSrv,
	}
}

// Mount registers document vault routes.
func (h *DocumentHandlers) Mount(r chi.Router) {
	setupRoutes := func(sub chi.Router) {
		sub.With(middleware.RequirePermission(h.authSrv, "documents", "upload")).Post("/driver/{driver_id}", h.UploadDriverDoc)
		sub.With(middleware.RequirePermission(h.authSrv, "documents", "upload")).Post("/vehicle/{vehicle_id}", h.UploadVehicleDoc)
		sub.With(middleware.RequirePermission(h.authSrv, "documents", "read")).Get("/driver/{driver_id}", h.ListDriverDocs)
		sub.With(middleware.RequirePermission(h.authSrv, "documents", "read")).Get("/vehicle/{vehicle_id}", h.ListVehicleDocs)
		sub.With(middleware.RequirePermission(h.authSrv, "documents", "upload")).Post("/verify/{entity_type}/{entity_id}/{document_id}", h.VerifyDoc)
	}

	r.Route("/api/documents", setupRoutes)
	r.Route("/api/v1/documents", setupRoutes)
}

// UploadDriverDoc handles multipart upload for a driver document.
func (h *DocumentHandlers) UploadDriverDoc(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "driver_id")
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, `{"error":"invalid_form","message":"failed to parse multipart form"}`, http.StatusBadRequest)
		return
	}

	docType := r.FormValue("doc_type")
	expiryStr := r.FormValue("expiry_date")
	var expiryDate *time.Time
	if expiryStr != "" {
		if t, err := time.Parse("2006-01-02", expiryStr); err == nil {
			expiryDate = &t
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file_required","message":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("close document upload failed", "error", err)
		}
	}()

	doc, err := h.docSvc.UploadDriverDoc(r.Context(), driverID, docType, header, expiryDate)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, service.ErrInvalidDocType) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_doc_type", "message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "upload_failed", "message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(doc)
}

// UploadVehicleDoc handles multipart upload for a vehicle document.
func (h *DocumentHandlers) UploadVehicleDoc(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "vehicle_id")
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, `{"error":"invalid_form","message":"failed to parse multipart form"}`, http.StatusBadRequest)
		return
	}

	docType := r.FormValue("doc_type")
	expiryStr := r.FormValue("expiry_date")
	var expiryDate *time.Time
	if expiryStr != "" {
		if t, err := time.Parse("2006-01-02", expiryStr); err == nil {
			expiryDate = &t
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"file_required","message":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Warn("close document upload failed", "error", err)
		}
	}()

	doc, err := h.docSvc.UploadVehicleDoc(r.Context(), vehicleID, docType, header, expiryDate)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, service.ErrInvalidDocType) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_doc_type", "message": err.Error()})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "upload_failed", "message": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(doc)
}

// ListDriverDocs returns list of documents for a driver.
func (h *DocumentHandlers) ListDriverDocs(w http.ResponseWriter, r *http.Request) {
	driverID := chi.URLParam(r, "driver_id")
	docs, err := h.docSvc.ListDriverDocs(r.Context(), driverID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"driver_id": driverID,
		"documents": docs,
		"count":     len(docs),
	})
}

// ListVehicleDocs returns list of documents for a vehicle.
func (h *DocumentHandlers) ListVehicleDocs(w http.ResponseWriter, r *http.Request) {
	vehicleID := chi.URLParam(r, "vehicle_id")
	docs, err := h.docSvc.ListVehicleDocs(r.Context(), vehicleID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"vehicle_id": vehicleID,
		"documents":  docs,
		"count":      len(docs),
	})
}

// VerifyDoc handles document verification.
func (h *DocumentHandlers) VerifyDoc(w http.ResponseWriter, r *http.Request) {
	entityType := chi.URLParam(r, "entity_type")
	entityID := chi.URLParam(r, "entity_id")
	docID := chi.URLParam(r, "document_id")

	user, _ := h.getUserFromContext(r)
	verifiedBy := "admin"
	if user != nil {
		verifiedBy = user.UserID
	}

	err := h.docSvc.VerifyDocument(r.Context(), entityType, entityID, docID, verifiedBy)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if errors.Is(err, service.ErrDocumentNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "document_not_found"})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "verified",
		"verified_by": verifiedBy,
		"document_id": docID,
		"message":     fmt.Sprintf("document %s verified successfully", docID),
	})
}
