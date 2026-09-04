package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"transport-app/internal/agent/rl"
)

// ApprovalService gates mutating tools behind admin sign-off.
type ApprovalService struct {
	rl        *rl.Service
	env       *ToolEnv
	originals map[string]ToolFunc
	gated     map[string]bool
}

// NewApprovalService wires the approval gate.
func NewApprovalService(rlSvc *rl.Service, env *ToolEnv) *ApprovalService {
	return &ApprovalService{
		rl:        rlSvc,
		env:       env,
		originals: map[string]ToolFunc{},
		gated:     map[string]bool{},
	}
}

// Gate marks a tool as requiring approval and stores its original handler.
func (a *ApprovalService) Gate(name string, handler ToolFunc) {
	a.originals[name] = handler
	a.gated[name] = true
}

// IsGated reports whether a tool requires approval.
func (a *ApprovalService) IsGated(name string) bool { return a.gated[name] }

// GatedTool returns a wrapped tool that submits an action instead of executing.
func (a *ApprovalService) GatedTool(t *RegisteredTool) *RegisteredTool {
	return &RegisteredTool{
		Name:        t.Name,
		Description: t.Description + " [REQUIRES ADMIN APPROVAL: the action is submitted for review, not executed immediately]",
		Parameters:  t.Parameters,
		// RBAC tag intentionally not copied: submitting for approval is not
		// the privileged act — the ADMIN who approves is, and the action
		// executes under the admin's identity (which holds the permission).
		// The original (permission-tagged) handler stays registered for
		// post-approval execution.
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			requester := userNameFrom(ctx)
			if requester == "" {
				requester = a.env.UserName
			}
			action := &rl.Action{
				EpisodeID:   episodeIDFrom(ctx),
				ToolName:    t.Name,
				ArgsJSON:    string(args),
				Summary:     summaryOf(t.Name, args),
				RequestedBy: requester,
			}
			if err := a.rl.SubmitAction(action); err != nil {
				return "", err
			}
			return fmt.Sprintf("Action %s submitted for admin approval: %s. Pending decision in the approval queue.", action.ID, action.Summary), nil
		},
	}
}

// MutatingTools lists the tools that get approval-gated.
func MutatingTools() []string {
	return []string{"create_booking", "assign_driver", "assign_vehicle", "record_payment", "approve_kharcha", "reject_kharcha", "extend_ewaybill", "create_work_order", "transition_work_order"}
}

// Approve claims a pending action atomically, then executes it under the
// admin's identity (carried via context, so no shared state is mutated).
func (a *ApprovalService) Approve(ctx context.Context, actionID, adminID, adminName string) (*rl.Action, error) {
	claimed, err := a.rl.ClaimAction(actionID, adminName, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, fmt.Errorf("action %s is not pending (already decided or being decided)", actionID)
	}

	action, err := a.rl.GetAction(actionID)
	if err != nil {
		return nil, err
	}
	handler, ok := a.originals[action.ToolName]
	if !ok {
		return nil, fmt.Errorf("no handler registered for tool %q", action.ToolName)
	}

	// Execute under the admin's identity via context: kharcha approvals
	// record the approver without mutating shared state.
	execCtx := context.WithValue(ctx, userIDCtxKey, adminID)
	execCtx = context.WithValue(execCtx, userNameCtxKey, adminName)

	var result string
	var execErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				execErr = fmt.Errorf("tool handler panicked: %v", r)
			}
		}()
		result, execErr = handler(execCtx, json.RawMessage(action.ArgsJSON))
	}()

	action.DecidedBy = adminName
	action.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	action.Result = result
	if execErr != nil {
		action.Status = rl.ActionFailed
		action.Error = execErr.Error()
	} else {
		action.Status = rl.ActionExecuted
	}
	if err := a.rl.UpdateActionDecision(action, rl.ActionApproved); err != nil {
		// Never leave the action 'approved' after the tool ran: mark it
		// failed so a re-approval cannot execute the mutation twice.
		failed := *action
		failed.Status = rl.ActionFailed
		failed.Error = "decision persist failed: " + err.Error()
		_ = a.rl.UpdateActionDecision(&failed, rl.ActionApproved)
		return nil, err
	}
	return action, nil
}

// Reject rejects a pending action (atomic CAS on pending).
func (a *ApprovalService) Reject(ctx context.Context, actionID, adminName, reason string) (*rl.Action, error) {
	action, err := a.rl.GetAction(actionID)
	if err != nil {
		return nil, err
	}
	if action.Status != rl.ActionPending {
		return nil, fmt.Errorf("action %s is %s, not pending", actionID, action.Status)
	}
	action.Status = rl.ActionRejected
	action.DecidedBy = adminName
	action.DecidedAt = time.Now().UTC().Format(time.RFC3339)
	action.Result = "rejected: " + reason
	applied, err := a.rl.ClaimAction(actionID, adminName, action.DecidedAt)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, fmt.Errorf("action %s is no longer pending (concurrent decision)", actionID)
	}
	if err := a.rl.UpdateActionDecision(action, rl.ActionApproved); err != nil {
		return nil, err
	}
	return action, nil
}

// ListPending returns the approval queue.
func (a *ApprovalService) ListPending() ([]rl.Action, error) {
	return a.rl.ListPendingActions()
}

// summaryOf builds a short human-readable line for the queue page.
func summaryOf(toolName string, args json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(args, &m); err != nil {
		return toolName
	}
	keys := []string{"customer_id", "route_id", "pickup_date", "vehicle_type", "price", "trip_id", "driver_id", "vehicle_id", "invoice_id", "amount", "method", "expense_id", "reason", "id", "title", "status", "assignee", "vendor", "cost_estimate", "due_at"}
	parts := []string{}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			parts = append(parts, k+"="+fmt.Sprintf("%v", v))
		}
	}
	return toolName + ": " + join(parts, ", ")
}

func join(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

type ctxKey string

const episodeCtxKey ctxKey = "agent_episode_id"

func episodeIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(episodeCtxKey).(string); ok {
		return v
	}
	return ""
}
