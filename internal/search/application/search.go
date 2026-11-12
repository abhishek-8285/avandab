package application

// Global cross-entity search, extracted from internal/handlers/search.go
// (handlers-end-state ADR, Phase 2). The handler keeps the permission
// closure (AuthSrv.Can), rendering and wire response shaping; this file owns
// the per-entity queries, tenant scoping (Spec 22 §2.5) and LIKE escaping.
//
// Each section is gated by the caller-supplied canRead(resource) predicate so
// the result set never leaks resources the user cannot list. Every query
// filters on the request tenant; customers is scoped through its bookings
// because the table carries no tenant column.

import (
	"context"
	"database/sql"
	"strings"
)

// Row is one search hit.
type Row struct {
	ID    string
	Title string
	Sub   string
	Href  string
}

// Section is one entity group of search hits.
type Section struct {
	Key   string
	Label string
	Rows  []Row
	Total int
}

// LikeEscape escapes LIKE wildcards in user input (paired with ESCAPE '\').
func LikeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// spec describes one searchable entity group.
type spec struct {
	key, label, countSQL, rowSQL string
	args                         []any
	scan                         func(scan func(...any) error) (Row, error)
}

// Service runs the global search.
type Service struct {
	db *sql.DB
}

// NewService builds a search service.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// Search executes every permitted section for rawQuery in tenantID. A blank
// query (or nil DB) yields no sections.
func (s *Service) Search(ctx context.Context, tenantID, rawQuery string, canRead func(resource string) bool) []Section {
	q := strings.TrimSpace(rawQuery)
	if q == "" || s == nil || s.db == nil {
		return nil
	}
	like := "%" + LikeEscape(q) + "%"
	sections := []Section{}
	for _, sp := range buildSpecs(tenantID, like) {
		if canRead != nil && !canRead(sp.key) {
			continue
		}
		section := Section{Key: sp.key, Label: sp.label}
		if err := s.db.QueryRowContext(ctx, sp.countSQL, sp.args...).Scan(&section.Total); err != nil {
			section.Total = 0
			continue
		}
		if section.Total == 0 {
			continue
		}
		section.Rows = s.fetchRows(ctx, sp.rowSQL, sp.args, sp.scan)
		sections = append(sections, section)
	}
	return sections
}

// fetchRows runs one search query and closes its rows via defer —
// sqlclosecheck requires defer even though errors here are non-fatal (the
// section is skipped).
func (s *Service) fetchRows(ctx context.Context, query string, args []any, scan func(func(...any) error) (Row, error)) []Row {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []Row
	for rows.Next() {
		row, err := scan(rows.Scan)
		if err != nil {
			break
		}
		out = append(out, row)
	}
	return out
}

