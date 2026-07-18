package repository

import (
	"regexp"
	"strings"

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
	case "switch":
		// A draft switch (no cases / no decider yet) is a no-op like other
		// unconfigured nodes; bound the surface of what IS set. Cases become
		// draggable dots on the node and route via ai_label branch conditions,
		// so their names share the branch-label bound.
		switch a.SwitchOn {
		case "", "ai", "value":
		default:
			return errx.ErrSequenceAction
		}
		if len(a.AIInstruction) > maxAIStepInstruction || len(a.SwitchValue) > maxSwitchValue {
			return errx.ErrSequenceAction
		}
		if len(a.SwitchCases) > maxSwitchCases {
			return errx.ErrSequenceAction
		}
		seen := map[string]bool{}
		for _, c := range a.SwitchCases {
			name := strings.ToLower(strings.TrimSpace(c))
			if name == "" || len(c) > maxAIStepName || seen[name] {
				return errx.ErrSequenceAction
			}
			seen[name] = true
			// A "/pattern/" case is a regex in value mode: reject patterns that
			// don't compile so a broken case never sits silently unmatched.
			if len(name) >= 3 && strings.HasPrefix(name, "/") && strings.HasSuffix(name, "/") {
				if _, err := regexp.Compile("(?i)" + name[1:len(name)-1]); err != nil {
					return errx.ErrSequenceAction
				}
			}
		}
		return nil
	default:
		return errx.ErrSequenceAction
	}
}

// Switch step bounds. Case names live on the node config AND on the ai_label
// branch conditions (branch_conditions.go), so they share maxAIStepName.
const (
	maxAIStepInstruction = 4000
	maxAIStepName        = 80
	maxSwitchValue       = 500
	maxSwitchCases       = 20
)
