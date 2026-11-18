package rl

import (
	"path/filepath"
	"testing"
)

func testService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	return svc
}

func TestEpisodeRecordingAndRewards(t *testing.T) {
	svc := testService(t)

	ep := &Episode{
		ID:        svc.NewEpisodeID(),
		TenantID:  "1",
		UserID:    "usr-1",
		AgentName: "ops",
		Query:     "how many trips today",
		Answer:    "5 trips today",
		TurnCount: 2,
		Traces: []ToolTrace{
			{Name: "get_dashboard", OK: true},
			{Name: "list_trips", OK: true},
		},
	}
	if err := svc.RecordEpisode(ep); err != nil {
		t.Fatal(err)
	}

	// 2 successful tools (+0.3) + 2 turns efficient (+0.2) = 0.5
	examples := svc.ExamplesFor("1", "ops", "trips today", 3)
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}
	if examples[0].Reward < 0.49 {
		t.Errorf("expected reward ~0.5, got %v", examples[0].Reward)
	}
}

func TestToolFailurePenalty(t *testing.T) {
	svc := testService(t)

	ep := &Episode{
		ID:        svc.NewEpisodeID(),
		AgentName: "booking",
		Query:     "quote mumbai pune",
		Answer:    "no route",
		Traces: []ToolTrace{
			{Name: "get_quote", OK: false, Error: "no route found"},
			{Name: "get_quote", OK: false, Error: "no route found"},
			{Name: "get_quote", OK: false, Error: "no route found"},
			{Name: "search_routes", OK: true},
		},
	}
	if err := svc.RecordEpisode(ep); err != nil {
		t.Fatal(err)
	}

	stats, err := svc.store.ToolStats("booking")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range stats {
		if st.Tool == "get_quote" && st.FailureRate != 1.0 {
			t.Errorf("expected get_quote failure rate 1.0, got %v", st.FailureRate)
		}
	}

	notes := svc.PolicyNotesFor("booking")
	found := false
	for _, n := range notes {
		if n.Tool == "get_quote" {
			found = true
		}
	}
	if !found {
		t.Error("expected policy note for failing tool")
	}
}

func TestActionApprovalFlow(t *testing.T) {
	svc := testService(t)

	ep := &Episode{ID: svc.NewEpisodeID(), AgentName: "booking", Query: "book", Answer: "ok"}
	if err := svc.RecordEpisode(ep); err != nil {
		t.Fatal(err)
	}

	a := &Action{
		EpisodeID:   ep.ID,
		ToolName:    "create_booking",
		ArgsJSON:    `{"price":100}`,
		Summary:     "create_booking: price=100",
		RequestedBy: "Dispatcher",
	}
	if err := svc.SubmitAction(a); err != nil {
		t.Fatal(err)
	}

	pending, err := svc.ListPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Status != ActionPending {
		t.Fatalf("expected 1 pending action, got %+v", pending)
	}

	// approve path: claim (pending -> approved) then finalize (approved -> executed)
	if claimed, err := svc.ClaimAction(a.ID, "Admin", "2024-01-01T00:00:00Z"); err != nil || !claimed {
		t.Fatalf("claim failed: claimed=%v err=%v", claimed, err)
	}
	a2, err := svc.GetAction(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	a2.Status = ActionExecuted
	a2.DecidedBy = "Admin"
	if err := svc.UpdateActionDecision(a2, ActionApproved); err != nil {
		t.Fatal(err)
	}

	// reward signal: +1.0 for executed action
	got, err := svc.store.GetAction(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != ActionExecuted {
		t.Errorf("expected executed, got %s", got.Status)
	}

	// rejected path
	a3 := &Action{
		EpisodeID: ep.ID,
		ToolName:  "record_payment",
		ArgsJSON:  `{}`,
		Summary:   "record_payment",
	}
	if err := svc.SubmitAction(a3); err != nil {
		t.Fatal(err)
	}
	got3, _ := svc.GetAction(a3.ID)
	got3.Status = ActionRejected
	if _, err := svc.ClaimAction(a3.ID, "Admin", "2024-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateActionDecision(got3, ActionApproved); err != nil {
		t.Fatal(err)
	}
}

func TestExamplesRelevanceRanking(t *testing.T) {
	episodes := []Episode{
		{Query: "quote mumbai to pune price", Answer: "₹4500", Reward: 2.0, Traces: []ToolTrace{{Name: "get_quote", OK: true}}},
		{Query: "how many trucks available", Answer: "3", Reward: 1.5},
		{Query: "unpaid invoices list", Answer: "INV-2", Reward: 3.0},
	}
	ex := ExamplesFor(episodes, "quote for mumbai pune", 2)
	if len(ex) != 2 {
		t.Fatalf("expected 2 examples, got %d", len(ex))
	}
	if ex[0].Query != "quote mumbai to pune price" {
		t.Errorf("expected most relevant first, got %q", ex[0].Query)
	}
}

func TestPolicyNotesThreshold(t *testing.T) {
	stats := []ToolStat{
		{Tool: "flaky", Calls: 5, Failures: 3, FailureRate: 0.6},
		{Tool: "reliable", Calls: 5, Failures: 1, FailureRate: 0.2},
		{Tool: "untested", Calls: 2, Failures: 2, FailureRate: 1.0},
	}
	notes := PolicyNotes(stats)
	if len(notes) != 1 || notes[0].Tool != "flaky" {
		t.Errorf("expected only flaky flagged, got %+v", notes)
	}
}
