package handlers

// RBAC registry ratchet: every permission string referenced by a route guard
// or sidebar guard MUST exist as a row in `permissions` at migration head,
// and the core roles MUST be granted what the UI promises them.
//
// History: /ewaybill guarded on `ewaybill:read` which existed nowhere (403
// for every role); org_admin 403'd on pages the sidebar showed it
// (errors, features); post-00064 migrations kept forgetting role 6.
// These tests make that class of rot fail loudly at `go test` time.

import (
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"transport-app/internal/auth"
)

// collectGuardPermissions extracts every resource:action pair referenced by
// route guards and sidebar `can` checks. Deliberately exhaustive, not
// curated: adding a guard without seeding its permission must break the build.
// (Regexes are func-local: repo CI forbids package-level vars in _test.go.)
func collectGuardPermissions(t *testing.T) map[string]struct{} {
	t.Helper()
	guardCallRe := regexp.MustCompile(`(?:ResourcePermission|RequirePermission)\(\s*[A-Za-z0-9_.]+,\s*"([^"]+)",\s*"([^"]+)"\s*\)`)
	sidebarCanRe := regexp.MustCompile(`\{\{\s*if\s+can\s+\.User\s+"([^"]+)"\s+"([^"]+)"`)
	roots := []string{".", "../integration", "../../cmd/server"}
	out := map[string]struct{}{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(root, e.Name()))
			require.NoError(t, err)
			for _, m := range guardCallRe.FindAllSubmatch(src, -1) {
				out[string(m[1])+":"+string(m[2])] = struct{}{}
			}
		}
	}
	// Sidebar guards live in templates, not Go.
	layout, err := os.ReadFile("../templates/layout.html")
	require.NoError(t, err)
	for _, m := range sidebarCanRe.FindAllSubmatch(layout, -1) {
		out[string(m[1])+":"+string(m[2])] = struct{}{}
	}
	require.NotEmpty(t, out, "registry scan found zero guard permissions — scan is broken, not the code")
	return out
}

func permissionRows(t *testing.T) map[string]struct{} {
	t.Helper()
	db := newCustomersSelectedDB(t)
	rows, err := db.Query(`SELECT name FROM permissions`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		out[name] = struct{}{}
	}
	require.NoError(t, rows.Err())
	return out
}

func TestRouteGuardPermissions_ExistInDB(t *testing.T) {
	seeded := permissionRows(t)
	var missing []string
	for perm := range collectGuardPermissions(t) {
		if _, ok := seeded[perm]; !ok {
			missing = append(missing, perm)
		}
	}
	require.Empty(t, missing,
		"route/sidebar guards reference permissions with no `permissions` row — seed them in a migration: %v", missing)
}

