package sqlite

import (
	"context"
	"database/sql"

	"transport-app/internal/domain"
	"transport-app/internal/shared"

	db "transport-app/db/generated/sqlite"
)

// tenantIDForReads derives the acting tenant for customer reads, falling back
// to the bootstrap default so read visibility matches write-side fallback
// semantics (AGENTS.md Prohibition #4: no tenant literals at call sites).
func tenantIDForReads(ctx context.Context) string {
	if id := shared.TenantIDFromContext(ctx); id != "" {
		return string(id)
	}
	return string(shared.DefaultTenant)
}

// CustomerRepository implementation

func (r *SQLRepository) CreateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	code := sql.NullString{String: customer.CustomerCode, Valid: customer.CustomerCode != ""}
	if code.String == "" {
		code = sql.NullString{Valid: false}
	}
	meta := customer.Meta
	if meta == "" {
		meta = "{}"
	}
	custType := customer.Type
	if custType == "" {
		custType = "individual"
	}
	status := customer.Status
	if status == "" {
		status = "active"
	}
	tenantID := customer.TenantID
	if tenantID == "" {
		tenantID = tenantIDFromCtx(ctx)
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}
	}
	created, err := r.Q(ctx).CreateCustomer(ctx, db.CreateCustomerParams{
		ID:               string(customer.ID),
		CustomerCode:     code,
		Name:             customer.Name,
		Title:            nullString(customer.Title),
		Company:          nullString(customer.Company),
		ContactPerson:    nullString(customer.ContactPerson),
		Phone:            customer.Phone,
		Email:            nullString(customer.Email),
		Gst:              nullString(customer.GST),
		Address:          nullString(customer.Address),
		BillingAddress:   nullString(customer.BillingAddress),
		InternalID:       nullString(customer.InternalID),
		PhotoUrl:         nullString(customer.PhotoURL),
		PlaceUuid:        nullString(customer.PlaceUUID),
		Meta:             meta,
		Type:             custType,
		Status:           status,
		PaymentTermsDays: int64(customer.PaymentTermsDays),
		TenantID:         tenantID,
		StateCode:        nullString(customer.StateCode),
		Notes:            nullString(customer.Notes),
	})
	if err != nil {
		return domain.Customer{}, err
	}
	return toDomainCustomerFromCreateRow(created), nil
}

func (r *SQLRepository) GetCustomerByID(ctx context.Context, id domain.CustomerID) (domain.Customer, error) {
	c, err := r.Q(ctx).GetCustomerByID(ctx, db.GetCustomerByIDParams{
		ID:       string(id),
		TenantID: tenantIDForReads(ctx),
	})
	if err != nil {
		return domain.Customer{}, err
	}
	return toDomainCustomerFromGetRow(c), nil
}

func (r *SQLRepository) GetCustomerByPhone(ctx context.Context, phone string) (domain.Customer, error) {
	c, err := r.Q(ctx).GetCustomerByPhone(ctx, db.GetCustomerByPhoneParams{
		Phone:    phone,
		TenantID: tenantIDForReads(ctx),
	})
	if err != nil {
		return domain.Customer{}, err
	}
	return toDomainCustomerFromPhoneRow(c), nil
}

func (r *SQLRepository) UpdateCustomer(ctx context.Context, customer domain.Customer) (domain.Customer, error) {
	meta := customer.Meta
	if meta == "" {
		meta = "{}"
	}
	custType := customer.Type
	if custType == "" {
		custType = "individual"
	}
	status := customer.Status
	if status == "" {
		status = "active"
	}
	tenantID := customer.TenantID
	if tenantID == "" {
		tenantID = tenantIDFromCtx(ctx)
		if tenantID == "" {
			tenantID = string(shared.DefaultTenant)
		}
	}
	updated, err := r.Q(ctx).UpdateCustomer(ctx, db.UpdateCustomerParams{
		CustomerCode:     sql.NullString{String: customer.CustomerCode, Valid: customer.CustomerCode != ""},
		Name:             customer.Name,
		Title:            nullString(customer.Title),
		Company:          nullString(customer.Company),
		ContactPerson:    nullString(customer.ContactPerson),
		Phone:            customer.Phone,
		Email:            nullString(customer.Email),
		Gst:              nullString(customer.GST),
		Address:          nullString(customer.Address),
		BillingAddress:   nullString(customer.BillingAddress),
		InternalID:       nullString(customer.InternalID),
		PhotoUrl:         nullString(customer.PhotoURL),
		PlaceUuid:        nullString(customer.PlaceUUID),
		Meta:             meta,
		Type:             custType,
		Status:           status,
		PaymentTermsDays: int64(customer.PaymentTermsDays),
		TenantID:         tenantID,
		StateCode:        nullString(customer.StateCode),
		Notes:            nullString(customer.Notes),
		ID:               string(customer.ID),
	})
	if err != nil {
		return domain.Customer{}, err
	}
	return toDomainCustomerFromUpdateRow(updated), nil
}

func (r *SQLRepository) DeleteCustomer(ctx context.Context, id domain.CustomerID) error {
	return r.Q(ctx).DeleteCustomer(ctx, db.DeleteCustomerParams{
		ID:       string(id),
		TenantID: tenantIDForReads(ctx),
	})
}

func (r *SQLRepository) SearchCustomers(ctx context.Context, query string, limit, offset int) ([]domain.Customer, error) {
	rows, err := r.Q(ctx).SearchCustomers(ctx, db.SearchCustomersParams{
		TenantID: tenantIDForReads(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  sql.NullString{String: query, Valid: true},
		Column6:  sql.NullString{String: query, Valid: true},
		Column7:  sql.NullString{String: query, Valid: true},
		Column8:  sql.NullString{String: query, Valid: true},
		Limit:    int64(limit),
		Offset:   int64(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]domain.Customer, len(rows))
	for i, c := range rows {
		result[i] = toDomainCustomerFromSearchRow(c)
	}
	return result, nil
}

func (r *SQLRepository) CountCustomers(ctx context.Context, query string) (int64, error) {
	count, err := r.Q(ctx).CountCustomers(ctx, db.CountCustomersParams{
		TenantID: tenantIDForReads(ctx),
		Column2:  sql.NullString{String: query, Valid: true},
		Column3:  sql.NullString{String: query, Valid: true},
		Column4:  sql.NullString{String: query, Valid: true},
		Column5:  sql.NullString{String: query, Valid: true},
		Column6:  sql.NullString{String: query, Valid: true},
		Column7:  sql.NullString{String: query, Valid: true},
		Column8:  sql.NullString{String: query, Valid: true},
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
