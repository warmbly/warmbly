package aitools

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/audit"
	"github.com/warmbly/warmbly/internal/app/placement"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
)

// PlacementTests is the slice of placement.Service the tools use.
type PlacementTests interface {
	CreateTests(ctx context.Context, in placement.CreateInput) ([]placement.TestView, *errx.Error)
	ListTests(ctx context.Context, orgID *uuid.UUID, campaignID *uuid.UUID, limit, offset int) ([]placement.TestView, int, *errx.Error)
	GetTest(ctx context.Context, orgID *uuid.UUID, id uuid.UUID) (*placement.TestDetail, *errx.Error)
}

type placementTools struct {
	svc   PlacementTests
	audit audit.AuditService
}

// RegisterPlacementTools adds the inbox placement tools once the placement
// service exists, which is after the registry is built.
func RegisterPlacementTools(r *Registry, svc PlacementTests, auditSvc audit.AuditService) {
	if r == nil || svc == nil {
		return
	}
	p := placementTools{svc: svc, audit: auditSvc}

	r.Register(Tool{
		Name:        "list_placement_tests",
		Description: "List the workspace's inbox placement tests, newest first, with where each test's copies landed (primary inbox, Gmail tabs, spam, never arrived). Use this to answer whether copy is landing in the inbox before or during a campaign.",
		InputSchema: objectSchema(map[string]any{
			"campaign_id": strProp("Only this campaign's tests (UUID)."),
			"limit":       intProp("Maximum tests to return (default 10, max 50)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         p.list,
	})

	r.Register(Tool{
		Name:        "get_placement_test",
		Description: "Read one inbox placement test in full: where every copy landed per mail provider, the content check of the tested copy, and for a tracking comparison the untracked and tracked halves side by side.",
		InputSchema: objectSchema(map[string]any{
			"test_id": strProp("The placement test's UUID."),
		}, "test_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         p.get,
	})

	r.Register(Tool{
		Name: "run_placement_test",
		Description: "Start an inbox placement test: send a campaign step (or an ad-hoc subject and body) from one of the workspace's mailboxes to a panel of seed inboxes and report where each copy lands. " +
			"Every copy is a real send counted against that mailbox's daily limit, so only run it when the user asked for a placement test. Results arrive over the next minutes; read them with get_placement_test.",
		InputSchema: objectSchema(map[string]any{
			"sender_account_id": strProp("The mailbox to send from (UUID)."),
			"campaign_id":       strProp("Test this campaign's copy (UUID)."),
			"sequence_id":       strProp("The campaign step to test (UUID); requires campaign_id."),
			"subject":           strProp("Subject of an ad-hoc template, when not testing a campaign step."),
			"body_plain":        strProp("Plain-text body of an ad-hoc template."),
			"tracking":          enumProp("Open and click tracking on the copies (default campaign).", "campaign", "on", "off", "compare"),
			"panel":             enumProp("Which seed inboxes to test on (default instance).", "instance", "workspace", "cloud"),
		}, "sender_account_id"),
		Risk:            generation.RiskSend,
		RequiredOrgPerm: models.PermSendCampaigns,
		RequiredAPIPerm: models.APIPermSendCampaigns,
		Handler:         p.run,
	})
}

func placementSummary(v placement.TestView) map[string]any {
	out := map[string]any{
		"test_id":       v.ID.String(),
		"status":        v.Status,
		"sender":        v.SenderEmail,
		"subject":       v.Subject,
		"panel":         v.Panel,
		"open_tracking": v.OpenTracking,
		"link_tracking": v.LinkTracking,
		"created_at":    v.CreatedAt,
		"summary":       v.Summary,
		"by_provider":   v.Families,
	}
	if v.CampaignID != nil {
		out["campaign_id"] = v.CampaignID.String()
	}
	if v.Error != "" {
		out["error"] = v.Error
	}
	return out
}

func (p placementTools) list(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		CampaignID string `json:"campaign_id"`
		Limit      int    `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	var campaignID *uuid.UUID
	if in.CampaignID != "" {
		id, err := parseUUIDArg(in.CampaignID)
		if err != nil {
			return "", err
		}
		campaignID = &id
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	limit = min(limit, 50)
	org := inv.OrgID
	tests, total, xerr := p.svc.ListTests(ctx, &org, campaignID, limit, 0)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(tests))
	for _, t := range tests {
		out = append(out, placementSummary(t))
	}
	return jsonResult(map[string]any{"tests": out, "total": total})
}

func (p placementTools) get(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		TestID string `json:"test_id"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.TestID)
	if err != nil {
		return "", err
	}
	org := inv.OrgID
	detail, xerr := p.svc.GetTest(ctx, &org, id)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := placementSummary(detail.TestView)
	out["content_score"] = detail.Content.Score
	out["content_issues"] = detail.Content.Issues
	results := make([]map[string]any, 0, len(detail.Results))
	for _, r := range detail.Results {
		row := map[string]any{"provider": r.FamilyLabel, "folder": r.Folder}
		if r.Error != "" {
			row["error"] = r.Error
		}
		results = append(results, row)
	}
	out["copies"] = results
	if detail.Compare != nil {
		out["comparison"] = placementSummary(*detail.Compare)
	}
	return jsonResult(out)
}

func (p placementTools) run(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		SenderAccountID string `json:"sender_account_id"`
		CampaignID      string `json:"campaign_id"`
		SequenceID      string `json:"sequence_id"`
		Subject         string `json:"subject"`
		BodyPlain       string `json:"body_plain"`
		Tracking        string `json:"tracking"`
		Panel           string `json:"panel"`
	}](args)
	if err != nil {
		return "", err
	}
	sender, err := parseUUIDArg(in.SenderAccountID)
	if err != nil {
		return "", err
	}
	optional := func(s string) (*uuid.UUID, error) {
		if s == "" {
			return nil, nil
		}
		id, err := parseUUIDArg(s)
		return &id, err
	}
	campaignID, err := optional(in.CampaignID)
	if err != nil {
		return "", err
	}
	sequenceID, err := optional(in.SequenceID)
	if err != nil {
		return "", err
	}
	var userID *uuid.UUID
	if inv.UserID != uuid.Nil {
		u := inv.UserID
		userID = &u
	}
	tests, xerr := p.svc.CreateTests(ctx, placement.CreateInput{
		OrgID:           inv.OrgID,
		UserID:          userID,
		SenderAccountID: sender,
		CampaignID:      campaignID,
		SequenceID:      sequenceID,
		Subject:         in.Subject,
		BodyPlain:       in.BodyPlain,
		Tracking:        in.Tracking,
		Panel:           in.Panel,
		Origin:          models.PlacementOriginManual,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(tests))
	for _, t := range tests {
		id := t.ID
		if p.audit != nil {
			p.audit.LogAction(ctx, inv.OrgID, inv.UserID, models.AuditActionCreate, models.AuditEntityPlacementTest, &id,
				inv.IP, inv.UserAgent, nil, map[string]string{"sender": t.SenderEmail, "panel": t.Panel})
		}
		out = append(out, placementSummary(t))
	}
	return jsonResult(map[string]any{
		"tests": out,
		"note":  "Copies go out one at a time; read the result with get_placement_test in a few minutes.",
	})
}
