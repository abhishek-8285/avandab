package rl

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A process killed between ClaimAction and UpdateActionDecision leaves an
// action 'approved' with an unknown outcome — the mutation may or may not
// have committed. Startup must NOT requeue it, or reclaiming would execute
// the mutation twice. It becomes terminal with a diagnosis; a human verifies
// and submits a fresh action instead. Fresh in-flight claims are untouched.
func TestNewQuarantinesStaleApprovedActions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rl.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}

	stale := &Action{ToolName: "create_booking", ArgsJSON: `{}`, Summary: "crashed mid-handler", RequestedBy: "u", TenantID: "t"}
	fresh := &Action{ToolName: "create_booking", ArgsJSON: `{}`, Summary: "still running", RequestedBy: "u", TenantID: "t"}
	for _, a := range []*Action{stale, fresh} {
		if err := store.CreateAction(a); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := store.ClaimAction(stale.ID, "admin", time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)); err != nil || !ok {
		t.Fatalf("claim stale: ok=%v err=%v", ok, err)
	}
	if ok, err := store.ClaimAction(fresh.ID, "admin", time.Now().UTC().Format(time.RFC3339)); err != nil || !ok {
		t.Fatalf("claim fresh: ok=%v err=%v", ok, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Fresh process startup.
	svc, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()

	gotStale, err := svc.GetAction(stale.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotStale.Status != ActionFailed {
		t.Fatalf("stale action status = %s, want failed (outcome unknown)", gotStale.Status)
	}
	if gotStale.Error == "" || !strings.Contains(gotStale.Error, "outcome unknown") {
		t.Fatalf("stale action error = %q, want outcome-unknown diagnosis", gotStale.Error)
	}
	if ok, err := svc.ClaimAction(stale.ID, "admin-2", time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("claim quarantined action: %v", err)
	} else if ok {
		t.Fatal("quarantined action must never be re-claimable: mutation may already have committed")
	}

	gotFresh, err := svc.GetAction(fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotFresh.Status != ActionApproved {
		t.Fatalf("fresh in-flight action status = %s, want approved", gotFresh.Status)
	}
}
