package aitools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/warmbly/warmbly/internal/pkg/generation"
)

func (d Deps) registerSkillTools(r *Registry) {
	if d.Skills == nil {
		return
	}
	r.Register(Tool{
		Name:        "load_skill",
		Description: "Read the full content of one of this workspace's playbooks (listed in the system prompt) by name. Use when a playbook is relevant to the current task.",
		InputSchema: objectSchema(map[string]any{
			"name": strProp("The playbook name, exactly as listed."),
		}, "name"),
		Risk:    generation.RiskRead,
		Handler: d.loadSkill,
	})
}

func (d Deps) loadSkill(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Name string `json:"name"`
	}](args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Name) == "" {
		return "", ErrInvalidArgs
	}
	sk, gerr := d.Skills.GetByName(ctx, inv.OrgID, in.Name)
	if gerr != nil {
		return "", gerr
	}
	if sk == nil {
		return `{"error":"no enabled playbook with that name"}`, nil
	}
	return jsonResult(map[string]any{"name": sk.Name, "content": sk.Content})
}
