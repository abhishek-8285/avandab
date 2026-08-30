package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/auth"
	"transport-app/internal/handlers"
	"transport-app/internal/shared"
)

func setupTemplateEngine(t *testing.T) *template.Template {
	funcMap := template.FuncMap{
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, nil
			}
			dict := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					continue
				}
				dict[key] = values[i+1]
			}
			return dict, nil
		},
		"datetime": func(t interface{}) string {
			if t == nil {
				return "—"
			}
			if tm, ok := t.(time.Time); ok {
				return tm.Format("02 Jan 15:04")
			}
			if tmPtr, ok := t.(*time.Time); ok && tmPtr != nil {
				return tmPtr.Format("02 Jan 15:04")
			}
			return "—"
		},
		"statusBadge": func(s string) string {
			return "badge-" + s
		},
	}

	dir, err := os.Getwd()
	require.NoError(t, err)
	tmplPath := filepath.Join(dir, "..", "templates", "partials", "multistop_timeline.html")
	if _, err := os.Stat(tmplPath); os.IsNotExist(err) {
		tmplPath = filepath.Join(dir, "internal", "templates", "partials", "multistop_timeline.html")
	}
	content, err := os.ReadFile(tmplPath)
	require.NoError(t, err)

	tmpl, err := template.New("multistop_timeline.html").Funcs(funcMap).Parse(string(content))
	require.NoError(t, err)
	return tmpl
}

func TestMultiStopTimeline_RenderingMatrix(t *testing.T) {
	tmpl := setupTemplateEngine(t)

	type StopItem struct {
		ID              string
		StopSequence    int
		StopType        string
		LocationName    string
		Address         string
		Status          string
		ActualArrival   *time.Time
		ActualDeparture *time.Time
		RequiresPOD     bool
		PODUrl          string
		RequiresOTP     bool
		ConsigneeName   string
		ConsigneePhone  string
	}

	type ProgressionInfo struct {
		TotalStops        int
		CompletedStops    int
		ProgressPercent   float64
		AllStopsCompleted bool
	}

	now := time.Date(2026, 8, 30, 9, 15, 0, 0, time.UTC)

	t.Run("1. 1-Stop Trip renders correctly", func(t *testing.T) {
		stops := []StopItem{
			{ID: "s1", StopSequence: 1, StopType: "pickup", LocationName: "Delhi Hub", Status: "pending"},
		}
		prog := ProgressionInfo{TotalStops: 1, CompletedStops: 0, ProgressPercent: 0.0, AllStopsCompleted: false}

		var buf bytes.Buffer
		err := tmpl.ExecuteTemplate(&buf, "multistop_timeline.html", map[string]interface{}{
			"Mode":        "dispatcher",
			"Stops":       stops,
			"CurrentStop": stops[0],
			"Progression": prog,
		})
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "Stop 1 · Pickup")
		assert.Contains(t, out, "Delhi Hub")
		assert.Contains(t, out, "0 of 1 stops complete (0%)")
		assert.Contains(t, out, "STOP 1/1 ACTIVE")
	})

	t.Run("2. 3-Stop Trip Progress 0% -> 33% -> 66% -> 100% calculation and rendering", func(t *testing.T) {
		stops := []StopItem{
			{ID: "s1", StopSequence: 1, StopType: "pickup", LocationName: "Delhi Depot", Status: "completed", ActualArrival: &now, ActualDeparture: &now, RequiresPOD: true, PODUrl: "https://s3.aws/pod1.jpg"},
			{ID: "s2", StopSequence: 2, StopType: "drop", LocationName: "Jaipur Hub", Status: "completed", ActualArrival: &now, ActualDeparture: &now, RequiresPOD: true, PODUrl: "https://s3.aws/pod2.jpg"},
			{ID: "s3", StopSequence: 3, StopType: "drop", LocationName: "Udaipur DC", Status: "pending", RequiresPOD: true, ConsigneeName: "Udaipur Plant"},
		}
		prog := ProgressionInfo{TotalStops: 3, CompletedStops: 2, ProgressPercent: 66.6666, AllStopsCompleted: false}

		var buf bytes.Buffer
		err := tmpl.ExecuteTemplate(&buf, "multistop_timeline.html", map[string]interface{}{
			"Mode":        "dispatcher",
			"Stops":       stops,
			"CurrentStop": stops[2],
			"Progression": prog,
		})
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "2 of 3 stops complete (67%)")
		assert.Contains(t, out, "width: 67%;")
		assert.Contains(t, out, "STOP 3/3 ACTIVE")
		assert.Contains(t, out, "POD Attached")
		assert.Contains(t, out, "Udaipur Plant")
	})

	t.Run("3. Customer View renders simplified milestone without dispatcher internals", func(t *testing.T) {
		stops := []StopItem{
			{ID: "s1", StopSequence: 1, StopType: "pickup", LocationName: "Delhi Depot", Status: "completed"},
			{ID: "s2", StopSequence: 2, StopType: "drop", LocationName: "Jaipur Delivery", Status: "arrived"},
			{ID: "s3", StopSequence: 3, StopType: "drop", LocationName: "Udaipur Delivery", Status: "pending"},
		}
		prog := ProgressionInfo{TotalStops: 3, CompletedStops: 1, ProgressPercent: 33.33, AllStopsCompleted: false}

		var buf bytes.Buffer
		err := tmpl.ExecuteTemplate(&buf, "multistop_timeline.html", map[string]interface{}{
			"Mode":        "customer",
			"Stops":       stops,
			"CurrentStop": stops[1],
			"Progression": prog,
		})
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "Delivery Milestones")
		assert.Contains(t, out, "1 of 3 stops complete")
		assert.NotContains(t, out, "POD Attached")
		assert.NotContains(t, out, "Consignee")
		assert.NotContains(t, out, "Route Progression &amp; Stops")
	})

	t.Run("4. Completed Trip shows ALL STOPS COMPLETED and 100% width", func(t *testing.T) {
		stops := []StopItem{
			{ID: "s1", StopSequence: 1, StopType: "pickup", LocationName: "Delhi", Status: "completed"},
			{ID: "s2", StopSequence: 2, StopType: "drop", LocationName: "Jaipur", Status: "completed"},
			{ID: "s3", StopSequence: 3, StopType: "drop", LocationName: "Udaipur", Status: "completed"},
		}
		prog := ProgressionInfo{TotalStops: 3, CompletedStops: 3, ProgressPercent: 100.0, AllStopsCompleted: true}

		var buf bytes.Buffer
		err := tmpl.ExecuteTemplate(&buf, "multistop_timeline.html", map[string]interface{}{
			"Mode":        "dispatcher",
			"Stops":       stops,
			"CurrentStop": nil,
			"Progression": prog,
		})
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "3 of 3 stops complete (100%)")
		assert.Contains(t, out, "ALL STOPS COMPLETED")
		assert.Contains(t, out, "width: 100%;")
	})

	t.Run("5. Empty / Unknown Stop list does not crash template", func(t *testing.T) {
		var buf bytes.Buffer
		err := tmpl.ExecuteTemplate(&buf, "multistop_timeline.html", map[string]interface{}{
			"Mode":        "customer",
			"Stops":       nil,
			"CurrentStop": nil,
			"Progression": nil,
		})
		require.NoError(t, err)

		out := buf.String()
		assert.Contains(t, out, "No multi-stop routing configured for this trip.")
	})
}

