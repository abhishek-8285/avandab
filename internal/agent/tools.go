package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"transport-app/internal/domain"
	"transport-app/internal/ewaybill"
	"transport-app/internal/service"
)

// ToolEnv carries live service dependencies + the acting user.
type ToolEnv struct {
	Services *service.Services
	UserID   string
	UserName string
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RegisterTools builds the agent's tool set bound to the service layer.
func RegisterTools(env *ToolEnv) []*RegisteredTool {
	optStr := func(v any) map[string]any {
		return map[string]any{
			"type":        "string",
			"description": fmt.Sprintf("%v", v),
		}
	}

	return []*RegisteredTool{
		{
			Name:        "search_routes",
			Description: "Search routes by source or destination city. Returns route ids, distance, estimated hours and standard fare.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": optStr("city or partial name to search, e.g. 'Mumbai' or 'mum'"),
				},
				"required": []string{"query"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				routes, _, err := env.Services.Routes.ListRoutes(ctx, in.Query, 20, 0)
				if err != nil {
					return "", err
				}
				type row struct {
					ID             string  `json:"id"`
					Source         string  `json:"source"`
					Destination    string  `json:"destination"`
					DistanceKM     float64 `json:"distance_km"`
					EstimatedHours float64 `json:"estimated_hours"`
					StandardFare   float64 `json:"standard_fare"`
				}
				out := make([]row, 0, len(routes))
				for _, r := range routes {
					out = append(out, row{r.ID.String(), r.Source, r.Destination, r.Distance, r.EstimatedHours, r.StandardFare})
				}
				if len(out) == 0 {
					return "No routes found matching: " + in.Query, nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "get_quote",
			Description: "Get a fare quote between two cities. Looks up the route and computes fare with GST. Returns distance, hours, base fare and total.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"from":        optStr("source city, e.g. 'Mumbai'"),
					"to":          optStr("destination city, e.g. 'Pune'"),
					"vehicleType": optStr("optional: truck, mini_truck, bus, van, pickup, tempo"),
				},
				"required": []string{"from", "to"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					From        string `json:"from"`
					To          string `json:"to"`
					VehicleType string `json:"vehicleType"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				routes, _, err := env.Services.Routes.ListRoutes(ctx, in.From, 50, 0)
				if err != nil {
					return "", err
				}
				var match *domain.Route
				for i := range routes {
					d, f, rev := routes[i].GetDistanceAndFare(in.From, in.To)
					if d > 0 && f >= 0 {
						r := routes[i]
						r.Distance = d
						if rev && r.ReverseStandardFare != nil {
							r.StandardFare = *r.ReverseStandardFare
						} else {
							r.StandardFare = f
						}
						match = &r
						break
					}
				}
				if match == nil {
					return fmt.Sprintf("No route found between %s and %s. Suggest: 'search_routes' to see available routes.", in.From, in.To), nil
				}
				settings, err := env.Services.Settings.GetSettings(ctx)
				if err != nil {
					return "", fmt.Errorf("settings unavailable: %w", err)
				}
				rate := settings.GSTRate
				tax := match.StandardFare * rate / 100
				return jsonString(map[string]any{
					"route_id":        match.ID.String(),
					"from":            match.Source,
					"to":              match.Destination,
					"distance_km":     match.Distance,
					"estimated_hours": match.EstimatedHours,
					"base_fare":       match.StandardFare,
					"gst_rate_pct":    rate,
					"gst_amount":      tax,
					"total_with_gst":  match.StandardFare + tax,
				})
			},
		},
		{
			Name:        "create_booking",
			Description: "Create a new booking. Requires customer_id, route_id, pickup_date (YYYY-MM-DD HH:MM), vehicle_type, passengers and price.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"customer_id":  optStr("customer id (cust_...) — search customers first if unknown"),
					"route_id":     optStr("route id (rout_...)"),
					"pickup_date":  optStr("pickup datetime 'YYYY-MM-DD HH:MM'"),
					"vehicle_type": optStr("truck, mini_truck, bus, van, pickup or tempo"),
					"passengers":   map[string]any{"type": "integer", "description": "number of passengers"},
					"price":        map[string]any{"type": "number", "description": "booking price in INR"},
					"notes":        optStr("optional notes"),
				},
				"required": []string{"customer_id", "route_id", "pickup_date", "vehicle_type", "passengers", "price"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					CustomerID  string  `json:"customer_id"`
					RouteID     string  `json:"route_id"`
					PickupDate  string  `json:"pickup_date"`
					VehicleType string  `json:"vehicle_type"`
					Passengers  int64   `json:"passengers"`
					Price       float64 `json:"price"`
					Notes       string  `json:"notes"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				t, err := time.Parse("2006-01-02 15:04", in.PickupDate)
				if err != nil {
					return "", fmt.Errorf("pickup_date must be 'YYYY-MM-DD HH:MM', got %q", in.PickupDate)
				}
				vt := domain.VehicleType(strings.ToLower(in.VehicleType))
				b, err := env.Services.Bookings.CreateBooking(ctx, service.CreateBookingRequest{
					CustomerID:  domain.CustomerID(in.CustomerID),
					RouteID:     domain.RouteID(in.RouteID),
					PickupDate:  t.Format(time.RFC3339),
					VehicleType: vt,
					Passengers:  in.Passengers,
					Price:       in.Price,
					Notes:       in.Notes,
				})
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{
					"booking_id":     b.ID.String(),
					"booking_number": b.BookingNumber,
					"status":         b.Status,
					"pickup_date":    b.PickupDate.Format(time.RFC3339),
					"price":          b.Price,
				})
			},
		},
		{
			Name:        "search_customers",
			Description: "Search customers by name, phone or company.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": optStr("name, phone or company to search"),
				},
				"required": []string{"query"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Query string `json:"query"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				customers, _, err := env.Services.Customers.ListCustomers(ctx, in.Query, 10, 0)
				if err != nil {
					return "", err
				}
				type row struct {
					ID           string `json:"id"`
					Name         string `json:"name"`
					Company      string `json:"company"`
					Phone        string `json:"phone"`
					CustomerCode string `json:"customer_code"`
				}
				out := make([]row, 0, len(customers))
				for _, c := range customers {
					comp := ""
					if c.Company != nil {
						comp = *c.Company
					}
					out = append(out, row{c.ID.String(), c.Name, comp, c.Phone, c.CustomerCode})
				}
				if len(out) == 0 {
					return "No customers found matching: " + in.Query, nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "get_booking",
			Description: "Get booking details by booking number (e.g. BK-0001) or booking id.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"number": optStr("booking number or id"),
				},
				"required": []string{"number"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Number string `json:"number"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				b, err := env.Services.Bookings.GetBookingByNumber(ctx, in.Number)
				if err != nil {
					return "", fmt.Errorf("booking not found: %v", err)
				}
				return jsonString(map[string]any{
					"id":           b.ID.String(),
					"number":       b.BookingNumber,
					"customer":     b.CustomerName,
					"route":        b.RouteSource + " -> " + b.RouteDestination,
					"pickup_date":  b.PickupDate.Format(time.RFC3339),
					"vehicle_type": b.VehicleType,
					"passengers":   b.Passengers,
					"price":        b.Price,
					"status":       b.Status,
				})
			},
		},
		{
			Name:        "list_trips",
			Description: "List trips, optionally filtered by status (draft, scheduled, assigned, started, reached_pickup, in_transit, delivered, completed, cancelled) or a search term.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": optStr("optional status filter"),
					"query":  optStr("optional search (trip number, driver, route)"),
					"limit":  map[string]any{"type": "integer", "description": "max results"},
				},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Status string `json:"status"`
					Query  string `json:"query"`
					Limit  int    `json:"limit"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				if in.Limit <= 0 {
					in.Limit = 20
				}
				trips, _, err := env.Services.Trips.ListTrips(ctx, in.Query, in.Status, in.Limit, 0)
				if err != nil {
					return "", err
				}
				type row struct {
					ID        string `json:"id"`
					Number    string `json:"number"`
					Route     string `json:"route"`
					Driver    string `json:"driver"`
					Vehicle   string `json:"vehicle"`
					Departure string `json:"departure_time"`
					Status    string `json:"status"`
				}
				out := make([]row, 0, len(trips))
				for _, t := range trips {
					drv := ""
					if t.DriverFirstName != nil {
						drv = *t.DriverFirstName + " " + derefStr(t.DriverLastName)
					}
					veh := ""
					if t.VehicleNumber != nil {
						veh = *t.VehicleNumber
					}
					out = append(out, row{t.ID.String(), t.TripNumber, t.RouteSource + " -> " + t.RouteDestination, strings.TrimSpace(drv), veh, t.DepartureTime.Format("2006-01-02 15:04"), string(t.Status)})
				}
				if len(out) == 0 {
					return "No trips found" + statusClause(in.Status), nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "get_trip",
			Description: "Get trip details by trip number (e.g. TR-0001) or trip id.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"number": optStr("trip number or id"),
				},
				"required": []string{"number"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Number string `json:"number"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				t, err := env.Services.Trips.GetTripByNumber(ctx, in.Number)
				if err != nil {
					return "", fmt.Errorf("trip not found: %v", err)
				}
				drv := ""
				if t.DriverFirstName != nil {
					drv = *t.DriverFirstName + " " + derefStr(t.DriverLastName)
				}
				return jsonString(map[string]any{
					"id":         t.ID.String(),
					"number":     t.TripNumber,
					"route":      t.RouteSource + " -> " + t.RouteDestination,
					"driver":     strings.TrimSpace(drv),
					"vehicle":    derefStr(t.VehicleNumber),
					"departure":  t.DepartureTime.Format("2006-01-02 15:04"),
					"status":     string(t.Status),
					"booking_id": bookingIDStr(t.BookingID),
				})
			},
		},
		{
			Name:        "list_available_drivers",
			Description: "List drivers currently available for assignment.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				drivers, err := env.Services.Drivers.GetAvailableDrivers(ctx)
				if err != nil {
					return "", err
				}
				type row struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Phone   string `json:"phone"`
					License string `json:"license_number"`
				}
				out := make([]row, 0, len(drivers))
				for _, d := range drivers {
					out = append(out, row{d.ID.String(), d.FirstName + " " + d.LastName, d.Phone, d.LicenseNumber})
				}
				if len(out) == 0 {
					return "No drivers available right now.", nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "list_available_vehicles",
			Description: "List vehicles currently available for assignment.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				vehicles, err := env.Services.Vehicles.GetAvailableVehicles(ctx)
				if err != nil {
					return "", err
				}
				type row struct {
					ID       string `json:"id"`
					Number   string `json:"registration_number"`
					Type     string `json:"type"`
					Capacity int64  `json:"capacity"`
				}
				out := make([]row, 0, len(vehicles))
				for _, v := range vehicles {
					if env.Services != nil && env.Services.Vehicles != nil {
						if blocked, _, err := env.Services.Vehicles.IsMaintenanceBlocked(ctx, v.ID.String()); err == nil && blocked {
							continue
						}
					}
					out = append(out, row{v.ID.String(), v.RegistrationNumber, string(v.VehicleType), v.Capacity})
				}
				if len(out) == 0 {
					return "No vehicles available right now.", nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "assign_driver",
			Description: "Assign a driver to a trip. Requires trip id and driver id.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"trip_id":   optStr("trip id (trip_...)"),
					"driver_id": optStr("driver id (drv_...)"),
				},
				"required": []string{"trip_id", "driver_id"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					TripID   string `json:"trip_id"`
					DriverID string `json:"driver_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				t, err := env.Services.Trips.AssignDriver(ctx, domain.TripID(in.TripID), domain.DriverID(in.DriverID))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Driver %s assigned to trip %s. Status: %s", in.DriverID, in.TripID, t.Status), nil
			},
		},
		{
			Name:        "assign_vehicle",
			Description: "Assign a vehicle to a trip. Requires trip id and vehicle id. Driver must be assigned first.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"trip_id":    optStr("trip id (trip_...)"),
					"vehicle_id": optStr("vehicle id (veh_...)"),
				},
				"required": []string{"trip_id", "vehicle_id"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					TripID    string `json:"trip_id"`
					VehicleID string `json:"vehicle_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				t, err := env.Services.Trips.AssignVehicle(ctx, domain.TripID(in.TripID), domain.VehicleID(in.VehicleID))
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("Vehicle %s assigned to trip %s. Status: %s", in.VehicleID, in.TripID, t.Status), nil
			},
		},
		{
			Name:        "get_invoice",
			Description: "Get invoice details by number (e.g. INV-0001) including outstanding balance.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"number": optStr("invoice number or id"),
				},
				"required": []string{"number"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Number string `json:"number"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				inv, err := env.Services.Invoices.GetInvoiceByNumber(ctx, in.Number)
				if err != nil {
					return "", fmt.Errorf("invoice not found: %v", err)
				}
				balance, err := env.Services.Invoices.GetBalance(ctx, inv.ID)
				if err != nil {
					return "", fmt.Errorf("balance unavailable: %w", err)
				}
				return jsonString(map[string]any{
					"id":          inv.ID.String(),
					"number":      inv.InvoiceNumber,
					"customer":    inv.CustomerName,
					"total":       inv.Total,
					"paid":        inv.PaidAmount,
					"outstanding": balance,
					"status":      inv.Status,
					"due_date":    inv.DueDate,
				})
			},
		},
		{
			Name:        "list_unpaid_invoices",
			Description: "List invoices that are not fully paid (pending or partially paid).",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				invs, _, err := env.Services.Invoices.ListInvoices(ctx, "", "", 50, 0)
				if err != nil {
					return "", err
				}
				type row struct {
					ID          string  `json:"id"`
					Number      string  `json:"number"`
					Customer    string  `json:"customer"`
					Total       float64 `json:"total"`
					Paid        float64 `json:"paid"`
					Outstanding float64 `json:"outstanding"`
				}
				var out []row
				for _, inv := range invs {
					if inv.PaymentStatus == "paid" {
						continue
					}
					balance, err := env.Services.Invoices.GetBalance(ctx, inv.ID)
					if err != nil {
						return "", fmt.Errorf("balance unavailable: %w", err)
					}
					out = append(out, row{inv.ID.String(), inv.InvoiceNumber, inv.CustomerName, inv.Total, inv.PaidAmount, balance})
				}
				if len(out) == 0 {
					return "No unpaid invoices. All invoices are settled.", nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "record_payment",
			Description: "Record a payment against an invoice. Requires invoice id, amount and method (cash, upi, bank_transfer, cheque).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"invoice_id": optStr("invoice id (inv_...)"),
					"amount":     map[string]any{"type": "number", "description": "amount in INR"},
					"method":     optStr("cash, upi, bank_transfer or cheque"),
					"reference":  optStr("optional payment reference"),
					"remarks":    optStr("optional remarks"),
				},
				"required": []string{"invoice_id", "amount", "method"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					InvoiceID string  `json:"invoice_id"`
					Amount    float64 `json:"amount"`
					Method    string  `json:"method"`
					Reference string  `json:"reference"`
					Remarks   string  `json:"remarks"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				if in.Amount <= 0 {
					return "", fmt.Errorf("amount must be greater than zero, got %.2f", in.Amount)
				}
				balance, err := env.Services.Invoices.GetBalance(ctx, domain.InvoiceID(in.InvoiceID))
				if err != nil {
					return "", fmt.Errorf("balance unavailable: %w", err)
				}
				if in.Amount > balance {
					return "", fmt.Errorf("payment amount %.2f exceeds outstanding balance %.2f; record only up to the outstanding amount", in.Amount, balance)
				}
				p, err := env.Services.Payments.RecordPayment(ctx, domain.InvoiceID(in.InvoiceID), in.Amount, domain.PaymentMethod(strings.ToLower(in.Method)), in.Reference, in.Remarks, "")
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{
					"payment_id": p.ID.String(),
					"invoice_id": in.InvoiceID,
					"amount":     p.Amount,
					"method":     p.Method,
					"status":     "recorded",
				})
			},
		},
		{
			Name:        "get_revenue",
			Description: "Get total revenue and monthly revenue figures.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				total, err := env.Services.Payments.GetTotalRevenue(ctx)
				if err != nil {
					return "", fmt.Errorf("revenue unavailable: %w", err)
				}
				monthly, err := env.Services.Payments.GetMonthlyRevenue(ctx)
				if err != nil {
					return "", fmt.Errorf("revenue unavailable: %w", err)
				}
				rows := make([]map[string]any, 0, len(monthly))
				for _, m := range monthly {
					rows = append(rows, map[string]any{"month": m.Month, "total": m.Total})
				}
				return jsonString(map[string]any{"total_revenue": total, "monthly": rows})
			},
		},
		{
			Name:        "list_pending_kharcha",
			Description: "List driver expenses (kharcha) waiting for approval.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				expenses, err := env.Services.Kharcha.ListPendingExpenses(ctx)
				if err != nil {
					return "", err
				}
				type row struct {
					ID       string  `json:"id"`
					Trip     string  `json:"trip_number"`
					Driver   string  `json:"driver"`
					Category string  `json:"category"`
					Amount   float64 `json:"amount"`
					Desc     string  `json:"description"`
				}
				out := make([]row, 0, len(expenses))
				for _, e := range expenses {
					out = append(out, row{e.ID, e.TripNumber, e.DriverName, e.Category, e.Amount, e.Description})
				}
				if len(out) == 0 {
					return "No pending kharcha expenses.", nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "approve_kharcha",
			Description: "Approve a pending driver expense (kharcha). Requires expense id.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expense_id": optStr("kharcha expense id"),
				},
				"required": []string{"expense_id"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					ExpenseID string `json:"expense_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				actor := userIDFrom(ctx)
				if actor == "" {
					actor = env.UserID
				}
				if err := env.Services.Kharcha.ApproveExpense(ctx, in.ExpenseID, actor); err != nil {
					return "", err
				}
				return fmt.Sprintf("Expense %s approved.", in.ExpenseID), nil
			},
		},
		{
			Name:        "reject_kharcha",
			Description: "Reject a pending driver expense (kharcha). Requires expense id and reason.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"expense_id": optStr("kharcha expense id"),
					"reason":     optStr("rejection reason"),
				},
				"required": []string{"expense_id", "reason"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					ExpenseID string `json:"expense_id"`
					Reason    string `json:"reason"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				actor := userIDFrom(ctx)
				if actor == "" {
					actor = env.UserID
				}
				if err := env.Services.Kharcha.RejectExpense(ctx, in.ExpenseID, actor, in.Reason); err != nil {
					return "", err
				}
				return fmt.Sprintf("Expense %s rejected: %s", in.ExpenseID, in.Reason), nil
			},
		},
		{
			Name:        "get_dashboard",
			Description: "Get today's operations summary: revenue, upcoming trips, available drivers/vehicles, pending payments.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				data, err := env.Services.Dashboard.GetDashboardData(ctx)
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{
					"today_trips_count":  data.TodaysTripsCount,
					"active_trips":       data.ActiveTripsCount,
					"completed_trips":    data.CompletedTripsCount,
					"cancelled_trips":    data.CancelledTripsCount,
					"available_drivers":  data.AvailableDriversCount,
					"available_vehicles": data.AvailableVehiclesCount,
					"pending_payments":   data.PendingPaymentsCount,
					"monthly_revenue":    data.MonthlyRevenue,
				})
			},
		},
		{
			Name:        "get_open_alerts",
			Description: "List open operational alerts (source, severity, entity, title). Optional severity filter.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"severity": optStr("optional severity filter: warning, critical, blocker"),
				},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Severity string `json:"severity"`
				}
				if len(args) > 0 {
					_ = json.Unmarshal(args, &in)
				}
				db := env.Services.DB()
				if db == nil {
					return "database not available", nil
				}
				query := `SELECT id, source, alert_type, severity, entity_type, entity_id, title, message, status, created_at FROM alerts WHERE status = 'open'`
				var queryArgs []any
				if in.Severity != "" {
					query += ` AND severity = ?`
					queryArgs = append(queryArgs, in.Severity)
				}
				query += ` ORDER BY created_at DESC LIMIT 20`

				rows, err := db.QueryContext(ctx, query, queryArgs...)
				if err != nil {
					return "No alerts found: " + err.Error(), nil
				}
				defer rows.Close()

				type alertRow struct {
					ID         string `json:"id"`
					Source     string `json:"source"`
					AlertType  string `json:"alert_type"`
					Severity   string `json:"severity"`
					EntityType string `json:"entity_type"`
					EntityID   string `json:"entity_id"`
					Title      string `json:"title"`
					Message    string `json:"message"`
					Status     string `json:"status"`
					CreatedAt  string `json:"created_at"`
				}
				var list []alertRow
				for rows.Next() {
					var a alertRow
					if err := rows.Scan(&a.ID, &a.Source, &a.AlertType, &a.Severity, &a.EntityType, &a.EntityID, &a.Title, &a.Message, &a.Status, &a.CreatedAt); err == nil {
						list = append(list, a)
					}
				}
				if len(list) == 0 {
					return "No open alerts found.", nil
				}
				return jsonString(list)
			},
		},
		{
			Name:        "extend_ewaybill",
			Description: "Request e-way bill extension for a trip. Requires trip id. Subject to geofence evidence gate.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"trip_id": optStr("required trip id to extend eway bill for"),
				},
				"required": []string{"trip_id"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					TripID string `json:"trip_id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				if in.TripID == "" {
					return "", fmt.Errorf("trip_id is required")
				}
				svc := env.Services.EWayBill
				if svc == nil {
					return "eway bill service not available", nil
				}
				rec, err := svc.GetByTrip(ctx, in.TripID)
				if err != nil {
					return fmt.Sprintf("no eway bill found for trip %s", in.TripID), nil
				}
				if rec.Status != "active" {
					return fmt.Sprintf("eway bill %s is not active (status: %s)", rec.EwbNumber, rec.Status), nil
				}
				updated, err := svc.Extend(ctx, rec.EwbNumber, ewaybill.ExtendRequest{
					EwbNumber: rec.EwbNumber,
					Reason:    "extended via ops assistant",
				})
				if err != nil {
					return fmt.Sprintf("extend failed for ewb %s: %v", rec.EwbNumber, err), nil
				}
				return fmt.Sprintf("extended ewb %s until %s", updated.EwbNumber, updated.ValidUntil.Format(time.RFC3339)), nil
			},
		},
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func bookingIDStr(id *domain.BookingID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func statusClause(status string) string {
	if status == "" {
		return "."
	}
	return " with status " + status + "."
}
