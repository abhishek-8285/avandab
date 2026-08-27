package sqlite

import (
	"context"
	"database/sql"

	"transport-app/internal/domain"

	db "transport-app/db/generated/sqlite"
)

// DriverRepository implementation

func (r *SQLRepository) CreateDriver(ctx context.Context, driver domain.Driver) (domain.Driver, error) {
	created, err := r.Q(ctx).CreateDriver(ctx, db.CreateDriverParams{
		ID:                    string(driver.ID),
		DriverID:              driver.DriverID,
		FirstName:             driver.FirstName,
		LastName:              driver.LastName,
		Phone:                 driver.Phone,
		Email:                 nullString(driver.Email),
		Address:               nullString(driver.Address),
		LicenseNumber:         driver.LicenseNumber,
		LicenseExpiry:         driver.LicenseExpiry,
		ExperienceYears:       driver.ExperienceYears,
		Status:                string(driver.Status),
		EmergencyContactName:  nullString(driver.EmergencyContactName),
		EmergencyContactPhone: nullString(driver.EmergencyContactPhone),
		Notes:                 nullString(driver.Notes),
		TenantID:              tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Driver{}, err
	}
	d := db.Driver{
		ID:                    created.ID,
		DriverID:              created.DriverID,
		FirstName:             created.FirstName,
		LastName:              created.LastName,
		Phone:                 created.Phone,
		Email:                 created.Email,
		Address:               created.Address,
		LicenseNumber:         created.LicenseNumber,
		LicenseExpiry:         created.LicenseExpiry,
		ExperienceYears:       created.ExperienceYears,
		Status:                created.Status,
		EmergencyContactName:  created.EmergencyContactName,
		EmergencyContactPhone: created.EmergencyContactPhone,
		Notes:                 created.Notes,
		CreatedAt:             created.CreatedAt,
		UpdatedAt:             created.UpdatedAt,
	}
	if driver.Aadhaar != nil || driver.PAN != nil || driver.BankDetails != nil {
		_, _ = r.exec(ctx, `UPDATE drivers SET aadhaar = ?, pan = ?, bank_details = ? WHERE id = ?`,
			driver.Aadhaar, driver.PAN, driver.BankDetails, string(driver.ID))
	}
	dom := toDomainDriver(d)
	dom.Aadhaar = driver.Aadhaar
	dom.PAN = driver.PAN
	dom.BankDetails = driver.BankDetails
	return dom, nil
}

func (r *SQLRepository) GetDriverByID(ctx context.Context, id domain.DriverID) (domain.Driver, error) {
	row, err := r.Q(ctx).GetDriverByID(ctx, db.GetDriverByIDParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Driver{}, err
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
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	dom := toDomainDriver(d)
	var aadhaar, pan, bank sql.NullString
	_ = r.queryRow(ctx, `SELECT aadhaar, pan, bank_details FROM drivers WHERE id = ?`, string(id)).Scan(&aadhaar, &pan, &bank)
	if aadhaar.Valid {
		dom.Aadhaar = &aadhaar.String
	}
	if pan.Valid {
		dom.PAN = &pan.String
	}
	if bank.Valid {
		dom.BankDetails = &bank.String
	}
	return dom, nil
}

func (r *SQLRepository) GetDriverByDriverID(ctx context.Context, driverID string) (domain.Driver, error) {
	row, err := r.Q(ctx).GetDriverByDriverID(ctx, db.GetDriverByDriverIDParams{
		DriverID: driverID,
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Driver{}, err
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
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	dom := toDomainDriver(d)
	var aadhaar, pan, bank sql.NullString
	_ = r.queryRow(ctx, `SELECT aadhaar, pan, bank_details FROM drivers WHERE id = ?`, row.ID).Scan(&aadhaar, &pan, &bank)
	if aadhaar.Valid {
		dom.Aadhaar = &aadhaar.String
	}
	if pan.Valid {
		dom.PAN = &pan.String
	}
	if bank.Valid {
		dom.BankDetails = &bank.String
	}
	return dom, nil
}

func (r *SQLRepository) GetDriverByPhone(ctx context.Context, phone string) (domain.Driver, error) {
	row, err := r.Q(ctx).GetDriverByPhone(ctx, db.GetDriverByPhoneParams{
		Phone:    phone,
		TenantID: tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Driver{}, err
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
		CreatedAt:             row.CreatedAt,
		UpdatedAt:             row.UpdatedAt,
	}
	return toDomainDriver(d), nil
}

func (r *SQLRepository) UpdateDriver(ctx context.Context, driver domain.Driver) (domain.Driver, error) {
	updated, err := r.Q(ctx).UpdateDriver(ctx, db.UpdateDriverParams{
		DriverID:              driver.DriverID,
		FirstName:             driver.FirstName,
		LastName:              driver.LastName,
		Phone:                 driver.Phone,
		Email:                 nullString(driver.Email),
		Address:               nullString(driver.Address),
		LicenseNumber:         driver.LicenseNumber,
		LicenseExpiry:         driver.LicenseExpiry,
		ExperienceYears:       driver.ExperienceYears,
		Status:                string(driver.Status),
		EmergencyContactName:  nullString(driver.EmergencyContactName),
		EmergencyContactPhone: nullString(driver.EmergencyContactPhone),
		Notes:                 nullString(driver.Notes),
		ID:                    string(driver.ID),
		TenantID:              tenantIDFromCtx(ctx),
	})
	if err != nil {
		return domain.Driver{}, err
	}
	d := db.Driver{
		ID:                    updated.ID,
		DriverID:              updated.DriverID,
		FirstName:             updated.FirstName,
		LastName:              updated.LastName,
		Phone:                 updated.Phone,
		Email:                 updated.Email,
		Address:               updated.Address,
		LicenseNumber:         updated.LicenseNumber,
		LicenseExpiry:         updated.LicenseExpiry,
		ExperienceYears:       updated.ExperienceYears,
		Status:                updated.Status,
		EmergencyContactName:  updated.EmergencyContactName,
		EmergencyContactPhone: updated.EmergencyContactPhone,
		Notes:                 updated.Notes,
		CreatedAt:             updated.CreatedAt,
		UpdatedAt:             updated.UpdatedAt,
	}
	if driver.Aadhaar != nil || driver.PAN != nil || driver.BankDetails != nil {
		_, _ = r.exec(ctx, `UPDATE drivers SET aadhaar = ?, pan = ?, bank_details = ? WHERE id = ?`,
			driver.Aadhaar, driver.PAN, driver.BankDetails, string(driver.ID))
	}
	dom := toDomainDriver(d)
	dom.Aadhaar = driver.Aadhaar
	dom.PAN = driver.PAN
	dom.BankDetails = driver.BankDetails
	return dom, nil
}

func (r *SQLRepository) DeleteDriver(ctx context.Context, id domain.DriverID) error {
	return r.Q(ctx).DeleteDriver(ctx, db.DeleteDriverParams{
		ID:       string(id),
		TenantID: tenantIDFromCtx(ctx),
	})
}

func (r *SQLRepository) SearchDrivers(ctx context.Context, query string, status string, limit, offset int) ([]domain.Driver, error) {
	rows, err := r.Q(ctx).SearchDrivers(ctx, db.SearchDriversParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  sql.NullString{String: query, Valid: true},
		Column6:  status,
		Status:   status,
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Driver, len(rows))
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
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		}
		result[i] = toDomainDriver(d)
	}
	return result, nil
}

func (r *SQLRepository) CountDrivers(ctx context.Context, query string, status string) (int64, error) {
	count, err := r.Q(ctx).CountDrivers(ctx, db.CountDriversParams{
		TenantID: tenantIDFromCtx(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  sql.NullString{String: query, Valid: true},
		Column6:  status,
		Status:   status,
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (r *SQLRepository) GetAvailableDrivers(ctx context.Context) ([]domain.Driver, error) {
	rows, err := r.Q(ctx).GetAvailableDrivers(ctx, tenantIDFromCtx(ctx))
	if err != nil {
		return nil, err
	}
	result := make([]domain.Driver, len(rows))
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
			CreatedAt:             row.CreatedAt,
			UpdatedAt:             row.UpdatedAt,
		}
		result[i] = toDomainDriver(d)
	}
	return result, nil
}
