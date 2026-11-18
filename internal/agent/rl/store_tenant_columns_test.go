package rl

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenStoreAddsMissingActionColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agent_actions (
		id TEXT PRIMARY KEY, episode_id TEXT, tool_name TEXT, args_json TEXT,
		summary TEXT, status TEXT, requested_by TEXT, decided_by TEXT,
		decided_at TEXT, result TEXT, error TEXT, created_at TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_actions (id, episode_id, tool_name, args_json, summary, status, requested_by, decided_by, decided_at, result, error, created_at)
		VALUES ('act-old', '', 'create_booking', '{}', 'old', 'pending', '', '', '', '', '', '2024-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	svc, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	action, err := svc.GetAction("act-old")
	if err != nil {
		t.Fatalf("old action unreadable after column add: %v", err)
	}
	if action.TenantID != "" || action.Resource != "" || action.Action != "" {
		t.Fatalf("old action got stray scope values: %+v", action)
	}
	if err := svc.SubmitAction(&Action{
		EpisodeID: "ep-1", ToolName: "record_payment", ArgsJSON: `{}`, Summary: "new",
		TenantID: "tenant-a", Resource: "payments", Action: "create",
	}); err != nil {
		t.Fatalf("new action with scope columns: %v", err)
	}
	actions, err := svc.ListActions("", 10)
	if err != nil {
		t.Fatal(err)
	}
	var foundScope bool
	for _, a := range actions {
		if strings.Contains(a.Summary, "new") {
			foundScope = a.TenantID == "tenant-a" && a.Resource == "payments" && a.Action == "create"
		}
	}
	if !foundScope {
		t.Fatalf("scope columns not persisted/loaded: %+v", actions)
	}
}
