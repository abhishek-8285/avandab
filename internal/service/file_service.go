package service

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"transport-app/internal/domain"
	"transport-app/internal/storage"
)

// FileService handles file uploads and storage.
type FileService struct {
	baseService
	storage storage.Store
}

// SetStorage configures the storage backend for FileService.
func (s *FileService) SetStorage(store storage.Store) {
	s.storage = store
}

// UploadResult contains information about an uploaded file.
type UploadResult struct {
	File domain.File
}

// Allowed image types for uploads.
var AllowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// Allowed document types for uploads.
var AllowedDocTypes = map[string]bool{
	"application/pdf": true,
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
}

// allowedUploadContentTypes are the content types accepted by UploadFile,
// validated against the file's magic bytes.
var allowedUploadContentTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/gif":       true,
	"image/webp":      true,
	"application/pdf": true,
}

// UploadFile saves an uploaded file to disk and creates a database record.
func (s *FileService) UploadFile(ctx context.Context, header *multipart.FileHeader, uploadableType string, uploadableID string) (domain.File, error) {
	if header == nil {
		return domain.File{}, fmt.Errorf("no file provided")
	}

	file, err := header.Open()
	if err != nil {
		return domain.File{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Validate content by magic bytes from the first 512 bytes
	buf := make([]byte, 512)
	n, err := io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return domain.File{}, fmt.Errorf("failed to read file: %w", err)
	}
	if n == 0 {
		return domain.File{}, fmt.Errorf("empty file")
	}
	detected := http.DetectContentType(buf[:n])
	if !allowedUploadContentTypes[detected] {
		return domain.File{}, fmt.Errorf("unsupported file type %q", detected)
	}
	if declared := header.Header.Get("Content-Type"); declared != "" && declared != "application/octet-stream" && declared != detected {
		return domain.File{}, fmt.Errorf("declared content type %q does not match file content %q", declared, detected)
	}

	// Generate a safe unique filename: random ID + sanitized extension
	filename := uuid.NewString() + safeExtension(header.Filename, detected)

	// Determine upload subdirectory
	var subdir string
	switch uploadableType {
	case "driver_license":
		subdir = "drivers"
	case "vehicle_insurance", "vehicle_permit", "vehicle_rc", "vehicle_fitness", "vehicle_puc":
		subdir = "vehicles"
	case "company_logo", "logo":
		subdir = "company"
	case "trip_pod":
		subdir = "trips"
	case "expense_receipt":
		subdir = "expenses"
	default:
		subdir = "misc"
	}

	relPath := filepath.ToSlash(filepath.Join(subdir, filename))
	var localCreatedPath string

	// Rewind past the sniff bytes instead of replaying them through a
	// MultiReader: multipart.File is an io.ReadSeeker, so the S3 backend can
	// stream the upload (seek-to-end for length) rather than io.ReadAll it
	// into memory. buf is reused only for content-type detection above.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return domain.File{}, fmt.Errorf("failed to rewind file: %w", err)
	}

	if s.storage != nil {
		if _, err := s.storage.Save(ctx, relPath, file, detected); err != nil {
			return domain.File{}, fmt.Errorf("storage: save: %w", err)
		}
	} else {
		dir := filepath.Join(uploadDir(s), subdir)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return domain.File{}, fmt.Errorf("failed to create upload directory: %w", err)
		}

		path := filepath.Join(dir, filename)
		dest, err := os.Create(path)
		if err != nil {
			return domain.File{}, fmt.Errorf("failed to create file: %w", err)
		}
		if _, err := io.Copy(dest, file); err != nil {
			_ = dest.Close()
			_ = os.Remove(path)
			return domain.File{}, fmt.Errorf("failed to write file: %w", err)
		}
		if err := dest.Close(); err != nil {
			_ = os.Remove(path)
			return domain.File{}, fmt.Errorf("failed to close file: %w", err)
		}
		localCreatedPath = path
	}

	f := domain.File{
		ID:             domain.FileID(generateID()),
		Filename:       filename,
		OriginalName:   header.Filename,
		Path:           relPath,
		Size:           header.Size,
		MimeType:       detected,
		UploadableType: uploadableType,
		UploadableID:   &uploadableID,
	}

	created, err := s.store.CreateFile(ctx, f)
	if err != nil {
		if localCreatedPath != "" {
			_ = os.Remove(localCreatedPath)
		}
		if s.storage != nil {
			_ = s.storage.Delete(ctx, relPath)
		}
		return domain.File{}, err
	}

	s.log.Info("file uploaded", "file_id", created.ID, "type", uploadableType)
	return created, nil
}

// GetFile retrieves a file by ID.
func (s *FileService) GetFile(ctx context.Context, id domain.FileID) (domain.File, error) {
	return s.store.GetFileByID(ctx, id)
}

// OpenFile returns an io.ReadCloser for the stored file content. It queries
// the configured storage backend (e.g. S3/R2) first, and falls back to disk.
func (s *FileService) OpenFile(ctx context.Context, f domain.File) (io.ReadCloser, error) {
	if s.storage != nil && f.Path != "" {
		return s.storage.Open(ctx, f.Path)
	}
	baseDir := filepath.Clean(uploadDir(s))
	if baseDir == "." {
		baseDir = ""
	}
	cleanPath := filepath.Clean(filepath.Join(baseDir, f.Path))
	if baseDir != "" && !strings.HasPrefix(cleanPath, baseDir+string(os.PathSeparator)) && cleanPath != baseDir {
		return nil, fmt.Errorf("storage: invalid file path %q", f.Path)
	}
	return os.Open(cleanPath)
}

// GetFilesByEntity retrieves files for a specific entity.
func (s *FileService) GetFilesByEntity(ctx context.Context, uploadableType string, uploadableID string) ([]domain.File, error) {
	return s.store.GetFilesByUploadable(ctx, uploadableType, uploadableID)
}

// DeleteFile removes a file from storage and database.
func (s *FileService) DeleteFile(ctx context.Context, id domain.FileID) error {
	f, err := s.store.GetFileByID(ctx, id)
	if err != nil {
		return err
	}

	// Delete from storage engine if configured
	if s.storage != nil && f.Path != "" {
		_ = s.storage.Delete(ctx, f.Path)
	}

	// Delete from disk safely (validate path is within upload directory)
	if f.Path != "" {
		baseDir := filepath.Clean(uploadDir(s))
		if baseDir == "." {
			baseDir = ""
		}
		cleanPath := filepath.Clean(filepath.Join(baseDir, f.Path))
		if baseDir == "" || strings.HasPrefix(cleanPath, baseDir+string(os.PathSeparator)) || cleanPath == baseDir {
			_ = os.Remove(cleanPath)
		}
	}

	// Delete from database
	if err := s.store.DeleteFile(ctx, id); err != nil {
		return err
	}

	s.log.Info("file deleted", "file_id", id)
	return nil
}

// uploadDir returns the configured uploads directory, defaulting to ./uploads.
func uploadDir(s *FileService) string {
	if s.cfg != nil && s.cfg.UploadDir != "" {
		return s.cfg.UploadDir
	}
	return "./uploads"
}

// safeExtension returns a whitelisted lowercase extension for the stored file,
// derived from the original filename; falls back to the extension matching the
// detected content type. Never includes path separators or "..".
func safeExtension(filename, contentType string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filepath.Clean(filename))))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf":
		if ext == ".jpeg" {
			return ".jpg"
		}
		return ext
	}
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}
