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
	case "wait", "add_tag", "remove_tag", "unsubscribe", "notify", "create_task", "create_deal", "move_deal_stage", "run_automation", "end":
		// Type must be known. Sub-config (wait minutes, tag) is filled in the
		// editor; an unconfigured node is a harmless no-op at send time, so we
		// don't block creating a draft node here.
		return nil
	default:
		return errx.ErrSequenceAction
	}
}
