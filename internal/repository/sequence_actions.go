package repository

import (
	"strings"

	"github.com/google/uuid"

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
		// An unconfigured AI node is a draft no-op like the rest; when configured,
		// bound the prompt surface (an oversized instruction or label set is a
		// malformed write, not a draft).
		if len(a.AIInstruction) > maxAIStepInstruction {
			return errx.ErrSequenceAction
		}
		if len(a.AILabels) > maxAIStepList || len(a.AIOutputFields) > maxAIStepList {
			return errx.ErrSequenceAction
		}
		for _, v := range a.AILabels {
			if len(v) > maxAIStepName {
				return errx.ErrSequenceAction
			}
		}
		for _, v := range a.AIOutputFields {
			if len(v) > maxAIStepName {
				return errx.ErrSequenceAction
			}
		}
		if len(a.AIActions) > maxAIStepList {
			return errx.ErrSequenceAction
		}
		seen := map[string]bool{}
		for i := range a.AIActions {
			act := &a.AIActions[i]
			id := act.ID
			if id == "" || len(id) > maxAIStepName || seen[id] {
				return errx.ErrSequenceAction
			}
			seen[id] = true
			if len(act.When) > maxAIStepWhen {
				return errx.ErrSequenceAction
			}
			// The model may only pull pre-configured side-effect triggers: no
			// nested ai (recursion), no wait/end (timing/routing is the canvas's
			// job), and no further nesting.
			if !aiStepActionTypes[act.Action.Type] || len(act.Action.AIActions) > 0 {
				return errx.ErrSequenceAction
			}
			// Choice sets (the model picks WHICH tags/labels apply) only make
			// sense on the tag/label actions, from a bounded named set.
			if len(act.Choices) > 0 {
				switch act.Action.Type {
				case "add_tag", "remove_tag", "label_email":
				default:
					return errx.ErrSequenceAction
				}
				if len(act.Choices) > maxAIStepChoices {
					return errx.ErrSequenceAction
				}
				for _, c := range act.Choices {
					name := strings.TrimSpace(c.Name)
					if c.CategoryID == uuid.Nil || name == "" || len(name) > maxAIStepName {
						return errx.ErrSequenceAction
					}
				}
			}
			if act.MaxChoices < 0 || act.MaxChoices > maxAIStepChoices {
				return errx.ErrSequenceAction
			}
		}
		return nil
	default:
		return errx.ErrSequenceAction
	}
}

// AI step config bounds: one instruction, small closed label/field/action sets.
const (
	maxAIStepInstruction = 4000
	maxAIStepList        = 10
	maxAIStepName        = 80
	maxAIStepWhen        = 200
	maxAIStepChoices     = 20
)

// aiStepActionTypes are the side effects an AI step may be allowed to trigger.
// Keep in sync with the executor loop in internal/tasks/ai_step.go and the
// editor's palette in web (CampaignFlow AIStepFields).
var aiStepActionTypes = map[string]bool{
	"add_tag":         true,
	"remove_tag":      true,
	"label_email":     true,
	"unsubscribe":     true,
	"create_task":     true,
	"create_deal":     true,
	"move_deal_stage": true,
	"run_automation":  true,
	"fire_event":      true,
}
