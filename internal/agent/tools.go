package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	alertsapp "transport-app/internal/alerts/application"
	bookingapp "transport-app/internal/booking/application"
	customerapp "transport-app/internal/customer/application"
	dispatchapp "transport-app/internal/dispatch/application"
	"transport-app/internal/domain"
	"transport-app/internal/ewaybill"
	invoiceapp "transport-app/internal/invoice/application"
	kharchaapp "transport-app/internal/kharcha/application"
	"transport-app/internal/maintenance"
	maintdomain "transport-app/internal/maintenance/domain"
	paymentapp "transport-app/internal/payment/application"
	routeapp "transport-app/internal/route/application"
	"transport-app/internal/service"
	"transport-app/internal/shared"
	tripapp "transport-app/internal/trip/application"
	tripagg "transport-app/internal/trip/domain/aggregate"
)

// ToolEnv carries live service dependencies + the acting user.
type ToolEnv struct {
	Services *service.Services
	Dispatch *dispatchapp.Service
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
			Resource:    "routes",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				routes, err := routeapp.NewAgentRouteQueries(db).Search(ctx, shared.TenantIDFromContext(ctx), in.Query, 20)
				if err != nil {
					return "", err
				}
				if len(routes) == 0 {
					return "No routes found matching: " + in.Query, nil
				}
				return jsonString(routes)
			},
		},
		{
			Name:        "get_quote",
			Description: "Get a fare quote between two cities. Looks up the route and computes fare with GST. Returns distance, hours, base fare and total.",
			Resource:    "routes",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				quote, err := routeapp.NewAgentRouteQueries(db).Quote(ctx, shared.TenantIDFromContext(ctx), in.From, in.To)
				if err != nil {
					return "", err
				}
				if quote == nil {
					return fmt.Sprintf("No route found between %s and %s. Suggest: 'search_routes' to see available routes.", in.From, in.To), nil
				}
				return jsonString(quote)
			},
		},
		{
			Name:        "create_booking",
			Description: "Create a new booking. Requires customer_id, route_id, pickup_date (YYYY-MM-DD HH:MM), vehicle_type, passengers and price.",
			Resource:    "bookings",
			Action:      "create",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				b, err := bookingapp.NewAgentBookingService(db).Create(ctx, shared.TenantIDFromContext(ctx), bookingapp.AgentCreateBookingInput{
					CustomerID: in.CustomerID, RouteID: in.RouteID, PickupDate: t.Format(time.RFC3339),
					VehicleType: strings.ToLower(in.VehicleType), Passengers: in.Passengers,
					Price: in.Price, Notes: in.Notes,
				})
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{
					"booking_id":     b.ID,
					"booking_number": b.Number,
					"status":         b.Status,
					"pickup_date":    b.PickupDate.Format(time.RFC3339),
					"price":          b.Price,
				})
			},
		},
		{
			Name:        "search_customers",
			Description: "Search customers by name, phone or company.",
			Resource:    "customers",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				customers, err := customerapp.NewAgentCustomerQueries(db).Search(ctx, shared.TenantIDFromContext(ctx), in.Query, 10)
				if err != nil {
					return "", err
				}
				if len(customers) == 0 {
					return "No customers found matching: " + in.Query, nil
				}
				return jsonString(customers)
			},
		},
		{
			Name:        "get_booking",
			Description: "Get booking details by booking number (e.g. BK-0001) or booking id.",
			Resource:    "bookings",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				b, err := bookingapp.NewAgentBookingService(db).GetByNumber(ctx, shared.TenantIDFromContext(ctx), in.Number)
				if err != nil {
					return "", fmt.Errorf("booking not found: %v", err)
				}
				return jsonString(map[string]any{
					"id":           b.ID,
					"number":       b.Number,
					"customer":     b.Customer,
					"route":        b.Route,
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
			Resource:    "trips",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				trips, err := tripapp.NewAgentTripService(db).List(ctx, shared.TenantIDFromContext(ctx), in.Query, in.Status, in.Limit)
				if err != nil {
					return "", err
				}
				if len(trips) == 0 {
					return "No trips found" + statusClause(in.Status), nil
				}
				return jsonString(trips)
			},
		},
		{
			Name:        "get_trip",
			Description: "Get trip details by trip number (e.g. TR-0001) or trip id.",
			Resource:    "trips",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				t, err := tripapp.NewAgentTripService(db).GetByNumber(ctx, shared.TenantIDFromContext(ctx), in.Number)
				if err != nil {
					return "", fmt.Errorf("trip not found: %v", err)
				}
				return jsonString(t)
			},
		},
		{
			Name:        "list_available_drivers",
			Description: "List drivers currently available for assignment.",
			Resource:    "drivers",
			Action:      "read",
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
			Resource:    "vehicles",
			Action:      "read",
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
			Resource:    "trips",
			Action:      "assign",
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
				if env.Dispatch != nil {
					err := env.Dispatch.AssignDriver(ctx, dispatchapp.AssignDriverCommand{
						TenantID: shared.TenantIDFromContext(ctx), ActorID: env.UserID,
						TripID: tripagg.TripID(in.TripID), DriverID: string(in.DriverID),
					})
					if err != nil {
						return "", err
					}
					return fmt.Sprintf("Driver %s assigned to trip %s.", in.DriverID, in.TripID), nil
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
			Resource:    "trips",
			Action:      "assign",
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
				if env.Dispatch != nil {
					err := env.Dispatch.AssignVehicle(ctx, dispatchapp.AssignVehicleCommand{
						TenantID: shared.TenantIDFromContext(ctx), ActorID: env.UserID,
						TripID: tripagg.TripID(in.TripID), VehicleID: string(in.VehicleID),
					})
					if err != nil {
						return "", err
					}
					return fmt.Sprintf("Vehicle %s assigned to trip %s.", in.VehicleID, in.TripID), nil
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
			Resource:    "invoices",
			Action:      "read",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				inv, err := invoiceapp.NewAgentInvoiceQueries(db).GetByNumber(ctx, shared.TenantIDFromContext(ctx), in.Number)
				if err != nil {
					return "", fmt.Errorf("invoice not found: %v", err)
				}
				return jsonString(map[string]any{"id": inv.ID, "number": inv.Number, "customer": inv.Customer, "total": inv.Total, "paid": inv.Paid, "outstanding": inv.Outstanding, "status": inv.Status, "due_date": inv.DueDate})
			},
		},
		{
			Name:        "list_unpaid_invoices",
			Description: "List invoices that are not fully paid (pending or partially paid).",
			Resource:    "invoices",
			Action:      "read",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				invs, err := invoiceapp.NewAgentInvoiceQueries(db).ListUnpaid(ctx, shared.TenantIDFromContext(ctx), 50)
				if err != nil {
					return "", err
				}
				if len(invs) == 0 {
					return "No unpaid invoices. All invoices are settled.", nil
				}
				return jsonString(invs)
			},
		},
		{
			Name:        "record_payment",
			Description: "Record a payment against an invoice. Requires invoice id, amount and method (cash, upi, bank_transfer, cheque).",
			Resource:    "payments",
			Action:      "create",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				tenant := shared.TenantIDFromContext(ctx)
				inv, err := invoiceapp.NewAgentInvoiceQueries(db).GetByID(ctx, tenant, in.InvoiceID)
				if err != nil {
					return "", fmt.Errorf("balance unavailable: %w", err)
				}
				if in.Amount > inv.Outstanding {
					return "", fmt.Errorf("payment amount %.2f exceeds outstanding balance %.2f; record only up to the outstanding amount", in.Amount, inv.Outstanding)
				}
				method := strings.ToLower(in.Method)
				pid, err := paymentapp.NewAgentMoneyService(db).RecordPayment(ctx, tenant, in.InvoiceID, in.Amount, method, in.Reference, in.Remarks)
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{"payment_id": pid, "invoice_id": in.InvoiceID, "amount": in.Amount, "method": method, "status": "recorded"})
			},
		},
		{
			Name:        "get_revenue",
			Description: "Get total revenue and monthly revenue figures.",
			Resource:    "reports",
			Action:      "read",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				total, monthly, err := paymentapp.NewAgentMoneyService(db).Revenue(ctx, shared.TenantIDFromContext(ctx))
				if err != nil {
					return "", fmt.Errorf("revenue unavailable: %w", err)
				}
				return jsonString(map[string]any{"total_revenue": total, "monthly": monthly})
			},
		},
		{
			Name:        "list_pending_kharcha",
			Description: "List driver expenses (kharcha) waiting for approval.",
			Resource:    "trips",
			Action:      "read",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				expenses, err := kharchaapp.NewAgentExpenseStore(db).ListPending(ctx, shared.TenantIDFromContext(ctx))
				if err != nil {
					return "", err
				}
				if len(expenses) == 0 {
					return "No pending kharcha expenses.", nil
				}
				return jsonString(expenses)
			},
		},
		{
			Name:        "approve_kharcha",
			Description: "Approve a pending driver expense (kharcha). Requires expense id.",
			Resource:    "trips",
			Action:      "update",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				if err := kharchaapp.NewAgentExpenseStore(db).Approve(ctx, shared.TenantIDFromContext(ctx), in.ExpenseID, actor); err != nil {
					return "", err
				}
				return fmt.Sprintf("Expense %s approved.", in.ExpenseID), nil
			},
		},
		{
			Name:        "reject_kharcha",
			Description: "Reject a pending driver expense (kharcha). Requires expense id and reason.",
			Resource:    "trips",
			Action:      "update",
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
				db, err := toolDB(env)
				if err != nil {
					return "", err
				}
				if err := kharchaapp.NewAgentExpenseStore(db).Reject(ctx, shared.TenantIDFromContext(ctx), in.ExpenseID, actor, in.Reason); err != nil {
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
			Resource:    "alerts",
			Action:      "read",
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
				// Tenant isolation + wire shape live in the owning alerts read
				// model (audit 2026-09-17 regression is pinned there and in
				// TestGetOpenAlerts_TenantIsolation below).
				alerts, err := alertsapp.NewOpenAlertQueries(db).List(ctx, string(shared.TenantIDFromContext(ctx)), in.Severity, 20)
				if err != nil {
					return "No alerts found: " + err.Error(), nil
				}
				if len(alerts) == 0 {
					return "No open alerts found.", nil
				}
				return jsonString(alerts)
			},
		},
		{
			Name:        "extend_ewaybill",
			Description: "Request e-way bill extension for a trip. Requires trip id. Subject to geofence evidence gate.",
			Resource:    "trips",
			Action:      "update",
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
				rec, err := svc.GetByTripForTenant(ctx, in.TripID, string(shared.TenantIDFromContext(ctx)))
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
		{
			Name:        "list_work_orders",
			Description: "List maintenance job cards (work orders) for the org, optionally filtered by status (open, assigned, in_progress, on_hold, done, cancelled).",
			Resource:    "maintenance",
			Action:      "read",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": optStr("optional status filter"),
					"limit":  map[string]any{"type": "integer", "description": "max results (default 20)"},
				},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					Status string `json:"status"`
					Limit  int    `json:"limit"`
				}
				if len(args) > 0 {
					_ = json.Unmarshal(args, &in)
				}
				if in.Limit <= 0 {
					in.Limit = 20
				}
				if in.Limit > 50 {
					in.Limit = 50
				}
				facade, err := workOrderFacade(env)
				if err != nil {
					return "", err
				}
				orders, err := facade.List(ctx, in.Status, in.Limit)
				if err != nil {
					return "", err
				}
				type row struct {
					ID       string `json:"id"`
					Title    string `json:"title"`
					Vehicle  string `json:"vehicle_id"`
					Assignee string `json:"assignee"`
					Status   string `json:"status"`
				}
				out := make([]row, 0, len(orders))
				for _, w := range orders {
					out = append(out, row{w.ID, w.Title, w.VehicleID, w.Assignee, w.Status})
				}
				if len(out) == 0 {
					return "No job cards found" + statusClause(in.Status), nil
				}
				return jsonString(out)
			},
		},
		{
			Name:        "get_work_order",
			Description: "Get one maintenance job card by id: status, vehicle, assignee, costs, due/closed dates.",
			Resource:    "maintenance",
			Action:      "read",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": optStr("job card id"),
				},
				"required": []string{"id"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				facade, err := workOrderFacade(env)
				if err != nil {
					return "", err
				}
				wo, err := facade.Get(ctx, in.ID)
				if err != nil {
					return "", err
				}
				if wo == nil {
					return "", fmt.Errorf("job card %s not found", in.ID)
				}
				return jsonString(workOrderJSON(wo))
			},
		},
		{
			Name:        "create_work_order",
			Description: "Open a maintenance job card in 'open' status. Requires vehicle_id and title. Never invent vehicle ids — confirm the vehicle first.",
			Resource:    "maintenance",
			Action:      "create",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"vehicle_id":    optStr("vehicle id (veh_...)"),
					"title":         optStr("short work description, e.g. 'Brake pad replacement'"),
					"description":   optStr("optional symptoms, findings, parts needed"),
					"assignee":      optStr("optional mechanic name"),
					"vendor":        optStr("optional garage / vendor"),
					"cost_estimate": map[string]any{"type": "number", "description": "optional estimated cost in INR"},
					"schedule_id":   optStr("optional linked preventive-maintenance schedule id"),
					"due_at":        optStr("optional due date RFC3339, e.g. '2026-09-10T00:00:00Z'"),
				},
				"required": []string{"vehicle_id", "title"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					VehicleID    string   `json:"vehicle_id"`
					Title        string   `json:"title"`
					Description  string   `json:"description"`
					Assignee     string   `json:"assignee"`
					Vendor       string   `json:"vendor"`
					CostEstimate *float64 `json:"cost_estimate"`
					ScheduleID   *string  `json:"schedule_id"`
					DueAt        string   `json:"due_at"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				if strings.TrimSpace(in.VehicleID) == "" || strings.TrimSpace(in.Title) == "" {
					return "", fmt.Errorf("vehicle_id and title are required")
				}
				input := maintenance.CreateWorkOrderInput{
					VehicleID:    strings.TrimSpace(in.VehicleID),
					Title:        strings.TrimSpace(in.Title),
					Description:  in.Description,
					Assignee:     in.Assignee,
					Vendor:       in.Vendor,
					CostEstimate: in.CostEstimate,
					ScheduleID:   in.ScheduleID,
				}
				if in.DueAt != "" {
					due, err := time.Parse(time.RFC3339, in.DueAt)
					if err != nil {
						return "", fmt.Errorf("due_at must be RFC3339, got %q", in.DueAt)
					}
					dueUTC := due.UTC()
					input.DueAt = &dueUTC
				}
				facade, err := workOrderFacade(env)
				if err != nil {
					return "", err
				}
				wo, err := facade.Create(ctx, input)
				if err != nil {
					return "", err
				}
				return jsonString(map[string]any{
					"id":     wo.ID,
					"title":  wo.Title,
					"status": wo.Status,
				})
			},
		},
		{
			Name:        "transition_work_order",
			Description: "Move a job card along open → assigned → in_progress → done (on_hold loops back, cancelled closes). Closing as done also writes the service record. Requires admin approval.",
			Resource:    "maintenance",
			Action:      "update",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":       optStr("job card id"),
					"status":   optStr("open, assigned, in_progress, on_hold, done or cancelled"),
					"assignee": optStr("optional mechanic name to set first"),
					"vendor":   optStr("optional garage / vendor to set first"),
				},
				"required": []string{"id", "status"},
			},
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var in struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Assignee string `json:"assignee"`
					Vendor   string `json:"vendor"`
				}
				if err := json.Unmarshal(args, &in); err != nil {
					return "", err
				}
				switch in.Status {
				case maintdomain.WorkOrderOpen, maintdomain.WorkOrderAssigned, maintdomain.WorkOrderInProgress,
					maintdomain.WorkOrderOnHold, maintdomain.WorkOrderDone, maintdomain.WorkOrderCancelled:
				default:
					return "", fmt.Errorf("unknown job card status %q", in.Status)
				}
				facade, err := workOrderFacade(env)
				if err != nil {
					return "", err
				}
				actor := userIDFrom(ctx)
				if actor == "" {
					actor = env.UserID
				}
				result, err := facade.Transition(ctx, maintenance.TransitionWorkOrderInput{
					ID: in.ID, Status: in.Status, Assignee: in.Assignee, Vendor: in.Vendor, Actor: actor,
				})
				if err != nil {
					return "", err
				}
				if result.Completed {
					return fmt.Sprintf("Job card %s closed as done; service record written.", result.ID), nil
				}
				return fmt.Sprintf("Job card %s moved to %s.", in.ID, in.Status), nil
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

// workOrderFacade binds the maintenance application facade to the service DB.
// Tools fail closed when the DB is unavailable (legacy model-facing string).
func workOrderFacade(env *ToolEnv) (*maintenance.WorkOrderFacade, error) {
	if env == nil || env.Services == nil {
		return nil, maintenance.ErrStoreUnavailable
	}
	f := maintenance.NewWorkOrderFacade(env.Services.DB())
	if f == nil {
		return nil, maintenance.ErrStoreUnavailable
	}
	return f, nil
}

// toolDB returns the service database for the domain application facades.
// Tools fail closed when it is unavailable.
func toolDB(env *ToolEnv) (*sql.DB, error) {
	if env == nil || env.Services == nil || env.Services.DB() == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	return env.Services.DB(), nil
}

// workOrderJSON renders one job card for the assistant.
func workOrderJSON(w *maintdomain.WorkOrder) map[string]any {
	out := map[string]any{
		"id":       w.ID,
		"title":    w.Title,
		"vehicle":  w.VehicleID,
		"assignee": w.Assignee,
		"vendor":   w.Vendor,
		"status":   w.Status,
		"created":  w.CreatedAt.Format(time.RFC3339),
		"terminal": w.Terminal(),
	}
	if w.Description != "" {
		out["description"] = w.Description
	}
	if w.CostEstimate != nil {
		out["cost_estimate"] = *w.CostEstimate
	}
	if w.CostActual != nil {
		out["cost_actual"] = *w.CostActual
	}
	if w.DueAt != nil {
		out["due_at"] = w.DueAt.Format(time.RFC3339)
	}
	if w.ClosedAt != nil {
		out["closed_at"] = w.ClosedAt.Format(time.RFC3339)
	}
	return out
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
