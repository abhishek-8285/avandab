package facility

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	db "transport-app/db/generated/sqlite"
)

type Repository interface {
	Create(ctx context.Context, tenantID string, in CreateFacilityInput) (*Facility, error)
	GetByID(ctx context.Context, tenantID, id string) (*Facility, error)
	GetByCode(ctx context.Context, tenantID, code string) (*Facility, error)
	Update(ctx context.Context, tenantID, id string, in UpdateFacilityInput) (*Facility, error)
	List(ctx context.Context, tenantID string, filter FacilityFilter) ([]Facility, int64, error)
	Delete(ctx context.Context, tenantID, id string) error
}

type SQLRepository struct {
	db      *sql.DB
	queries *db.Queries
}

func NewSQLRepository(database *sql.DB) *SQLRepository {
	return &SQLRepository{
		db:      database,
		queries: db.New(database),
	}
}

func mapDBToDomain(f db.Facility) Facility {
	var lat, lon *float64
	if f.Latitude.Valid {
		v := f.Latitude.Float64
		lat = &v
	}
	if f.Longitude.Valid {
		v := f.Longitude.Float64
		lon = &v
	}
	var vf, vt *time.Time
	if f.ValidFrom.Valid {
		t := f.ValidFrom.Time
		vf = &t
	}
	if f.ValidTo.Valid {
		t := f.ValidTo.Time
		vt = &t
	}

	return Facility{
		ID:           f.ID,
		TenantID:     f.TenantID,
		FacilityCode: f.FacilityCode,
		Name:         f.Name,
		FacilityType: FacilityType(f.FacilityType),
		Plant:        f.Plant,
		Circle:       f.Circle,
		ProfitCenter: f.ProfitCenter,
		CostCenter:   f.CostCenter,
		Address:      f.Address,
		City:         f.City,
		State:        f.State,
		Pincode:      f.Pincode,
		Latitude:     lat,
		Longitude:    lon,
		ValidFrom:    vf,
		ValidTo:      vt,
		IsActive:     f.IsActive != 0,
		CreatedAt:    f.CreatedAt,
		UpdatedAt:    f.UpdatedAt,
	}
}

func (r *SQLRepository) Create(ctx context.Context, tenantID string, in CreateFacilityInput) (*Facility, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	id := "fac-" + uuid.New().String()
	var lat, lon sql.NullFloat64
	if in.Latitude != nil {
		lat = sql.NullFloat64{Float64: *in.Latitude, Valid: true}
	}
	if in.Longitude != nil {
		lon = sql.NullFloat64{Float64: *in.Longitude, Valid: true}
	}
	var vf, vt sql.NullTime
	if in.ValidFrom != nil {
		vf = sql.NullTime{Time: *in.ValidFrom, Valid: true}
	}
	if in.ValidTo != nil {
		vt = sql.NullTime{Time: *in.ValidTo, Valid: true}
	}
	isActive := int64(1)
	if in.IsActive != nil && !*in.IsActive {
		isActive = 0
	}

	row, err := r.queries.CreateFacility(ctx, db.CreateFacilityParams{
		ID:           id,
		TenantID:     tenantID,
		FacilityCode: in.FacilityCode,
		Name:         in.Name,
		FacilityType: string(in.FacilityType),
		Plant:        in.Plant,
		Circle:       in.Circle,
		ProfitCenter: in.ProfitCenter,
		CostCenter:   in.CostCenter,
		Address:      in.Address,
		City:         in.City,
		State:        in.State,
		Pincode:      in.Pincode,
		Latitude:     lat,
		Longitude:    lon,
		ValidFrom:    vf,
		ValidTo:      vt,
		IsActive:     isActive,
	})
	if err != nil {
		return nil, fmt.Errorf("create facility: %w", err)
	}

	res := mapDBToDomain(row)
	return &res, nil
}

func (r *SQLRepository) GetByID(ctx context.Context, tenantID, id string) (*Facility, error) {
	row, err := r.queries.GetFacilityByID(ctx, db.GetFacilityByIDParams{
		ID:       id,
		TenantID: tenantID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get facility by id: %w", err)
	}
	res := mapDBToDomain(row)
	return &res, nil
}

func (r *SQLRepository) GetByCode(ctx context.Context, tenantID, code string) (*Facility, error) {
	row, err := r.queries.GetFacilityByCode(ctx, db.GetFacilityByCodeParams{
		FacilityCode: code,
		TenantID:     tenantID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get facility by code: %w", err)
	}
	res := mapDBToDomain(row)
	return &res, nil
}

func (r *SQLRepository) Update(ctx context.Context, tenantID, id string, in UpdateFacilityInput) (*Facility, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	var lat, lon sql.NullFloat64
	if in.Latitude != nil {
		lat = sql.NullFloat64{Float64: *in.Latitude, Valid: true}
	}
	if in.Longitude != nil {
		lon = sql.NullFloat64{Float64: *in.Longitude, Valid: true}
	}
	var vf, vt sql.NullTime
	if in.ValidFrom != nil {
		vf = sql.NullTime{Time: *in.ValidFrom, Valid: true}
	}
	if in.ValidTo != nil {
		vt = sql.NullTime{Time: *in.ValidTo, Valid: true}
	}
	isActive := int64(1)
	if in.IsActive != nil && !*in.IsActive {
		isActive = 0
	}

	row, err := r.queries.UpdateFacility(ctx, db.UpdateFacilityParams{
		Name:         in.Name,
		FacilityType: string(in.FacilityType),
		Plant:        in.Plant,
		Circle:       in.Circle,
		ProfitCenter: in.ProfitCenter,
		CostCenter:   in.CostCenter,
		Address:      in.Address,
		City:         in.City,
		State:        in.State,
		Pincode:      in.Pincode,
		Latitude:     lat,
		Longitude:    lon,
		ValidFrom:    vf,
		ValidTo:      vt,
		IsActive:     isActive,
		ID:           id,
		TenantID:     tenantID,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("update facility: %w", err)
	}

	res := mapDBToDomain(row)
	return &res, nil
}

func (r *SQLRepository) List(ctx context.Context, tenantID string, filter FacilityFilter) ([]Facility, int64, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	activeAll := ""
	activeVal := int64(1)
	if filter.ActiveOnly == nil {
		activeAll = "all"
	} else if !*filter.ActiveOnly {
		activeVal = 0
	}

	rows, err := r.queries.SearchFacilities(ctx, db.SearchFacilitiesParams{
		TenantID:     tenantID,
		Search:       filter.Search,
		FacilityType: filter.FacilityType,
		ActiveAll:    activeAll,
		IsActive:     activeVal,
		Limit:        int64(limit),
		Offset:       int64(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("search facilities: %w", err)
	}

	total, err := r.queries.CountFacilities(ctx, db.CountFacilitiesParams{
		TenantID:     tenantID,
		Search:       filter.Search,
		FacilityType: filter.FacilityType,
		ActiveAll:    activeAll,
		IsActive:     activeVal,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count facilities: %w", err)
	}

	dtos := make([]Facility, 0, len(rows))
	for _, row := range rows {
		dtos = append(dtos, mapDBToDomain(row))
	}

	return dtos, total, nil
}

func (r *SQLRepository) Delete(ctx context.Context, tenantID, id string) error {
	return r.queries.DeleteFacility(ctx, db.DeleteFacilityParams{
		ID:       id,
		TenantID: tenantID,
	})
}
