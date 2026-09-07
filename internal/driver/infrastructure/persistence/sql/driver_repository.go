package sql

import (
	"context"
	"database/sql"
	"errors"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/driver/domain"
	"transport-app/internal/driver/domain/aggregate"
	"transport-app/internal/driver/infrastructure/persistence/sql/converters"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/shared/outbox"
)

type driverRepository struct {
	dbConn *sql.DB
	q      *db.Queries
	outbox *outbox.OutboxWriter
}

// NewDriverRepository creates a SQLite-backed implementation of DriverRepository.
func NewDriverRepository(dbConn *sql.DB) domain.DriverRepository {
	return &driverRepository{
		dbConn: dbConn,
		q:      db.New(dbConn),
		outbox: outbox.NewOutboxWriter(dbConn),
	}
}

func (r *driverRepository) Q(ctx context.Context) *db.Queries {
	if tx := repository.TxFromContext(ctx); tx != nil {
		return r.q.WithTx(tx)
	}
	return r.q
}

func (r *driverRepository) Save(ctx context.Context, d *aggregate.DriverAggregate) error {
	var email, address, emergencyContactName, emergencyContactPhone, notes sql.NullString
	if d.Email != nil {
		email = sql.NullString{String: *d.Email, Valid: true}
	}
	if d.Address != nil {
		address = sql.NullString{String: *d.Address, Valid: true}
	}
	if d.EmergencyContactName != nil {
		emergencyContactName = sql.NullString{String: *d.EmergencyContactName, Valid: true}
	}
	if d.EmergencyContactPhone != nil {
		emergencyContactPhone = sql.NullString{String: *d.EmergencyContactPhone, Valid: true}
	}
	if d.Notes != nil {
		notes = sql.NullString{String: *d.Notes, Valid: true}
	}

	_, err := r.Q(ctx).GetDriverByID(ctx, db.GetDriverByIDParams{
		ID:       string(d.ID),
		TenantID: string(d.TenantID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			_, err = r.Q(ctx).CreateDriver(ctx, db.CreateDriverParams{
				ID:                    string(d.ID),
				DriverID:              d.DriverDisplayID,
				FirstName:             d.FirstName,
				LastName:              d.LastName,
				Phone:                 d.Phone,
				Email:                 email,
				Address:               address,
				LicenseNumber:         d.LicenseNumber,
				LicenseExpiry:         d.LicenseExpiry,
				ExperienceYears:       d.ExperienceYears,
				Status:                string(d.Status),
				EmergencyContactName:  emergencyContactName,
				EmergencyContactPhone: emergencyContactPhone,
				Notes:                 notes,
				TenantID:              string(d.TenantID),
			})
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		_, err = r.Q(ctx).UpdateDriver(ctx, db.UpdateDriverParams{
			DriverID:              d.DriverDisplayID,
			FirstName:             d.FirstName,
			LastName:              d.LastName,
			Phone:                 d.Phone,
			Email:                 email,
			Address:               address,
			LicenseNumber:         d.LicenseNumber,
			LicenseExpiry:         d.LicenseExpiry,
			ExperienceYears:       d.ExperienceYears,
			Status:                string(d.Status),
			EmergencyContactName:  emergencyContactName,
			EmergencyContactPhone: emergencyContactPhone,
			Notes:                 notes,
			ID:                    string(d.ID),
			TenantID:              string(d.TenantID),
		})
		if err != nil {
			return err
		}
	}

	err = r.outbox.SaveEvents(ctx, string(d.ID), "Driver", d.Events())
	if err != nil {
		return err
	}
	d.ClearEvents()
	return nil
}

func (r *driverRepository) Find(ctx context.Context, id aggregate.DriverID, tenantID shared.TenantID) (*aggregate.DriverAggregate, error) {
	row, err := r.Q(ctx).GetDriverByID(ctx, db.GetDriverByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		return nil, err
	}
	d := db.Driver{
		ID:                    row.ID,
		DriverID:              row.DriverID,
		FirstName:             row.FirstName,
		LastName:              row.LastName,
		Phone:                 row.Phone,
		Email:                 row.Email,
		Address:               row.Address,
		LicenseNumber:         row.LicenseNumber,
		LicenseExpiry:         row.LicenseExpiry,
		ExperienceYears:       row.ExperienceYears,
		Status:                row.Status,
		EmergencyContactName:  row.EmergencyContactName,
		EmergencyContactPhone: row.EmergencyContactPhone,
		Notes:                 row.Notes,
		TenantID:              row.TenantID,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	return converters.ToDomain(d), nil
}

func (r *driverRepository) GetReadModel(ctx context.Context, id aggregate.DriverID, tenantID shared.TenantID) (domain.DriverReadModel, error) {
	row, err := r.Q(ctx).GetDriverByID(ctx, db.GetDriverByIDParams{
		ID:       string(id),
		TenantID: string(tenantID),
	})
	if err != nil {
		return domain.DriverReadModel{}, err
	}
	d := db.Driver{
		ID:                    row.ID,
		DriverID:              row.DriverID,
		FirstName:             row.FirstName,
		LastName:              row.LastName,
		Phone:                 row.Phone,
		Email:                 row.Email,
		Address:               row.Address,
		LicenseNumber:         row.LicenseNumber,
		LicenseExpiry:         row.LicenseExpiry,
		ExperienceYears:       row.ExperienceYears,
		Status:                row.Status,
		EmergencyContactName:  row.EmergencyContactName,
		EmergencyContactPhone: row.EmergencyContactPhone,
		Notes:                 row.Notes,
		TenantID:              row.TenantID,
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	return converters.ToReadModel(d), nil
}

func (r *driverRepository) SearchReadModels(ctx context.Context, tenantID shared.TenantID, query string, status string, limit int, offset int) ([]domain.DriverReadModel, int64, error) {
	rows, err := r.Q(ctx).SearchDrivers(ctx, db.SearchDriversParams{
		TenantID:  string(tenantID),
		Search:    query,
		StatusAll: status,
		Status:    status,
		Limit:     int64(limit),
		Offset:    int64(offset),
	})
	if err != nil {
		return nil, 0, err
	}

	total, err := r.Q(ctx).CountDrivers(ctx, db.CountDriversParams{
		TenantID:  string(tenantID),
		Search:    query,
		StatusAll: status,
		Status:    status,
	})
	if err != nil {
		return nil, 0, err
	}

	readModels := make([]domain.DriverReadModel, len(rows))
	for i, row := range rows {
		d := db.Driver{
			ID:                    row.ID,
			DriverID:              row.DriverID,
			FirstName:             row.FirstName,
			LastName:              row.LastName,
			Phone:                 row.Phone,
			Email:                 row.Email,
			Address:               row.Address,
			LicenseNumber:         row.LicenseNumber,
			LicenseExpiry:         row.LicenseExpiry,
			ExperienceYears:       row.ExperienceYears,
			Status:                row.Status,
			EmergencyContactName:  row.EmergencyContactName,
			EmergencyContactPhone: row.EmergencyContactPhone,
			Notes:                 row.Notes,
			TenantID:              row.TenantID,
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		}
		readModels[i] = converters.ToReadModel(d)
	}

	return readModels, total, nil
}
