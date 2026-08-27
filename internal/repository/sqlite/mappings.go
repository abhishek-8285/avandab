package sqlite

import (
	"context"
	"database/sql"
	"time"

	db "transport-app/db/generated/sqlite"
	"transport-app/internal/domain"
	"transport-app/internal/repository"
	"transport-app/internal/shared"
)

// Helpers to convert between sql.Null* types and Go pointers.

func PtrString(s string) *string {
	return &s
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

func fromNullString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func PtrFloat(f float64) *float64 {
	return &f
}

func nullFloat(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{Valid: false}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

func fromNullFloat(nf sql.NullFloat64) *float64 {
	if !nf.Valid {
		return nil
	}
	return &nf.Float64
}

func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func fromNullTime(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}

	return &nt.Time
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func int64ToBool(i int64) bool {
	return i != 0
}

func tenantIDFromCtx(ctx context.Context) string {
	if t := shared.TenantIDFromContext(ctx); t != "" {
		return string(t)
	}
	return string(shared.DefaultTenant)
}

func FromNullInt64(ni sql.NullInt64) *int64 {
	if !ni.Valid {
		return nil
	}
	return &ni.Int64
}

func NullStringVal(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

// Entity mapping functions: sqlc model -> domain model

func toDomainRole(r db.Role) domain.Role {
	return domain.Role{
		ID:          r.ID,
		Name:        domain.RoleName(r.Name),
		Description: fromNullString(r.Description),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func toCreateUserRowWithRole(u db.CreateUserRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toGetUserByIDRowWithRole(u db.GetUserByIDRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toGetUserByEmailRowWithRole(u db.GetUserByEmailRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toUpdateUserRowWithRole(u db.UpdateUserRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toUpdateUserThemePreferenceRowWithRole(u db.UpdateUserThemePreferenceRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toUpdateUserPasswordRowWithRole(u db.UpdateUserPasswordRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toUpdateUserLastLoginRowWithRole(u db.UpdateUserLastLoginRow, role domain.Role) domain.User {
	return domain.User{
		ID:              domain.UserID(u.ID),
		Email:           u.Email,
		TenantID:        u.TenantID,
		PasswordHash:    u.PasswordHash,
		Name:            u.Name,
		Phone:           fromNullString(u.Phone),
		Timezone:        "Asia/Kolkata",
		ThemePreference: u.ThemePreference,
		Role:            role,
		Status:          domain.UserStatus(u.Status),
		LastLoginAt:     fromNullTime(u.LastLoginAt),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

func toDomainSession(s db.Session) domain.Session {
	return domain.Session{
		ID:        domain.SessionID(s.ID),
		UserID:    domain.UserID(s.UserID),
		TokenHash: s.TokenHash,
		ExpiresAt: s.ExpiresAt,
		UserAgent: fromNullString(s.UserAgent),
		IPAddress: fromNullString(s.IpAddress),
		CreatedAt: s.CreatedAt,
	}
}

func toDomainDriver(d db.Driver) domain.Driver {
	return domain.Driver{
		ID:                    domain.DriverID(d.ID),
		DriverID:              d.DriverID,
		FirstName:             d.FirstName,
		LastName:              d.LastName,
		Phone:                 d.Phone,
		Email:                 fromNullString(d.Email),
		Address:               fromNullString(d.Address),
		LicenseNumber:         d.LicenseNumber,
		LicenseExpiry:         d.LicenseExpiry,
		ExperienceYears:       d.ExperienceYears,
		Status:                domain.DriverStatus(d.Status),
		EmergencyContactName:  fromNullString(d.EmergencyContactName),
		EmergencyContactPhone: fromNullString(d.EmergencyContactPhone),
		Notes:                 fromNullString(d.Notes),
		CreatedAt:             d.CreatedAt,
		UpdatedAt:             d.UpdatedAt,
	}
}

func toDomainVehicle(v db.Vehicle) domain.Vehicle {
	return domain.Vehicle{
		ID:                 domain.VehicleID(v.ID),
		RegistrationNumber: v.RegistrationNumber,
		VehicleNumber:      v.VehicleNumber,
		VehicleType:        domain.VehicleType(v.VehicleType),
		Capacity:           v.Capacity,
		FuelType:           domain.FuelType(v.FuelType),
		InsuranceExpiry:    v.InsuranceExpiry,
		FitnessExpiry:      v.FitnessExpiry,
		PermitExpiry:       v.PermitExpiry,
		Status:             domain.VehicleStatus(v.Status),
		CurrentMileage:     fromNullFloat(v.CurrentMileage),
		CreatedAt:          v.CreatedAt,
		UpdatedAt:          v.UpdatedAt,
	}
}

func toDomainCustomer(c db.Customer) domain.Customer {
	return domain.Customer{
		ID:               domain.CustomerID(c.ID),
		CustomerCode:     c.CustomerCode.String,
		Title:            fromNullString(c.Title),
		Name:             c.Name,
		Company:          fromNullString(c.Company),
		ContactPerson:    fromNullString(c.ContactPerson),
		Phone:            c.Phone,
		Email:            fromNullString(c.Email),
		GST:              fromNullString(c.Gst),
		Address:          fromNullString(c.Address),
		BillingAddress:   fromNullString(c.BillingAddress),
		InternalID:       fromNullString(c.InternalID),
		PhotoURL:         fromNullString(c.PhotoUrl),
		PlaceUUID:        fromNullString(c.PlaceUuid),
		Meta:             c.Meta,
		Type:             c.Type,
		Status:           c.Status,
		PaymentTermsDays: int(c.PaymentTermsDays),
		TenantID:         c.TenantID,
		StateCode:        fromNullString(c.StateCode),
		Notes:            fromNullString(c.Notes),
		CreatedAt:        c.CreatedAt,
		UpdatedAt:        c.UpdatedAt,
	}
}

func toDomainCustomerFromCreateRow(r db.CreateCustomerRow) domain.Customer {
	return domain.Customer{
		ID:               domain.CustomerID(r.ID),
		CustomerCode:     r.CustomerCode.String,
		Title:            fromNullString(r.Title),
		Name:             r.Name,
		Company:          fromNullString(r.Company),
		ContactPerson:    fromNullString(r.ContactPerson),
		Phone:            r.Phone,
		Email:            fromNullString(r.Email),
		GST:              fromNullString(r.Gst),
		Address:          fromNullString(r.Address),
		BillingAddress:   fromNullString(r.BillingAddress),
		InternalID:       fromNullString(r.InternalID),
		PhotoURL:         fromNullString(r.PhotoUrl),
		PlaceUUID:        fromNullString(r.PlaceUuid),
		Meta:             r.Meta,
		Type:             r.Type,
		Status:           r.Status,
		PaymentTermsDays: int(r.PaymentTermsDays),
		TenantID:         r.TenantID,
		StateCode:        fromNullString(r.StateCode),
		Notes:            fromNullString(r.Notes),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func toDomainCustomerFromGetRow(r db.GetCustomerByIDRow) domain.Customer {
	return domain.Customer{
		ID:               domain.CustomerID(r.ID),
		CustomerCode:     r.CustomerCode.String,
		Title:            fromNullString(r.Title),
		Name:             r.Name,
		Company:          fromNullString(r.Company),
		ContactPerson:    fromNullString(r.ContactPerson),
		Phone:            r.Phone,
		Email:            fromNullString(r.Email),
		GST:              fromNullString(r.Gst),
		Address:          fromNullString(r.Address),
		BillingAddress:   fromNullString(r.BillingAddress),
		InternalID:       fromNullString(r.InternalID),
		PhotoURL:         fromNullString(r.PhotoUrl),
		PlaceUUID:        fromNullString(r.PlaceUuid),
		Meta:             r.Meta,
		Type:             r.Type,
		Status:           r.Status,
		PaymentTermsDays: int(r.PaymentTermsDays),
		TenantID:         r.TenantID,
		StateCode:        fromNullString(r.StateCode),
		Notes:            fromNullString(r.Notes),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func toDomainCustomerFromPhoneRow(r db.GetCustomerByPhoneRow) domain.Customer {
	return domain.Customer{
		ID:               domain.CustomerID(r.ID),
		CustomerCode:     r.CustomerCode.String,
		Title:            fromNullString(r.Title),
		Name:             r.Name,
		Company:          fromNullString(r.Company),
		ContactPerson:    fromNullString(r.ContactPerson),
		Phone:            r.Phone,
		Email:            fromNullString(r.Email),
		GST:              fromNullString(r.Gst),
		Address:          fromNullString(r.Address),
		BillingAddress:   fromNullString(r.BillingAddress),
		InternalID:       fromNullString(r.InternalID),
		PhotoURL:         fromNullString(r.PhotoUrl),
		PlaceUUID:        fromNullString(r.PlaceUuid),
		Meta:             r.Meta,
		Type:             r.Type,
		Status:           r.Status,
		PaymentTermsDays: int(r.PaymentTermsDays),
		TenantID:         r.TenantID,
		StateCode:        fromNullString(r.StateCode),
		Notes:            fromNullString(r.Notes),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func toDomainCustomerFromUpdateRow(r db.UpdateCustomerRow) domain.Customer {
	return domain.Customer{
		ID:               domain.CustomerID(r.ID),
		CustomerCode:     r.CustomerCode.String,
		Title:            fromNullString(r.Title),
		Name:             r.Name,
		Company:          fromNullString(r.Company),
		ContactPerson:    fromNullString(r.ContactPerson),
		Phone:            r.Phone,
		Email:            fromNullString(r.Email),
		GST:              fromNullString(r.Gst),
		Address:          fromNullString(r.Address),
		BillingAddress:   fromNullString(r.BillingAddress),
		InternalID:       fromNullString(r.InternalID),
		PhotoURL:         fromNullString(r.PhotoUrl),
		PlaceUUID:        fromNullString(r.PlaceUuid),
		Meta:             r.Meta,
		Type:             r.Type,
		Status:           r.Status,
		PaymentTermsDays: int(r.PaymentTermsDays),
		TenantID:         r.TenantID,
		StateCode:        fromNullString(r.StateCode),
		Notes:            fromNullString(r.Notes),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func toDomainCustomerFromSearchRow(r db.SearchCustomersRow) domain.Customer {
	return domain.Customer{
		ID:               domain.CustomerID(r.ID),
		CustomerCode:     r.CustomerCode.String,
		Title:            fromNullString(r.Title),
		Name:             r.Name,
		Company:          fromNullString(r.Company),
		ContactPerson:    fromNullString(r.ContactPerson),
		Phone:            r.Phone,
		Email:            fromNullString(r.Email),
		GST:              fromNullString(r.Gst),
		Address:          fromNullString(r.Address),
		BillingAddress:   fromNullString(r.BillingAddress),
		InternalID:       fromNullString(r.InternalID),
		PhotoURL:         fromNullString(r.PhotoUrl),
		PlaceUUID:        fromNullString(r.PlaceUuid),
		Meta:             r.Meta,
		Type:             r.Type,
		Status:           r.Status,
		PaymentTermsDays: int(r.PaymentTermsDays),
		TenantID:         r.TenantID,
		StateCode:        fromNullString(r.StateCode),
		Notes:            fromNullString(r.Notes),
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

// sqlc v1.31+ generates a distinct *Row type per query; these helpers normalise them.

// routeFields holds the common columns every route query returns.
type routeFields struct {
	ID                  string
	TenantID            string
	Source              string
	Destination         string
	SourceNormalized    string
	DestNormalized      string
	Distance            float64
	EstimatedHours      float64
	StandardFare        float64
	ReverseDistance     sql.NullFloat64
	ReverseStandardFare sql.NullFloat64
	Direction           string
	IsActive            int64
	Remarks             sql.NullString
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func routeFieldsToDomain(f routeFields) domain.Route {
	return domain.Route{
		ID:                  domain.RouteID(f.ID),
		TenantID:            f.TenantID,
		Source:              f.Source,
		Destination:         f.Destination,
		SourceNormalized:    f.SourceNormalized,
		DestNormalized:      f.DestNormalized,
		Distance:            f.Distance,
		EstimatedHours:      f.EstimatedHours,
		StandardFare:        f.StandardFare,
		ReverseDistance:     fromNullFloat(f.ReverseDistance),
		ReverseStandardFare: fromNullFloat(f.ReverseStandardFare),
		Direction:           f.Direction,
		IsActive:            int64ToBool(f.IsActive),
		Remarks:             fromNullString(f.Remarks),
		CreatedAt:           f.CreatedAt,
		UpdatedAt:           f.UpdatedAt,
	}
}

// routeRowToDomain is a generic helper that works with any route query row.
func routeRowToDomain(r interface{}) domain.Route {
	switch v := r.(type) {
	case db.CreateRouteRow:
		return routeFieldsToDomain(routeFields{
			v.ID, v.TenantID, v.Source, v.Destination, v.SourceNormalized, v.DestNormalized,
			v.Distance, v.EstimatedHours, v.StandardFare, v.ReverseDistance, v.ReverseStandardFare,
			v.Direction, v.IsActive, v.Remarks, v.CreatedAt, v.UpdatedAt,
		})
	case db.GetRouteByIDRow:
		return routeFieldsToDomain(routeFields{
			v.ID, v.TenantID, v.Source, v.Destination, v.SourceNormalized, v.DestNormalized,
			v.Distance, v.EstimatedHours, v.StandardFare, v.ReverseDistance, v.ReverseStandardFare,
			v.Direction, v.IsActive, v.Remarks, v.CreatedAt, v.UpdatedAt,
		})
	case db.GetRouteBySourceAndDestinationRow:
		return routeFieldsToDomain(routeFields{
			v.ID, v.TenantID, v.Source, v.Destination, v.SourceNormalized, v.DestNormalized,
			v.Distance, v.EstimatedHours, v.StandardFare, v.ReverseDistance, v.ReverseStandardFare,
			v.Direction, v.IsActive, v.Remarks, v.CreatedAt, v.UpdatedAt,
		})
	case db.UpdateRouteRow:
		return routeFieldsToDomain(routeFields{
			v.ID, v.TenantID, v.Source, v.Destination, v.SourceNormalized, v.DestNormalized,
			v.Distance, v.EstimatedHours, v.StandardFare, v.ReverseDistance, v.ReverseStandardFare,
			v.Direction, v.IsActive, v.Remarks, v.CreatedAt, v.UpdatedAt,
		})
	case db.SearchRoutesRow:
		return routeFieldsToDomain(routeFields{
			v.ID, v.TenantID, v.Source, v.Destination, v.SourceNormalized, v.DestNormalized,
			v.Distance, v.EstimatedHours, v.StandardFare, v.ReverseDistance, v.ReverseStandardFare,
			v.Direction, v.IsActive, v.Remarks, v.CreatedAt, v.UpdatedAt,
		})
	}
	return domain.Route{}
}

func toDomainBooking(b db.Booking) domain.Booking {
	return domain.Booking{
		ID:            domain.BookingID(b.ID),
		BookingNumber: b.BookingNumber,
		CustomerID:    domain.CustomerID(b.CustomerID),
		PickupDate:    b.PickupDate,
		RouteID:       domain.RouteID(b.RouteID),
		VehicleType:   domain.VehicleType(b.VehicleType),
		Passengers:    b.Passengers,
		CargoWeight:   fromNullFloat(b.CargoWeight),
		Price:         b.Price,
		Notes:         fromNullString(b.Notes),
		Status:        domain.BookingStatus(b.Status),
		CreatedAt:     b.CreatedAt,
		UpdatedAt:     b.UpdatedAt,
	}
}

func toDomainTrip(t db.Trip) domain.Trip {
	var bookingID *domain.BookingID
	if t.BookingID.Valid {
		bid := domain.BookingID(t.BookingID.String)
		bookingID = &bid
	}

	var driverID *domain.DriverID
	if t.DriverID.Valid {
		did := domain.DriverID(t.DriverID.String)
		driverID = &did
	}

	var vehicleID *domain.VehicleID
	if t.VehicleID.Valid {
		vid := domain.VehicleID(t.VehicleID.String)
		vehicleID = &vid
	}

	return domain.Trip{
		ID:            domain.TripID(t.ID),
		TripNumber:    t.TripNumber,
		BookingID:     bookingID,
		DriverID:      driverID,
		VehicleID:     vehicleID,
		RouteID:       domain.RouteID(t.RouteID),
		DepartureTime: t.DepartureTime,
		ArrivalTime:   fromNullTime(t.ArrivalTime),
		Status:        domain.TripStatus(t.Status),
		Remarks:       fromNullString(t.Remarks),
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}

func toDomainInvoice(i db.Invoice) domain.Invoice {
	var tripID *domain.TripID
	if i.TripID.Valid {
		tid := domain.TripID(i.TripID.String)
		tripID = &tid
	}

	return domain.Invoice{
		ID:            domain.InvoiceID(i.ID),
		InvoiceNumber: i.InvoiceNumber,
		BookingID:     domain.BookingID(i.BookingID),
		CustomerID:    domain.CustomerID(i.CustomerID),
		TripID:        tripID,
		Subtotal:      i.Subtotal,
		Tax:           i.Tax,
		Discount:      i.Discount,
		Total:         i.Total,
		PaymentStatus: domain.PaymentStatus(i.PaymentStatus),
		CreatedAt:     i.CreatedAt,
		UpdatedAt:     i.UpdatedAt,
	}
}

func toDomainPayment(p db.Payment) domain.Payment {
	return domain.Payment{
		ID:          domain.PaymentID(p.ID),
		InvoiceID:   domain.InvoiceID(p.InvoiceID),
		PaymentDate: p.PaymentDate,
		Amount:      p.Amount,
		Method:      domain.PaymentMethod(p.Method),
		Reference:   fromNullString(p.Reference),
		Remarks:     fromNullString(p.Remarks),
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
}

func toDomainFile(f db.File) domain.File {
	return domain.File{
		ID:             domain.FileID(f.ID),
		Filename:       f.Filename,
		OriginalName:   f.OriginalName,
		Path:           f.Path,
		Size:           f.Size,
		MimeType:       f.MimeType,
		UploadableType: f.UploadableType,
		UploadableID:   fromNullString(f.UploadableID),
		CreatedAt:      f.CreatedAt,
	}
}

func toDomainCompanySetting(c db.CompanySetting) domain.CompanySettings {
	return domain.CompanySettings{
		ID:            c.ID,
		CompanyName:   c.CompanyName,
		LogoPath:      fromNullString(c.LogoPath),
		Currency:      c.Currency,
		Timezone:      c.Timezone,
		GSTEnabled:    c.GstEnabled,
		GSTRate:       c.GstRate,
		BookingPrefix: c.BookingPrefix,
		TripPrefix:    c.TripPrefix,
		InvoicePrefix: c.InvoicePrefix,
		Address:       fromNullString(c.Address),
		Phone:         fromNullString(c.Phone),
		Email:         fromNullString(c.Email),
		GSTNumber:     fromNullString(c.GstNumber),
		FinancialYear: fromNullString(c.FinancialYear),
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}
}

// Nullable ID conversion helpers

func NullStringToTripID(ns sql.NullString) *domain.TripID {
	if !ns.Valid {
		return nil
	}
	tid := domain.TripID(ns.String)
	return &tid
}

func NullStringToBookingID(ns sql.NullString) *domain.BookingID {
	if !ns.Valid {
		return nil
	}
	bid := domain.BookingID(ns.String)
	return &bid
}

func NullStringToDriverID(ns sql.NullString) *domain.DriverID {
	if !ns.Valid {
		return nil
	}
	did := domain.DriverID(ns.String)
	return &did
}

func NullStringToVehicleID(ns sql.NullString) *domain.VehicleID {
	if !ns.Valid {
		return nil
	}
	vid := domain.VehicleID(ns.String)
	return &vid
}

// Trip row -> TripWithJoins
type TripRowFields struct {
	Base       db.Trip
	DriverID   *string
	DriverFN   *string
	DriverLN   *string
	VehicleReg *string
	VehicleNum *string
	RouteSrc   string
	RouteDest  string
}

func tripRowToWithJoins(
	id string, tripNumber string,
	bookingID, driverID, vehicleID sql.NullString,
	routeID string, departureTime time.Time, arrivalTime sql.NullTime,
	status string, remarks sql.NullString,
	createdAt, updatedAt time.Time,
	driverDisplayID, driverFirstName, driverLastName sql.NullString,
	vehicleReg, vehicleNum, routeSrc, routeDest sql.NullString,
) repository.TripWithJoins {
	t := db.Trip{
		ID:            id,
		TripNumber:    tripNumber,
		BookingID:     bookingID,
		DriverID:      driverID,
		VehicleID:     vehicleID,
		RouteID:       routeID,
		DepartureTime: departureTime,
		ArrivalTime:   arrivalTime,
		Status:        status,
		Remarks:       remarks,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}

	var src, dest string
	if routeSrc.Valid {
		src = routeSrc.String
	}
	if routeDest.Valid {
		dest = routeDest.String
	}

	return repository.TripWithJoins{
		Trip:                toDomainTrip(t),
		DriverDisplayID:     fromNullString(driverDisplayID),
		DriverFirstName:     fromNullString(driverFirstName),
		DriverLastName:      fromNullString(driverLastName),
		VehicleRegistration: fromNullString(vehicleReg),
		VehicleNumber:       fromNullString(vehicleNum),
		RouteSource:         src,
		RouteDestination:    dest,
	}
}

// Invoice row -> InvoiceWithJoins
func invoiceRowToWithJoins(
	id string, invoiceNumber string, bookingID string, customerID string,
	tripID sql.NullString, subtotal float64, tax float64, discount float64,
	total float64, paymentStatus string, createdAt, updatedAt time.Time,
	customerName string, customerCompany sql.NullString,
	bookingNumber sql.NullString, tripNumber sql.NullString,
) repository.InvoiceWithJoins {
	i := db.Invoice{
		ID:            id,
		InvoiceNumber: invoiceNumber,
		BookingID:     bookingID,
		CustomerID:    customerID,
		TripID:        tripID,
		Subtotal:      subtotal,
		Tax:           tax,
		Discount:      discount,
		Total:         total,
		PaymentStatus: paymentStatus,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}

	var bNum string
	if bookingNumber.Valid {
		bNum = bookingNumber.String
	}

	return repository.InvoiceWithJoins{
		Invoice:         toDomainInvoice(i),
		CustomerName:    customerName,
		CustomerCompany: fromNullString(customerCompany),
		BookingNumber:   bNum,
		TripNumber:      fromNullString(tripNumber),
	}
}

// Payment row -> PaymentWithInvoice
func paymentRowToWithInvoice(
	id string, invoiceID string, paymentDate time.Time, amount float64, method string,
	reference sql.NullString, remarks sql.NullString, createdAt, updatedAt time.Time,
	invoiceNumber string, invoiceTotal float64, invoicePaymentStatus string,
) repository.PaymentWithInvoice {
	p := db.Payment{
		ID:          id,
		InvoiceID:   invoiceID,
		PaymentDate: paymentDate,
		Amount:      amount,
		Method:      method,
		Reference:   reference,
		Remarks:     remarks,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	return repository.PaymentWithInvoice{
		Payment:              toDomainPayment(p),
		InvoiceNumber:        invoiceNumber,
		InvoiceTotal:         invoiceTotal,
		InvoicePaymentStatus: invoicePaymentStatus,
		CustomerName:         nil,
	}
}

func auditLogRowToWithUser(
	id string, userID sql.NullString, action string, tableName string,
	recordID sql.NullString, oldValues sql.NullString, newValues sql.NullString,
	ipAddress sql.NullString, createdAt time.Time, userName sql.NullString,
) repository.AuditLogWithUser {
	return repository.AuditLogWithUser{
		AuditLog: domain.AuditLog{
			ID:        domain.FileID(id),
			UserID:    nullStringToUserID(userID),
			Action:    action,
			TableName: tableName,
			RecordID:  fromNullString(recordID),
			OldValues: fromNullString(oldValues),
			NewValues: fromNullString(newValues),
			IPAddress: fromNullString(ipAddress),
			CreatedAt: createdAt,
		},
		UserName: fromNullString(userName),
	}
}

func nullStringToUserID(ns sql.NullString) *domain.UserID {
	if !ns.Valid {
		return nil
	}
	uid := domain.UserID(ns.String)
	return &uid
}