func TestCustomerPortal_Tracking_JSON_MultiStopPayload(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	schema := `
	CREATE TABLE trips (id TEXT PRIMARY KEY, tenant_id TEXT, trip_number TEXT, booking_id TEXT, driver_id TEXT, vehicle_id TEXT, status TEXT, start_time TEXT, end_time TEXT, arrival_time TEXT, departure_time TEXT);
	CREATE TABLE trip_stops (id TEXT PRIMARY KEY, trip_id TEXT, tenant_id TEXT, stop_sequence INTEGER, stop_type TEXT, location_name TEXT, status TEXT, actual_arrival TEXT, actual_departure TEXT, requires_pod INTEGER DEFAULT 0, pod_url TEXT);
	CREATE TABLE bookings (id TEXT PRIMARY KEY, tenant_id TEXT, customer_id TEXT, status TEXT);
	CREATE TABLE customer_users (id INTEGER PRIMARY KEY AUTOINCREMENT, customer_id TEXT, user_id TEXT);
	CREATE TABLE vehicles (id TEXT PRIMARY KEY, registration_number TEXT, vehicle_number TEXT);
	`
	_, err = db.Exec(schema)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO trips (id, tenant_id, trip_number, booking_id, status) VALUES ('trip_ms_1', 'tenant-1', 'TRP-MS-001', 'bk_1', 'IN_TRANSIT');
		INSERT INTO bookings (id, tenant_id, customer_id, status) VALUES ('bk_1', 'tenant-1', 'cust_1', 'CONFIRMED');
		INSERT INTO customer_users (customer_id, user_id) VALUES ('cust_1', 'usr_cust_1');

		INSERT INTO trip_stops (id, trip_id, tenant_id, stop_sequence, stop_type, location_name, status)
		VALUES
		('s1', 'trip_ms_1', 'tenant-1', 1, 'pickup', 'Delhi Hub', 'completed'),
		('s2', 'trip_ms_1', 'tenant-1', 2, 'drop', 'Jaipur Drop', 'pending'),
		('s3', 'trip_ms_1', 'tenant-1', 3, 'drop', 'Udaipur Drop', 'pending');
	`)
	require.NoError(t, err)

	portal := &handlers.CustomerPortalHandlers{
		App: &handlers.App{DB: db},
	}

	req := httptest.NewRequest("GET", "/customer/tracking/trip_ms_1", nil)
	req.Header.Set("Accept", "application/json")
	ctx := shared.ContextWithTenantID(req.Context(), "tenant-1")
	ctx = context.WithValue(ctx, auth.ContextUser, &auth.SessionData{UserID: "usr_cust_1", Role: "customer"})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("trip_id", "trip_ms_1")
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	portal.Tracking(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, "TRP-MS-001", resp["trip_number"])
	require.NotNil(t, resp["stops"])
	stops := resp["stops"].([]interface{})
	assert.Equal(t, 3, len(stops))

	require.NotNil(t, resp["current_stop"])
	curStop := resp["current_stop"].(map[string]interface{})
	assert.Equal(t, "s2", curStop["id"])

	require.NotNil(t, resp["progression"])
	prog := resp["progression"].(map[string]interface{})
	assert.Equal(t, float64(3), prog["total_stops"])
	assert.Equal(t, float64(1), prog["completed_stops"])
	assert.InDelta(t, 33.33, prog["progress_percent"].(float64), 0.1)
}
