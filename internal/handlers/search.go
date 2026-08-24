package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"transport-app/internal/shared"
)

// Global search across the core entities. Each section is gated by its own
// read permission so the result set never leaks resources the user cannot
// list. LIKE wildcards in user input are escaped (ESCAPE '\').

type SearchRow struct {
	ID    string
	Title string
	Sub   string
	Href  string
}

type SearchSection struct {
	Key   string
	Label string
	Rows  []SearchRow
	Total int
}

func likeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (a *App) canRead(r *http.Request, userID, resource string) bool {
	if a.AuthSrv == nil || userID == "" {
		return false
	}
	return a.AuthSrv.Can(userID, resource, "read")
}

// SearchPage renders GET /search?q= — grouped cross-entity results.
func (a *App) SearchPage(w http.ResponseWriter, r *http.Request) {
	session, _ := a.getUserFromContext(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))

	sections := []SearchSection{}
	if q != "" && a.DB != nil {
		like := "%" + likeEscape(q) + "%"
		userID := ""
		if session != nil {
			userID = session.UserID
		}
		// Spec 22 §2.5: every section is tenant-scoped from the request
		// context (never the query string).
		tenant := string(shared.TenantIDFromContext(r.Context()))
		if tenant == "" {
			tenant = string(shared.DefaultTenant)
		}

		specs := a.buildSearchSpecs(tenant, like)
		sections = a.runSearchSections(r, userID, specs)
	}

	a.renderPage(w, r, "search_results.html", PageData{
		Title: "Search",
		User:  session,
		Extra: map[string]interface{}{
			"Query":    q,
			"Sections": sections,
		},
	})
}

// fetchRows runs one search query and closes its rows via defer — sqlclosecheck
// requires defer even though errors here are non-fatal (section is skipped).
func fetchRows(r *http.Request, db *sql.DB, query string, args []any, scan func(func(...any) error) (SearchRow, error)) []SearchRow {
	rows, err := db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil
	}
	defer func() { _ = rows.Close() }()
	var out []SearchRow
	for rows.Next() {
		row, err := scan(rows.Scan)
		if err != nil {
			break
		}
		out = append(out, row)
	}
	return out
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

// searchSpec describes one searchable entity group.
type searchSpec struct {
	key, label, countSQL, rowSQL string
	args                         []any
	scan                         func(scan func(...any) error) (SearchRow, error)
}

// buildSearchSpecs returns the per-entity query set. Every query filters on
// the context tenant (Spec 22 §2.5); customers is scoped through its
// bookings because the table carries no tenant column.
func (a *App) buildSearchSpecs(tenant, like string) []searchSpec {
	esc := "ESCAPE '\\'"
	_ = esc
	return []searchSpec{
		{
			key: "bookings", label: "Bookings",
			countSQL: `SELECT COUNT(*) FROM bookings b LEFT JOIN customers c ON c.id = b.customer_id
					WHERE b.tenant_id = ? AND (b.booking_number LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')`,
			rowSQL: `SELECT b.id, b.booking_number, b.status, COALESCE(c.name, '') FROM bookings b
					LEFT JOIN customers c ON c.id = b.customer_id
					WHERE b.tenant_id = ? AND (b.booking_number LIKE ? ESCAPE '\' OR c.name LIKE ? ESCAPE '\')
					ORDER BY b.created_at DESC LIMIT 5`,
			args: []any{tenant, like, like},
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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
			scan: func(scan func(...any) error) (SearchRow, error) {
				var row SearchRow
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

// runSearchSections executes each permitted section query.
func (a *App) runSearchSections(r *http.Request, userID string, specs []searchSpec) []SearchSection {
	sections := []SearchSection{}
	for _, spec := range specs {
		if !a.canRead(r, userID, spec.key) {
			continue
		}
		section := SearchSection{Key: spec.key, Label: spec.label}
		if err := a.DB.QueryRowContext(r.Context(), spec.countSQL, spec.args...).Scan(&section.Total); err != nil {
			section.Total = 0
			continue
		}
		if section.Total == 0 {
			continue
		}
		section.Rows = fetchRows(r, a.DB, spec.rowSQL, spec.args, spec.scan)
		sections = append(sections, section)
	}
	return sections
}

// searchResponse is the Spec 22 §2.5 wire shape: one array per entity type.
type searchResponse struct {
	Vehicles  []SearchRow `json:"vehicles"`
	Drivers   []SearchRow `json:"drivers"`
	Bookings  []SearchRow `json:"bookings"`
	Invoices  []SearchRow `json:"invoices"`
	EwayBills []SearchRow `json:"eway_bills"`
}

// SearchAPI handles GET /api/search?q= — result-scoped by permission,
// tenant-scoped from context. Empty arrays are always present so clients
// can render stable groups.
func (a *App) SearchAPI(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	resp := searchResponse{
		Vehicles:  []SearchRow{},
		Drivers:   []SearchRow{},
		Bookings:  []SearchRow{},
		Invoices:  []SearchRow{},
		EwayBills: []SearchRow{},
	}
	w.Header().Set("Content-Type", "application/json")

	if q == "" || a.DB == nil {
		json.NewEncoder(w).Encode(resp)
		return
	}

	userID := ""
	if sess, ok := a.getUserFromContext(r); ok && sess != nil {
		userID = sess.UserID
	}

	like := "%" + likeEscape(q) + "%"
	tenant := string(shared.TenantIDFromContext(r.Context()))
	if tenant == "" {
		tenant = string(shared.DefaultTenant)
	}
	specs := a.buildSearchSpecs(tenant, like)
	sections := a.runSearchSections(r, userID, specs)
	for _, s := range sections {
		switch s.Key {
		case "vehicles":
			resp.Vehicles = s.Rows
		case "drivers":
			resp.Drivers = s.Rows
		case "bookings":
			resp.Bookings = s.Rows
		case "invoices":
			resp.Invoices = s.Rows
		case "eway_bills":
			resp.EwayBills = s.Rows
		}
	}
	_ = json.NewEncoder(w).Encode(resp)
}
