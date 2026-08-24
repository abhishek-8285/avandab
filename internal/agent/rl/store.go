package rl

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ActionStatus tracks the approval lifecycle of a mutating agent action.
type ActionStatus string

const (
	ActionPending  ActionStatus = "pending"
	ActionApproved ActionStatus = "approved"
	ActionRejected ActionStatus = "rejected"
	ActionExecuted ActionStatus = "executed"
	ActionFailed   ActionStatus = "failed"
)

// EpisodeStatus tracks learning lifecycle.
type EpisodeStatus string

const (
	EpisodePending  EpisodeStatus = "pending"
	EpisodeRewarded EpisodeStatus = "rewarded"
)

// ToolTrace is one tool invocation inside an episode.
type ToolTrace struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Duration int64  `json:"duration_ms"`
	Error    string `json:"error,omitempty"`
}

// Episode is a full agent conversation recorded for learning.
type Episode struct {
	ID        string      `json:"id"`
	UserID    string      `json:"user_id"`
	AgentName string      `json:"agent_name"`
	Query     string      `json:"query"`
	Answer    string      `json:"answer"`
	Reward    float64     `json:"reward"`
	TurnCount int         `json:"turn_count"`
	Messages  string      `json:"messages_json"`
	Traces    []ToolTrace `json:"traces"`
	CreatedAt string      `json:"created_at"`
}

// Action is a mutating agent action awaiting (or having received) admin decision.
type Action struct {
	ID          string       `json:"id"`
	EpisodeID   string       `json:"episode_id"`
	ToolName    string       `json:"tool_name"`
	ArgsJSON    string       `json:"args_json"`
	Summary     string       `json:"summary"`
	Status      ActionStatus `json:"status"`
	RequestedBy string       `json:"requested_by"`
	DecidedBy   string       `json:"decided_by"`
	DecidedAt   string       `json:"decided_at"`
	Result      string       `json:"result"`
	Error       string       `json:"error"`
	CreatedAt   string       `json:"created_at"`
}

// RewardSignal is one reward contribution to an episode.
type RewardSignal struct {
	ID        string  `json:"id"`
	EpisodeID string  `json:"episode_id"`
	Signal    string  `json:"signal"`
	Value     float64 `json:"value"`
	Note      string  `json:"note"`
	CreatedAt string  `json:"created_at"`
}

// Store persists episodes, actions, reward signals and tool stats in SQLite.
type Store struct {
	db *sql.DB
}

