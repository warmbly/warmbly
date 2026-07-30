package aitools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
)

// AdvisorReader is the read slice of the advisor service. It is declared here
// rather than imported so the tool registry stays free of a dependency on the
// advisor package, which itself calls back into this registry to apply fixes.
//
// Deliberately read-only: the assistant should read the Advisor's findings to
// ground its own answers ("your reply rate is low because...") and then make
// the change through the ordinary tool for that change, so the write goes
// through exactly one audited path.
type AdvisorReader interface {
	List(ctx context.Context, orgID uuid.UUID, f repository.AdvisorFindingFilter) ([]models.AdvisorFinding, *errx.Error)
	Summary(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSummary, *errx.Error)
}

// RegisterAdvisorTools adds the advisor read tools to an already-built
// registry. It is a standalone function rather than part of BuildRegistry
// because the advisor service needs the registry to apply its fixes, so the
// two cannot both be constructed first. Calling it later closes that loop
// without either package importing the other.
func RegisterAdvisorTools(r *Registry, reader AdvisorReader) {
	if r == nil || reader == nil {
		return
	}
	d := Deps{Advisor: reader}

	r.Register(Tool{
		Name:        "list_recommendations",
		Description: "Read the Advisor's open recommendations for this workspace: what is currently wrong with deliverability, mailbox configuration, warmup, campaign performance, email copy, or list quality, with the evidence behind each one. Use this before answering any question about why results are poor, instead of guessing.",
		InputSchema: objectSchema(map[string]any{
			"surface":     enumProp("Limit to one dashboard area.", "campaigns", "emails", "deliverability", "contacts", "analytics", "settings"),
			"category":    enumProp("Limit to one kind of problem.", "deliverability", "mailbox", "warmup", "campaign", "copy", "list"),
			"entity_type": enumProp("Limit to recommendations about one kind of thing.", "campaign", "email_account", "step"),
			"entity_id":   strProp("Limit to one campaign, mailbox, or step by UUID."),
			"limit":       intProp("Maximum recommendations to return (default 25)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         d.listRecommendations,
	})

	r.Register(Tool{
		Name:            "get_advisor_summary",
		Description:     "Read the workspace's Advisor health score and how many open recommendations there are at each severity, broken down by dashboard area.",
		InputSchema:     objectSchema(map[string]any{}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         d.getAdvisorSummary,
	})
}

func (d Deps) listRecommendations(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Surface    string `json:"surface"`
		Category   string `json:"category"`
		EntityType string `json:"entity_type"`
		EntityID   string `json:"entity_id"`
		Limit      int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}

	filter := repository.AdvisorFindingFilter{
		Surface:    models.AdvisorSurface(in.Surface),
		Category:   models.AdvisorCategory(in.Category),
		EntityType: in.EntityType,
		Limit:      in.Limit,
	}
	if filter.Limit <= 0 {
		filter.Limit = 25
	}
	if in.EntityID != "" {
		id, err := parseUUIDArg(in.EntityID)
		if err != nil {
			return "", err
		}
		filter.EntityID = &id
	}

	list, xerr := d.Advisor.List(ctx, inv.OrgID, filter)
	if xerr != nil {
		return "", fromErrx(xerr)
	}

	// Project to the fields that help the model reason, dropping the UI-only
	// action payload so a large tool result does not crowd out the transcript.
	type row struct {
		Severity string          `json:"severity"`
		Category string          `json:"category"`
		Subject  string          `json:"subject,omitempty"`
		Title    string          `json:"title"`
		Detail   string          `json:"detail"`
		Remedy   string          `json:"remedy"`
		Evidence json.RawMessage `json:"evidence,omitempty"`
		HasFix   bool            `json:"has_one_click_fix"`
	}
	out := make([]row, 0, len(list))
	for _, f := range list {
		out = append(out, row{
			Severity: string(f.Severity),
			Category: string(f.Category),
			Subject:  f.EntityLabel,
			Title:    f.Title,
			Detail:   f.Detail,
			Remedy:   f.Remedy,
			Evidence: f.Evidence,
			HasFix:   f.Action != nil,
		})
	}
	return jsonResult(map[string]any{"recommendations": out, "count": len(out)})
}

func (d Deps) getAdvisorSummary(ctx context.Context, inv Invocation, _ json.RawMessage) (string, error) {
	summary, xerr := d.Advisor.Summary(ctx, inv.OrgID)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	return jsonResult(summary)
}
