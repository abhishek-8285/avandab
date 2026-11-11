package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	searchapp "transport-app/internal/search/application"
	"transport-app/internal/shared"
)

// Global search across the core entities. Each section is gated by its own
// read permission so the result set never leaks resources the user cannot
// list. LIKE wildcards in user input are escaped (ESCAPE '\').

// SearchRow and SearchSection live in internal/search/application
// (handlers-end-state ADR, Phase 2); aliases keep template/test references
// unchanged.
type SearchRow = searchapp.Row

type SearchSection = searchapp.Section

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
		userID := ""
		if session != nil {
			userID = session.UserID
		}
		// Spec 22 §2.5: every section is tenant-scoped from the request
		// context (never the query string).
		tenant := string(shared.MustTenantID(r.Context()))
		sections = searchapp.NewService(a.DB).Search(r.Context(), tenant, q, func(resource string) bool {
			return a.canRead(r, userID, resource)
		})
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

	tenant := string(shared.MustTenantID(r.Context()))
	sections := searchapp.NewService(a.DB).Search(r.Context(), tenant, q, func(resource string) bool {
		return a.canRead(r, userID, resource)
	})
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