// OpenStore opens (creating if needed) the RL database.
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS agent_episodes (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			agent_name TEXT,
			query TEXT,
			answer TEXT,
			reward REAL DEFAULT 0,
			status TEXT DEFAULT 'pending',
			turn_count INTEGER DEFAULT 0,
			messages_json TEXT,
			traces_json TEXT,
			created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS agent_actions (
			id TEXT PRIMARY KEY,
			episode_id TEXT,
			tool_name TEXT,
			args_json TEXT,
			summary TEXT,
			status TEXT DEFAULT 'pending',
			requested_by TEXT,
			decided_by TEXT,
			decided_at TEXT,
			result TEXT,
			error TEXT,
			created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS agent_rewards (
			id TEXT PRIMARY KEY,
			episode_id TEXT,
			signal TEXT,
			value REAL,
			note TEXT,
			created_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS agent_tool_stats (
			agent_name TEXT,
			tool_name TEXT,
			calls INTEGER DEFAULT 0,
			failures INTEGER DEFAULT 0,
			PRIMARY KEY (agent_name, tool_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_episodes_agent_reward ON agent_episodes(agent_name, reward)`,
		`CREATE INDEX IF NOT EXISTS idx_actions_status ON agent_actions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_rewards_episode ON agent_rewards(episode_id)`,
	}
	for _, st := range stmts {
		if _, err := db.Exec(st); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func newID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return prefix + fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return prefix + hex.EncodeToString(b)
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// SaveEpisode stores a new episode (status pending).
func (s *Store) SaveEpisode(e *Episode) error {
	traces, err := jsonMarshal(e.Traces)
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = newID("ep-")
	}
	if e.CreatedAt == "" {
		e.CreatedAt = now()
	}
	_, err = s.db.Exec(`INSERT INTO agent_episodes (id, user_id, agent_name, query, answer, reward, status, turn_count, messages_json, traces_json, created_at)
		VALUES (?, ?, ?, ?, ?, 0, 'pending', ?, ?, ?, ?)`,
		e.ID, e.UserID, e.AgentName, e.Query, e.Answer, e.TurnCount, e.Messages, traces, e.CreatedAt)
	return err
}

// RecordEpisodeTx stores an episode and all its learning artifacts atomically:
// the episode row, trajectory rewards, tool-call stats and the finalization
// all commit together, so a mid-way failure can never leave a half-recorded
// episode stuck in 'pending' with partial rewards.
func (s *Store) RecordEpisodeTx(e *Episode, rewards []RewardSignal, toolCalls []ToolCallStat) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	traces, err := jsonMarshal(e.Traces)
	if err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = newID("ep-")
	}
	if e.CreatedAt == "" {
		e.CreatedAt = now()
	}
	if _, err := tx.Exec(`INSERT INTO agent_episodes (id, user_id, agent_name, query, answer, reward, status, turn_count, messages_json, traces_json, created_at)
		VALUES (?, ?, ?, ?, ?, 0, 'pending', ?, ?, ?, ?)`,
		e.ID, e.UserID, e.AgentName, e.Query, e.Answer, e.TurnCount, e.Messages, traces, e.CreatedAt); err != nil {
		return err
	}
	for _, r := range rewards {
		if _, err := tx.Exec(`INSERT INTO agent_rewards (id, episode_id, signal, value, note, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			newID("rw-"), e.ID, r.Signal, r.Value, r.Note, now()); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE agent_episodes SET reward = reward + ? WHERE id = ?`, r.Value, e.ID); err != nil {
			return err
		}
	}
	for _, t := range toolCalls {
		if _, err := tx.Exec(`INSERT INTO agent_tool_stats (agent_name, tool_name, calls, failures) VALUES (?, ?, 1, ?)
			ON CONFLICT(agent_name, tool_name) DO UPDATE SET calls = calls + 1, failures = failures + ?`,
			e.AgentName, t.ToolName, boolToInt(!t.OK), boolToInt(!t.OK)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE agent_episodes SET status = 'rewarded' WHERE id = ? AND status = 'pending'`, e.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// ToolCallStat is one tool invocation folded into per-agent stats.
type ToolCallStat struct {
	ToolName string
	OK       bool
}

// NewID generates a prefixed unique id (e.g. for episodes created in advance).
func (s *Store) NewID(prefix string) string { return newID(prefix) }

// AddReward records a signal and folds it into the episode total.
func (s *Store) AddReward(episodeID, signal, note string, value float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO agent_rewards (id, episode_id, signal, value, note, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		newID("rw-"), episodeID, signal, value, note, now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE agent_episodes SET reward = reward + ? WHERE id = ?`, value, episodeID); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishEpisode marks the episode rewarded (finalize learning).
func (s *Store) FinishEpisode(episodeID string) error {
	_, err := s.db.Exec(`UPDATE agent_episodes SET status = 'rewarded' WHERE id = ? AND status = 'pending'`, episodeID)
	return err
}

// TopEpisodes returns the highest-reward episodes for an agent (for few-shot replay).
func (s *Store) TopEpisodes(agentName string, minReward float64, limit int) ([]Episode, error) {
	rows, err := s.db.Query(`SELECT id, user_id, agent_name, query, answer, reward, turn_count, messages_json, traces_json, created_at
		FROM agent_episodes WHERE agent_name = ? AND reward >= ? AND status = 'rewarded'
		ORDER BY reward DESC, created_at DESC LIMIT ?`, agentName, minReward, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Episode
	for rows.Next() {
		var e Episode
		var tracesJSON string
		if err := rows.Scan(&e.ID, &e.UserID, &e.AgentName, &e.Query, &e.Answer, &e.Reward, &e.TurnCount, &e.Messages, &tracesJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tracesJSON), &e.Traces); err != nil {
			return nil, fmt.Errorf("corrupt traces_json for episode %s: %w", e.ID, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CreateAction stores a pending mutating action.
func (s *Store) CreateAction(a *Action) error {
	a.ID = newID("act-")
	a.Status = ActionPending
	a.CreatedAt = now()
	_, err := s.db.Exec(`INSERT INTO agent_actions (id, episode_id, tool_name, args_json, summary, status, requested_by, created_at)
		VALUES (?, ?, ?, ?, ?, 'pending', ?, ?)`,
		a.ID, a.EpisodeID, a.ToolName, a.ArgsJSON, a.Summary, a.RequestedBy, a.CreatedAt)
	return err
}

// GetAction loads an action by id.
func (s *Store) GetAction(id string) (*Action, error) {
	var a Action
	err := s.db.QueryRow(`SELECT id, episode_id, tool_name, args_json, summary, status,
		COALESCE(requested_by,''), COALESCE(decided_by,''), COALESCE(decided_at,''), COALESCE(result,''), COALESCE(error,''), created_at
		FROM agent_actions WHERE id = ?`, id).
		Scan(&a.ID, &a.EpisodeID, &a.ToolName, &a.ArgsJSON, &a.Summary, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecidedAt, &a.Result, &a.Error, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// ListActions returns actions filtered by status ("" = all), newest first.
func (s *Store) ListActions(status ActionStatus, limit int) ([]Action, error) {
	q := `SELECT id, episode_id, tool_name, args_json, summary, status,
		COALESCE(requested_by,''), COALESCE(decided_by,''), COALESCE(decided_at,''), COALESCE(result,''), COALESCE(error,''), created_at
		FROM agent_actions`
	args := []any{}
	if status != "" {
		q += ` WHERE status = ?`
		args = append(args, string(status))
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Action
	for rows.Next() {
		var a Action
		if err := rows.Scan(&a.ID, &a.EpisodeID, &a.ToolName, &a.ArgsJSON, &a.Summary, &a.Status, &a.RequestedBy, &a.DecidedBy, &a.DecidedAt, &a.Result, &a.Error, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ClaimAction atomically moves a pending action to 'approved', claiming it
// for execution. Returns false when the action is not pending (already
// decided, or being decided concurrently) so a mutating tool can never run
// twice from double-approval.
func (s *Store) ClaimAction(id, decidedBy, decidedAt string) (bool, error) {
	res, err := s.db.Exec(`UPDATE agent_actions SET status = 'approved', decided_by = ?, decided_at = ? WHERE id = ? AND status = 'pending'`,
		decidedBy, decidedAt, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// UpdateActionDecision finalizes a claimed (approved) action or records a
// rejection. The expected-status guard makes the transition atomic: it only
// applies when the action is still in the claimed state, so concurrent
// approve/reject can never double-execute or overwrite each other.
func (s *Store) UpdateActionDecision(a *Action, expect ActionStatus) (bool, error) {
	res, err := s.db.Exec(`UPDATE agent_actions SET status = ?, decided_by = ?, decided_at = ?, result = ?, error = ? WHERE id = ? AND status = ?`,
		a.Status, a.DecidedBy, a.DecidedAt, a.Result, a.Error, a.ID, expect)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// RecordToolCall updates per-agent tool usage stats.
func (s *Store) RecordToolCall(agentName, toolName string, ok bool) error {
	_, err := s.db.Exec(`INSERT INTO agent_tool_stats (agent_name, tool_name, calls, failures) VALUES (?, ?, 1, ?)
		ON CONFLICT(agent_name, tool_name) DO UPDATE SET calls = calls + 1, failures = failures + ?`,
		agentName, toolName, boolToInt(!ok), boolToInt(!ok))
	return err
}

// ToolStats returns per-agent tool failure rates.
func (s *Store) ToolStats(agentName string) ([]ToolStat, error) {
	rows, err := s.db.Query(`SELECT tool_name, calls, failures FROM agent_tool_stats WHERE agent_name = ? ORDER BY tool_name`, agentName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ToolStat
	for rows.Next() {
		var ts ToolStat
		if err := rows.Scan(&ts.Tool, &ts.Calls, &ts.Failures); err != nil {
			return nil, err
		}
		if ts.Calls > 0 {
			ts.FailureRate = float64(ts.Failures) / float64(ts.Calls)
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

// ToolStat aggregates call/failure counts for one tool.
type ToolStat struct {
	Tool        string  `json:"tool"`
	Calls       int     `json:"calls"`
	Failures    int     `json:"failures"`
	FailureRate float64 `json:"failure_rate"`
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
