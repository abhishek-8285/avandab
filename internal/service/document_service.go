package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime/multipart"
	"time"

	"github.com/google/uuid"

	"transport-app/internal/repository"
)

var (
	ErrInvalidDocType   = errors.New("invalid_doc_type")
	ErrDocumentNotFound = errors.New("document_not_found")
	ErrEntityNotFound   = errors.New("entity_not_found")
	ErrFileRequired     = errors.New("file_required")
)

// DriverDocument represents a driver document uploaded to the vault.
type DriverDocument struct {
	ID         string     `json:"id"`
	DriverID   string     `json:"driver_id"`
	DocType    string     `json:"doc_type"`
	FileURL    string     `json:"file_url"`
	ExpiryDate *time.Time `json:"expiry_date,omitempty"`
	Status     string     `json:"status"` // pending_review, verified, rejected
	VerifiedBy *string    `json:"verified_by,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// VehicleDocument represents a vehicle document uploaded to the vault.
type VehicleDocument struct {
	ID         string     `json:"id"`
	VehicleID  string     `json:"vehicle_id"`
	DocType    string     `json:"doc_type"`
	FileURL    string     `json:"file_url"`
	ExpiryDate *time.Time `json:"expiry_date,omitempty"`
	Status     string     `json:"status"` // pending_review, verified, rejected
	VerifiedBy *string    `json:"verified_by,omitempty"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// DocumentService manages document storage, listing, and verification workflows.
type DocumentService struct {
	baseService
	files *FileService
}

// NewDocumentService constructs a DocumentService.
func NewDocumentService(bs baseService, files *FileService) *DocumentService {
	return &DocumentService{
		baseService: bs,
		files:       files,
	}
}

var validDriverDocTypes = map[string]bool{
	"aadhaar":    true,
	"pan":        true,
	"dl":         true,
	"bank_proof": true,
	"medical":    true,
	"other":      true,
}

var validVehicleDocTypes = map[string]bool{
	"rc":        true,
	"insurance": true,
	"puc":       true,
	"fitness":   true,
	"permit":    true,
	"others":    true,
}

// UploadDriverDoc stores a driver document in the vault.
func (s *DocumentService) UploadDriverDoc(ctx context.Context, driverID, docType string, header *multipart.FileHeader, expiryDate *time.Time) (DriverDocument, error) {
	if !validDriverDocTypes[docType] {
		return DriverDocument{}, fmt.Errorf("%w: %s", ErrInvalidDocType, docType)
	}
	if header == nil {
		return DriverDocument{}, ErrFileRequired
	}

	uploadableType := "driver_license"

	fileURL := ""
	if s.files != nil {
		uploaded, err := s.files.UploadFile(ctx, header, uploadableType, driverID)
		if err != nil {
			return DriverDocument{}, fmt.Errorf("upload file: %w", err)
		}
		fileURL = "/uploads/" + uploaded.Path
	} else {
		fileURL = "/uploads/drivers/" + header.Filename
	}

	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return DriverDocument{}, fmt.Errorf("database unavailable")
	}
	db := getter.DB()

	docID := "ddoc-" + uuid.New().String()
	now := time.Now().UTC()

	var expStr *string
	if expiryDate != nil {
		s := expiryDate.Format("2006-01-02")
		expStr = &s
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO driver_documents (id, driver_id, doc_type, file_url, expiry_date, status, created_at)
		VALUES ($1, $2, $3, $4, $5, 'pending_review', $6)
	`, docID, driverID, docType, fileURL, expStr, now)
	if err != nil {
		return DriverDocument{}, fmt.Errorf("persist driver document: %w", err)
	}

	s.logAudit(ctx, nil, "upload_driver_doc", "driver_documents", docID, nil, &docType)

	return DriverDocument{
		ID:         docID,
		DriverID:   driverID,
		DocType:    docType,
		FileURL:    fileURL,
		ExpiryDate: expiryDate,
		Status:     "pending_review",
		CreatedAt:  now,
	}, nil
}

// UploadVehicleDoc stores a vehicle document in the vault.
func (s *DocumentService) UploadVehicleDoc(ctx context.Context, vehicleID, docType string, header *multipart.FileHeader, expiryDate *time.Time) (VehicleDocument, error) {
	if !validVehicleDocTypes[docType] {
		return VehicleDocument{}, fmt.Errorf("%w: %s", ErrInvalidDocType, docType)
	}
	if header == nil {
		return VehicleDocument{}, ErrFileRequired
	}

	var uploadableType string
	switch docType {
	case "rc":
		uploadableType = "vehicle_rc"
	case "insurance":
		uploadableType = "vehicle_insurance"
	case "puc":
		uploadableType = "vehicle_puc"
	case "fitness":
		uploadableType = "vehicle_fitness"
	case "permit":
		uploadableType = "vehicle_permit"
	default:
		uploadableType = "vehicle_rc"
	}

	fileURL := ""
	if s.files != nil {
		uploaded, err := s.files.UploadFile(ctx, header, uploadableType, vehicleID)
		if err != nil {
			return VehicleDocument{}, fmt.Errorf("upload file: %w", err)
		}
		fileURL = "/uploads/" + uploaded.Path
	} else {
		fileURL = "/uploads/vehicles/" + header.Filename
	}

	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return VehicleDocument{}, fmt.Errorf("database unavailable")
	}
	db := getter.DB()

	docID := "vdoc-" + uuid.New().String()
	now := time.Now().UTC()

	var expStr *string
	if expiryDate != nil {
		s := expiryDate.Format("2006-01-02")
		expStr = &s
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO vehicle_documents (id, vehicle_id, doc_type, file_url, expiry_date, status, created_at)
		VALUES ($1, $2, $3, $4, $5, 'pending_review', $6)
	`, docID, vehicleID, docType, fileURL, expStr, now)
	if err != nil {
		return VehicleDocument{}, fmt.Errorf("persist vehicle document: %w", err)
	}

	s.logAudit(ctx, nil, "upload_vehicle_doc", "vehicle_documents", docID, nil, &docType)

	return VehicleDocument{
		ID:         docID,
		VehicleID:  vehicleID,
		DocType:    docType,
		FileURL:    fileURL,
		ExpiryDate: expiryDate,
		Status:     "pending_review",
		CreatedAt:  now,
	}, nil
}

