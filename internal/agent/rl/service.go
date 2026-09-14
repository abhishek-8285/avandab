package rl

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Service bundles the online RL loop: episode recording, reward signals,
// preference memory (few-shot) and policy shaping (tool deprioritization).
type Service struct {
	store *Store
}

// New opens the RL database and returns the learning service.
func New(dbPath string) (*Service, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	return &Service{store: store}, nil
}

func (s *Service) Close() error { return s.store.Close() }

// NewEpisodeID generates an episode id up-front (so actions can reference it).
func (s *Service) NewEpisodeID() string { return s.store.NewID("ep-") }

// RecordEpisode stores a finished episode and applies implicit trajectory rewards.
// The whole sequence (episode row, rewards, tool stats, finalization) commits
// atomically; a failure can never leave a half-recorded episode behind.
func (s *Service) RecordEpisode(e *Episode) error {
	rewards := TrajectoryRewards(e.Traces)
	rewards = append(rewards, TurnEfficiencyReward(e.TurnCount)...)
	if strings.TrimSpace(e.Answer) == "" {
		rewards = append(rewards, RewardSignal{Signal: SignalAnswerEmpty, Value: RewardAnswerEmpty, Note: "empty answer"})
	}
	stats := make([]ToolCallStat, 0, len(e.Traces))
	for _, t := range e.Traces {
		stats = append(stats, ToolCallStat{ToolName: t.Name, OK: t.OK})
	}
	return s.store.RecordEpisodeTx(e, rewards, stats)
}

// SignalAction records the admin decision reward for a mutating action.
func (s *Service) SignalAction(episodeID string, status ActionStatus) error {
	switch status {
	case ActionExecuted:
		return s.store.AddReward(episodeID, SignalActionExecuted, "admin approved and executed", RewardActionExecuted)
	case ActionFailed:
		return s.store.AddReward(episodeID, SignalActionFailed, "approved but execution failed", RewardActionFailed)
	case ActionRejected:
		return s.store.AddReward(episodeID, SignalActionRejected, "admin rejected", RewardActionRejected)
	}
	return nil
}

// SubmitAction records a pending mutating action tied to an episode.
func (s *Service) SubmitAction(a *Action) error {
	return s.store.CreateAction(a)
}

// GetAction loads an action.
func (s *Service) GetAction(id string) (*Action, error) {
	return s.store.GetAction(id)
}

// ClaimAction atomically claims a pending action for execution (pending -> approved).
func (s *Service) ClaimAction(id, decidedBy, decidedAt string) (bool, error) {
	return s.store.ClaimAction(id, decidedBy, decidedAt)
}

// ListActions lists actions, optionally filtered by status.
func (s *Service) ListActions(status ActionStatus, limit int) ([]Action, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.store.ListActions(status, limit)
}

// ListPendingActions is a convenience for the approval queue.
func (s *Service) ListPendingActions() ([]Action, error) {
	return s.store.ListActions(ActionPending, 100)
}

// UpdateActionDecision persists an admin decision and applies its reward.
func (s *Service) UpdateActionDecision(a *Action, expect ActionStatus) error {
	applied, err := s.store.UpdateActionDecision(a, expect)
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("action %s is not %s anymore (concurrent decision?)", a.ID, expect)
	}
	if a.EpisodeID != "" {
		return s.SignalAction(a.EpisodeID, a.Status)
	}
	return nil
}

// ExamplesFor retrieves similar high-reward episodes as few-shot exemplars.
func (s *Service) ExamplesFor(agentName, query string, k int) []Example {
	episodes, err := s.store.TopEpisodes(agentName, 0.5, 50)
	if err != nil {
		return nil
	}
	return ExamplesFor(episodes, query, k)
}

// PolicyNotesFor returns tools the policy wants deprioritized for an agent.
func (s *Service) PolicyNotesFor(agentName string) []PolicyNote {
	stats, err := s.store.ToolStats(agentName)
	if err != nil {
		return nil
	}
	return PolicyNotes(stats)
}

// Stats returns learning stats for reporting.
func (s *Service) Stats() (map[string]any, error) {
	var episodes int
	if err := s.store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_episodes`).Scan(&episodes); err != nil {
		return nil, err
	}
	var actions int
	if err := s.store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_actions`).Scan(&actions); err != nil {
		return nil, err
	}
	var pending int
	if err := s.store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_actions WHERE status = 'pending'`).Scan(&pending); err != nil {
		return nil, err
	}
	var totalReward float64
	if err := s.store.db.QueryRowContext(context.Background(), `SELECT COALESCE(SUM(reward), 0) FROM agent_episodes`).Scan(&totalReward); err != nil {
		return nil, err
	}
	return map[string]any{
		"episodes":        episodes,
		"actions":         actions,
		"pending_actions": pending,
		"total_reward":    totalReward,
	}, nil
}

// MarshalMessages renders chat messages for storage.
func MarshalMessages(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
