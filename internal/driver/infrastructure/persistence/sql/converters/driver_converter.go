package converters

import (
	"database/sql"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/driver/domain"
	"transport-app/internal/driver/domain/aggregate"
	"transport-app/internal/shared"
)

// ToDomain converts db.Driver to *aggregate.DriverAggregate.
func ToDomain(d db.Driver) *aggregate.DriverAggregate {
	return aggregate.NewDriverAggregate(
		aggregate.DriverID(d.ID),
		shared.TenantID(d.TenantID),
		d.DriverID,
		d.FirstName,
		d.LastName,
		d.Phone,
		getStringPointer(d.Email),
		getStringPointer(d.Address),
		d.LicenseNumber.String,
		d.LicenseExpiry.Time,
		d.ExperienceYears,
		aggregate.DriverStatus(d.Status),
		getStringPointer(d.EmergencyContactName),
		getStringPointer(d.EmergencyContactPhone),
		getStringPointer(d.Notes),
		d.CreatedAt,
	)
}

// ToReadModel converts db.Driver to domain.DriverReadModel.
func ToReadModel(d db.Driver) domain.DriverReadModel {
	return domain.DriverReadModel{
		ID:                    d.ID,
		DriverDisplayID:       d.DriverID,
		FirstName:             d.FirstName,
		LastName:              d.LastName,
		Phone:                 d.Phone,
		Email:                 getStringPointer(d.Email),
		Address:               getStringPointer(d.Address),
		LicenseNumber:         d.LicenseNumber.String,
		LicenseExpiry:         d.LicenseExpiry.Time,
		ExperienceYears:       d.ExperienceYears,
		Status:                d.Status,
		EmergencyContactName:  getStringPointer(d.EmergencyContactName),
		EmergencyContactPhone: getStringPointer(d.EmergencyContactPhone),
		Notes:                 getStringPointer(d.Notes),
		CreatedAt:             d.CreatedAt,
		UpdatedAt:             d.UpdatedAt,
	}
}

func getStringPointer(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}
