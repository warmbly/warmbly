package repository

import (
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// validateActionConfig checks a non-email (action/wait) node's typed config
// before it is persisted. This is a shape gate only; the kind↔action pairing and
// cross-step routing targets are handled elsewhere (the canvas sets kind+action
// together, and dangling routes resolve to "stop" at schedule time).
func validateActionConfig(a *models.ActionConfig) *errx.Error {
	if a == nil {
		return nil
	}
	switch a.Type {
	case "wait", "add_tag", "remove_tag", "label_email", "unsubscribe",
		"notify", "create_task", "create_deal", "move_deal_stage",
		"run_automation", "fire_event", "end":
		// Type must be known. Sub-config (wait minutes, tag, event name, HTTP
		// request) is filled in the editor; an unconfigured node is a harmless
		// no-op at send time, so we don't block creating a draft node here. Keep
		// this list in sync with models.ActionConfig.Type and the task executor's
		// switch (internal/tasks/campaign_task.go, advanced/reply_actions.go).
		return nil
	case "ai":
		// The whole config is one instruction: outcomes are the step's outgoing
		// ai_label paths and side effects are ordinary steps on those paths. An
		// unconfigured AI node is a draft no-op like the rest; only bound the
		// prompt surface (an oversized instruction is a malformed write).
		if len(a.AIInstruction) > maxAIStepInstruction {
			return errx.ErrSequenceAction
		}
		return nil
	default:
		return errx.ErrSequenceAction
	}
}

// AI step bounds: the instruction is the step's one config field; outcome
// names live on the ai_label branch conditions (branch_conditions.go).
const (
	maxAIStepInstruction = 4000
	maxAIStepName        = 80
)