// TestOrgAdmin_RBACGrants pins the owner-role contract against the REAL
// Casbin enforcer on a head-migrated DB: what the UI shows org_admin it
// must be able to open; platform-only controls must stay denied.
func TestOrgAdmin_RBACGrants(t *testing.T) {
	db := newCustomersSelectedDB(t)
	// Seed users + role mappings (Casbin joins user_roles→users, so phantom
	// user_ids alone are invisible to the enforcer).
	_, err := db.Exec(`INSERT OR IGNORE INTO users
		(id, email, password_hash, name, role_id, status, created_at, updated_at, timezone, theme_preference, tenant_id, auth_provider)
		VALUES ('u-admin','admin@t.local','x','Admin',1,'active',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'Asia/Kolkata','light','1','local'),
		       ('u-disp','disp@t.local','x','Disp',2,'active',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'Asia/Kolkata','light','1','local'),
		       ('u-org','org@t.local','x','Org',6,'active',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,'Asia/Kolkata','light','1','local')`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT OR IGNORE INTO user_roles (user_id, role_id) VALUES
		('u-admin', 1), ('u-disp', 2), ('u-org', 6)`)
	require.NoError(t, err)
	authSvc, err := auth.NewCasbinAuthorizationService(db)
	require.NoError(t, err)

	mustAllow := map[string][][2]string{
		"u-admin": {
			{"users", "manage"}, {"features", "update"}, {"tenants", "manage"},
		},
		"u-org": {
			{"trips", "read"}, {"bookings", "read"}, {"invoices", "read"},
			{"settings", "update"}, {"users", "read"},
			// Previously 403'd for org_admin (fixed by 00150):
			{"errors", "read"}, {"errors", "update"},
			{"ewaybill", "read"}, {"ewaybill", "create"}, {"ewaybill", "update"},
			{"ewaybill", "write"}, {"esg", "read"}, {"esg", "write"},
			{"fuel", "write"}, {"fuel", "create"}, {"kharcha", "approve"},
			{"alerts", "write"}, {"rag", "read"}, {"dashboard", "read"},
			{"fastag", "read"}, {"fastag", "update"}, {"trips", "cancel"},
			{"scorecard", "update"}, {"accounting", "read"}, {"accounting", "sync"},
			{"integrations", "accounting"}, {"integrations", "gstn"},
		},
		"u-disp": {
			{"trips", "read"}, {"ewaybill", "read"}, {"ewaybill", "create"},
			{"errors", "read"}, {"esg", "read"}, {"trips", "cancel"},
			{"fastag", "read"}, {"fuel", "create"}, {"integrations", "gstn"},
		},
	}
	for user, pairs := range mustAllow {
		for _, p := range pairs {
			require.True(t, authSvc.Can(user, p[0], p[1]),
				"user %s must be allowed %s:%s", user, p[0], p[1])
		}
	}

	mustDeny := map[string][][2]string{
		// Platform-only by design (00064 + settings.go comment):
		"u-org": {
			{"founder", "read"}, {"experiments", "read"}, {"experiments", "write"},
			{"features", "update"}, {"tenants", "manage"}, {"users", "manage"},
			// Other roles' surfaces:
			{"customer_portal", "read"},
		},
		// Dispatcher approves no money and manages no corpus:
		"u-disp": {
			{"kharcha", "approve"}, {"rag", "write"},
			{"founder", "read"}, {"features", "update"},
		},
	}
	for user, pairs := range mustDeny {
		for _, p := range pairs {
			require.False(t, authSvc.Can(user, p[0], p[1]),
				"user %s must be denied %s:%s", user, p[0], p[1])
		}
	}
}

// TestMigration00150_DownUp proves the backfill applies and rolls back
// (Prove-It protocol: migrations must do both).
func TestMigration00150_DownUp(t *testing.T) {
	db := newCustomersSelectedDB(t)
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM permissions WHERE name = 'ewaybill:read'`).Scan(&n))
	require.Equal(t, 1, n, "00150 must seed ewaybill:read on migrate up")
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id
		  WHERE rp.role_id = 6 AND p.name = 'errors:read'`).Scan(&n))
	require.Equal(t, 1, n, "00150 must grant errors:read to org_admin on migrate up")

	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.Down(db, "../../db/migrations"))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM permissions WHERE name = 'ewaybill:read'`).Scan(&n))
	require.Equal(t, 0, n, "00150 down must remove the ewaybill rows")
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id
		  WHERE rp.role_id = 6 AND p.name = 'errors:read'`).Scan(&n))
	require.Equal(t, 0, n, "00150 down must remove the org_admin backfill")

	require.NoError(t, goose.Up(db, "../../db/migrations"))
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM permissions WHERE name = 'ewaybill:read'`).Scan(&n))
	require.Equal(t, 1, n, "00150 must re-apply cleanly after down")
}

// TestSidebarFeaturesLink_VisibilityByRole renders the real sidebar shell
// per role: the Features toggle page requires features:update, so its nav
// link must be hidden from org_admin (was: visible link → guaranteed 403).
func TestSidebarFeaturesLink_VisibilityByRole(t *testing.T) {
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "handlers" {
		t.Chdir("../..")
	}
	render := func(t *testing.T, svc auth.AuthorizationService, userID string) string {
		t.Helper()
		tmpl, err := parseTemplates(svc)
		require.NoError(t, err)
		var buf strings.Builder
		data := map[string]interface{}{
			"Title":    "Settings",
			"Version":  "test",
			"Content":  template.HTML(""),
			"Features": map[string]bool{},
			"User":     &auth.SessionData{UserID: userID, Role: "org_admin"},
		}
		require.NoError(t, tmpl.ExecuteTemplate(&buf, "layout.html", data))
		return buf.String()
	}

	orgOnly := &mockAuthSvc{allowed: map[string]bool{
		"u-org:settings:update": true,
		"u-org:settings:read":   true,
	}}
	out := render(t, orgOnly, "u-org")
	require.NotContains(t, out, `href="/settings/features"`,
		"org_admin without features:update must not see the Features nav link")
	require.Contains(t, out, `href="/settings"`,
		"sanity: org_admin still sees the Settings link")

	adminOut := render(t, &mockAuthSvc{}, "u-admin")
	require.Contains(t, adminOut, `href="/settings/features"`,
		"admin (allow-all) still sees the Features nav link")
}

// TestDashboardTabs_Wired pins the Today/Trends split: both tab buttons and
// all eight section anchors must exist with the right data-dashtab side.
// (The toggle itself is JS, verified live; this fails the build if the
// wiring rots.)
func TestDashboardTabs_Wired(t *testing.T) {
	src, err := os.ReadFile("../templates/dashboard.html")
	require.NoError(t, err)
	body := string(src)
	require.Contains(t, body, `data-dashtab-btn="today"`)
	require.Contains(t, body, `data-dashtab-btn="trends"`)
	today := []string{"dash-sec-kpis", "dash-sec-strip", "dash-sec-attention", "dash-sec-alerts", "dash-sec-upcoming"}
	trends := []string{"dash-sec-charts", "dash-sec-recents", "dash-sec-activity"}
	for _, id := range today {
		require.Contains(t, body, `id="`+id+`" data-dashtab="today"`, "section %s must sit on the Today tab", id)
	}
	for _, id := range trends {
		require.Contains(t, body, `id="`+id+`" data-dashtab="trends"`, "section %s must sit on the Trends tab", id)
	}
}