// buildSpecs returns the per-entity query set. Every query filters on the
// context tenant (Spec 22 §2.5); customers is scoped through its bookings
// because the table carries no tenant column.
func buildSpecs(tenant, like string) []spec {
	return []spec{
		{
			key: "bookings", label: "Bookings",
			countSQL: `SELECT COUNT(*) FROM bookings b LEFT JOIN customers c ON c.id = b.customer_id
					WHERE b.tenant_id = ? AND (b.booking_number LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')`,
			rowSQL: `SELECT b.id, b.booking_number, b.status, COALESCE(c.name, '') FROM bookings b
					LEFT JOIN customers c ON c.id = b.customer_id
					WHERE b.tenant_id = ? AND (b.booking_number LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')
					ORDER BY b.created_at DESC LIMIT 5`,
			args: []any{tenant, like, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var status, customer string
				if err := scan(&row.ID, &row.Title, &status, &customer); err != nil {
					return row, err
				}
				row.Sub = strings.Join(nonEmpty(status, customer), " · ")
				row.Href = "/bookings/" + row.ID
				return row, nil
			},
		},
		{
			key: "trips", label: "Trips",
			countSQL: `SELECT COUNT(*) FROM trips WHERE tenant_id = ? AND trip_number LIKE ? ESCAPE '\'`,
			rowSQL: `SELECT id, trip_number, status FROM trips
					WHERE tenant_id = ? AND trip_number LIKE ? ESCAPE '\' ORDER BY created_at DESC LIMIT 5`,
			args: []any{tenant, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var status string
				if err := scan(&row.ID, &row.Title, &status); err != nil {
					return row, err
				}
				row.Sub = status
				row.Href = "/trips/" + row.ID
				return row, nil
			},
		},
		{
			key: "vehicles", label: "Vehicles",
			countSQL: `SELECT COUNT(*) FROM vehicles WHERE tenant_id = ? AND (registration_number LIKE ? ESCAPE '\' OR vehicle_number LIKE ? ESCAPE '\')`,
			rowSQL: `SELECT id, registration_number, COALESCE(vehicle_number, '') FROM vehicles
					WHERE tenant_id = ? AND (registration_number LIKE ? ESCAPE '\' OR vehicle_number LIKE ? ESCAPE '\')
					ORDER BY registration_number LIMIT 5`,
			args: []any{tenant, like, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var num string
				if err := scan(&row.ID, &row.Title, &num); err != nil {
					return row, err
				}
				if num != "" && num != row.Title {
					row.Sub = num
				}
				row.Href = "/vehicles/" + row.ID
				return row, nil
			},
		},
		{
			key: "drivers", label: "Drivers",
			countSQL: `SELECT COUNT(*) FROM drivers WHERE tenant_id = ? AND (first_name LIKE ? ESCAPE '\' OR last_name LIKE ? ESCAPE '\'
					OR phone LIKE ? ESCAPE '\' OR license_number LIKE ? ESCAPE '\')`,
			rowSQL: `SELECT id, first_name || ' ' || last_name, phone, license_number FROM drivers
					WHERE tenant_id = ? AND (first_name LIKE ? ESCAPE '\' OR last_name LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\' OR license_number LIKE ? ESCAPE '\')
					ORDER BY first_name LIMIT 5`,
			args: []any{tenant, like, like, like, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var phone, licence string
				if err := scan(&row.ID, &row.Title, &phone, &licence); err != nil {
					return row, err
				}
				row.Sub = strings.Join(nonEmpty(phone, licence), " · ")
				row.Href = "/drivers/" + row.ID
				return row, nil
			},
		},
		{
			key: "customers", label: "Customers",
			countSQL: `SELECT COUNT(*) FROM customers WHERE EXISTS (
						SELECT 1 FROM bookings b WHERE b.customer_id = customers.id AND b.tenant_id = ?
					) AND (name LIKE ? ESCAPE '\' OR COALESCE(company,'') LIKE ? ESCAPE '\'
					OR COALESCE(gst,'') LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\')`,
			rowSQL: `SELECT id, name, COALESCE(company, ''), COALESCE(gst, ''), phone FROM customers
					WHERE EXISTS (
						SELECT 1 FROM bookings b WHERE b.customer_id = customers.id AND b.tenant_id = ?
					) AND (name LIKE ? ESCAPE '\' OR COALESCE(company,'') LIKE ? ESCAPE '\' OR COALESCE(gst,'') LIKE ? ESCAPE '\' OR phone LIKE ? ESCAPE '\')
					ORDER BY name LIMIT 5`,
			args: []any{tenant, like, like, like, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var company, gst, phone string
				if err := scan(&row.ID, &row.Title, &company, &gst, &phone); err != nil {
					return row, err
				}
				row.Sub = strings.Join(nonEmpty(company, gst, phone), " · ")
				row.Href = "/customers/" + row.ID
				return row, nil
			},
		},
		{
			key: "invoices", label: "Invoices",
			countSQL: `SELECT COUNT(*) FROM invoices WHERE tenant_id = ? AND invoice_number LIKE ? ESCAPE '\'`,
			rowSQL: `SELECT id, invoice_number, status FROM invoices
					WHERE tenant_id = ? AND invoice_number LIKE ? ESCAPE '\' ORDER BY created_at DESC LIMIT 5`,
			args: []any{tenant, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var status string
				if err := scan(&row.ID, &row.Title, &status); err != nil {
					return row, err
				}
				row.Sub = status
				row.Href = "/invoices/" + row.ID
				return row, nil
			},
		},
		{
			key: "eway_bills", label: "E-Way Bills",
			countSQL: `SELECT COUNT(*) FROM eway_bills e LEFT JOIN trips t ON t.id = e.trip_id
					WHERE t.tenant_id = ? AND e.ewb_number LIKE ? ESCAPE '\'`,
			rowSQL: `SELECT e.id, e.ewb_number, e.status FROM eway_bills e
					LEFT JOIN trips t ON t.id = e.trip_id
					WHERE t.tenant_id = ? AND e.ewb_number LIKE ? ESCAPE '\'
					ORDER BY e.created_at DESC LIMIT 5`,
			args: []any{tenant, like},
			scan: func(scan func(...any) error) (Row, error) {
				var row Row
				var status string
				if err := scan(&row.ID, &row.Title, &status); err != nil {
					return row, err
				}
				row.Sub = status
				row.Href = "/ewaybill/" + row.ID
				return row, nil
			},
		},
	}
}
