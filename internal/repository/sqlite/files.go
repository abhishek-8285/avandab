package sqlite

import (
	"context"

	"transport-app/internal/domain"

	db "transport-app/db/generated/sqlite"
)

// FileRepository implementation

func (r *SQLRepository) CreateFile(ctx context.Context, file domain.File) (domain.File, error) {
	created, err := r.Q(ctx).CreateFile(ctx, db.CreateFileParams{
		ID:             string(file.ID),
		Filename:       file.Filename,
		OriginalName:   file.OriginalName,
		Path:           file.Path,
		Size:           file.Size,
		MimeType:       file.MimeType,
		UploadableType: file.UploadableType,
		UploadableID:   nullString(file.UploadableID),
		TenantID:       tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.File{}, err
	}
	return toDomainFile(db.File{
		ID:             created.ID,
		Filename:       created.Filename,
		OriginalName:   created.OriginalName,
		Path:           created.Path,
		Size:           created.Size,
		MimeType:       created.MimeType,
		UploadableType: created.UploadableType,
		UploadableID:   created.UploadableID,
		CreatedAt:      created.CreatedAt,
	}), nil
}

func (r *SQLRepository) GetFileByID(ctx context.Context, id domain.FileID) (domain.File, error) {
	f, err := r.Q(ctx).GetFileByID(ctx, db.GetFileByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.File{}, err
	}
	return toDomainFile(db.File{
		ID:             f.ID,
		Filename:       f.Filename,
		OriginalName:   f.OriginalName,
		Path:           f.Path,
		Size:           f.Size,
		MimeType:       f.MimeType,
		UploadableType: f.UploadableType,
		UploadableID:   f.UploadableID,
		CreatedAt:      f.CreatedAt,
	}), nil
}

func (r *SQLRepository) GetFilesByUploadable(ctx context.Context, uploadableType string, uploadableID string) ([]domain.File, error) {
	rows, err := r.Q(ctx).GetFilesByUploadable(ctx, db.GetFilesByUploadableParams{
		UploadableType: uploadableType,
		UploadableID:   nullString(&uploadableID),
		TenantID:       tenantIDFromCtx(ctx),
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.File, len(rows))
	for i, f := range rows {
		result[i] = toDomainFile(db.File{
			ID:             f.ID,
			Filename:       f.Filename,
			OriginalName:   f.OriginalName,
			Path:           f.Path,
			Size:           f.Size,
			MimeType:       f.MimeType,
			UploadableType: f.UploadableType,
			UploadableID:   f.UploadableID,
			CreatedAt:      f.CreatedAt,
		})
	}
	return result, nil
}

func (r *SQLRepository) DeleteFile(ctx context.Context, id domain.FileID) error {
	return r.Q(ctx).DeleteFile(ctx, db.DeleteFileParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) DeleteFilesByUploadable(ctx context.Context, uploadableType string, uploadableID string) error {
	return r.Q(ctx).DeleteFilesByUploadable(ctx, db.DeleteFilesByUploadableParams{
		UploadableType: uploadableType,
		UploadableID:   nullString(&uploadableID),
		TenantID:       tenantIDFromCtx(ctx),
	})
}