// ListDriverDocs retrieves all documents for a driver.
func (s *DocumentService) ListDriverDocs(ctx context.Context, driverID string) ([]DriverDocument, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	db := getter.DB()

	rows, err := db.QueryContext(ctx, `
		SELECT id, driver_id, doc_type, file_url, expiry_date, status, verified_by, verified_at, created_at
		FROM driver_documents
		WHERE driver_id = $1
		ORDER BY created_at DESC
	`, driverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DriverDocument
	for rows.Next() {
		var d DriverDocument
		var exp, vBy, vAt, created sql.NullString
		if err := rows.Scan(&d.ID, &d.DriverID, &d.DocType, &d.FileURL, &exp, &d.Status, &vBy, &vAt, &created); err == nil {
			if exp.Valid {
				if t, ok := parseDBTime(exp.String); ok {
					d.ExpiryDate = &t
				}
			}
			if vBy.Valid {
				d.VerifiedBy = &vBy.String
			}
			if vAt.Valid {
				if t, ok := parseDBTime(vAt.String); ok {
					d.VerifiedAt = &t
				}
			}
			if created.Valid {
				if t, ok := parseDBTime(created.String); ok {
					d.CreatedAt = t
				}
			}
			docs = append(docs, d)
		}
	}
	return docs, nil
}

// ListVehicleDocs retrieves all documents for a vehicle.
func (s *DocumentService) ListVehicleDocs(ctx context.Context, vehicleID string) ([]VehicleDocument, error) {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	db := getter.DB()

	rows, err := db.QueryContext(ctx, `
		SELECT id, vehicle_id, doc_type, file_url, expiry_date, status, verified_by, verified_at, created_at
		FROM vehicle_documents
		WHERE vehicle_id = $1
		ORDER BY created_at DESC
	`, vehicleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []VehicleDocument
	for rows.Next() {
		var d VehicleDocument
		var exp, vBy, vAt, created sql.NullString
		if err := rows.Scan(&d.ID, &d.VehicleID, &d.DocType, &d.FileURL, &exp, &d.Status, &vBy, &vAt, &created); err == nil {
			if exp.Valid {
				if t, ok := parseDBTime(exp.String); ok {
					d.ExpiryDate = &t
				}
			}
			if vBy.Valid {
				d.VerifiedBy = &vBy.String
			}
			if vAt.Valid {
				if t, ok := parseDBTime(vAt.String); ok {
					d.VerifiedAt = &t
				}
			}
			if created.Valid {
				if t, ok := parseDBTime(created.String); ok {
					d.CreatedAt = t
				}
			}
			docs = append(docs, d)
		}
	}
	return docs, nil
}

// VerifyDocument approves and marks a document as verified.
func (s *DocumentService) VerifyDocument(ctx context.Context, entityType, entityID, documentID, verifiedBy string) error {
	getter, ok := s.store.(repository.DBGetter)
	if !ok || getter.DB() == nil {
		return fmt.Errorf("database unavailable")
	}
	db := getter.DB()

	table := "driver_documents"
	if entityType == "vehicle" {
		table = "vehicle_documents"
	}

	query := fmt.Sprintf(`
		UPDATE %s
		SET status = 'verified', verified_by = $1, verified_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`, table)

	res, err := db.ExecContext(ctx, query, verifiedBy, documentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDocumentNotFound
	}

	info := fmt.Sprintf("verified_by=%s", verifiedBy)
	s.logAudit(ctx, nil, "document_verified", table, documentID, nil, &info)
	return nil
}
