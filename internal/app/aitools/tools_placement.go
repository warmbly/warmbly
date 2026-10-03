package aitools

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

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
	CreateBatch(ctx context.Context, in placement.BatchInput) (*placement.BatchView, *errx.Error)
	ListBatches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]placement.BatchView, int, *errx.Error)
	GetBatch(ctx context.Context, orgID, id uuid.UUID) (*placement.BatchDetail, *errx.Error)
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
			"pace":              enumProp("spaced (default, about a minute between copies) or quick (a few seconds apart, results in minutes).", "spaced", "quick"),
			"families":          arrProp("Only seeds at these providers, as family ids: gmail, google_workspace, outlook, microsoft365, yahoo and the others a test's by_provider lists.", map[string]any{"type": "string"}),
		}, "sender_account_id"),
		Risk:            generation.RiskSend,
		RequiredOrgPerm: models.PermSendCampaigns,
		RequiredAPIPerm: models.APIPermSendCampaigns,
		Handler:         p.run,
	})

	r.Register(Tool{
		Name:        "list_placement_batches",
		Description: "List the workspace's placement batches (one placement test run from many sending mailboxes), newest first, with each batch's progress and overall inbox placement.",
		InputSchema: objectSchema(map[string]any{
			"limit": intProp("Maximum batches to return (default 10, max 50)."),
		}),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         p.listBatches,
	})

	r.Register(Tool{
		Name: "get_placement_batch",
		Description: "Read one placement batch: progress, overall placement, and placement by sending domain, by sending provider and by recipient provider, worst first. " +
			"Use it to tell one bad mailbox from a whole domain or provider losing reputation.",
		InputSchema: objectSchema(map[string]any{
			"batch_id": strProp("The placement batch's UUID."),
		}, "batch_id"),
		Risk:            generation.RiskRead,
		RequiredOrgPerm: models.PermViewAnalytics,
		RequiredAPIPerm: models.APIPermReadAnalytics,
		Handler:         p.getBatch,
	})

	r.Register(Tool{
		Name: "run_placement_batch",
		Description: "Start a placement batch: run the same placement test from many of the workspace's mailboxes (a campaign's senders or the whole workspace, optionally sampled). " +
			"Every copy is a real send counted against its mailbox's daily limit, and the backend starts a few senders at a time, so a large batch takes hours. Only run it when the user asked for a fleet-wide placement test.",
		InputSchema: objectSchema(map[string]any{
			"scope":          enumProp("Whose mailboxes to test: a campaign's senders or every mailbox in the workspace.", "campaign", "workspace"),
			"campaign_id":    strProp("The campaign whose senders to test and, with sequence_id, whose copy to send (UUID)."),
			"sequence_id":    strProp("The campaign step to test (UUID); requires campaign_id."),
			"subject":        strProp("Subject of an ad-hoc template, when not testing a campaign step."),
			"body_plain":     strProp("Plain-text body of an ad-hoc template."),
			"sample":         enumProp("all (default), random (sample_count mailboxes), percent (sample_percent of them, spread across providers), per_domain or per_provider (sample_count each).", "all", "random", "percent", "per_domain", "per_provider"),
			"sample_count":   intProp("Mailboxes for a random sample, or per group for per_domain and per_provider."),
			"sample_percent": intProp("Share of mailboxes for a percent sample, 1 to 100."),
			"panel":          enumProp("Which seed inboxes to test on (default instance).", "instance", "workspace", "cloud"),
			"tracking":       enumProp("Open and click tracking on the copies (default campaign).", "campaign", "on", "off", "compare"),
		}, "scope"),
		Risk:            generation.RiskSend,
		RequiredOrgPerm: models.PermSendCampaigns,
		RequiredAPIPerm: models.APIPermSendCampaigns,
		Handler:         p.runBatch,
	})
}

func batchSummary(v placement.BatchView) map[string]any {
	out := map[string]any{
		"batch_id":   v.ID.String(),
		"status":     v.Status,
		"subject":    v.Subject,
		"panel":      v.Panel,
		"tracking":   v.Tracking,
		"senders":    v.SenderCount,
		"progress":   v.Progress,
		"summary":    v.Summary,
		"created_at": v.CreatedAt,
	}
	if v.CampaignID != nil {
		out["campaign_id"] = v.CampaignID.String()
	}
	if v.Error != "" {
		out["error"] = v.Error
	}
	return out
}

func (p placementTools) listBatches(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		Limit int `json:"limit"`
	}](args)
	if err != nil {
		return "", err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10
	}
	batches, total, xerr := p.svc.ListBatches(ctx, inv.OrgID, min(limit, 50), 0)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := make([]map[string]any, 0, len(batches))
	for _, b := range batches {
		out = append(out, batchSummary(b))
	}
	return jsonResult(map[string]any{"batches": out, "total": total})
}

func (p placementTools) getBatch(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	in, err := decodeArgs[struct {
		BatchID string `json:"batch_id"`
	}](args)
	if err != nil {
		return "", err
	}
	id, err := parseUUIDArg(in.BatchID)
	if err != nil {
		return "", err
	}
	d, xerr := p.svc.GetBatch(ctx, inv.OrgID, id)
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	out := batchSummary(d.BatchView)
	if d.Untracked != nil {
		out["untracked_summary"] = d.Untracked
	}
	// The worst groups carry the answer; the rest are on the batch page.
	out["by_sending_domain"] = d.Domains[:min(len(d.Domains), 25)]
	out["by_sending_provider"] = d.Providers
	out["by_recipient_provider"] = d.Recipients
	out["content_score"] = d.Content.Score
	return jsonResult(out)
}

func (p placementTools) runBatch(ctx context.Context, inv Invocation, args json.RawMessage) (string, error) {
	// An API key may be limited to some mailboxes, which this path cannot
	// see; the REST endpoint applies that limit.
	if inv.IsAPIKey {
		return "", errors.New("start a placement batch with POST /placement/batches, which applies this key's mailbox limits")
	}
	in, err := decodeArgs[struct {
		Scope         string `json:"scope"`
		CampaignID    string `json:"campaign_id"`
		SequenceID    string `json:"sequence_id"`
		Subject       string `json:"subject"`
		BodyPlain     string `json:"body_plain"`
		Sample        string `json:"sample"`
		SampleCount   int    `json:"sample_count"`
		SamplePercent int    `json:"sample_percent"`
		Panel         string `json:"panel"`
		Tracking      string `json:"tracking"`
	}](args)
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
	scope := &models.PlacementSenderScope{Type: in.Scope, CampaignID: campaignID}
	sample := models.PlacementSample{Mode: in.Sample, Count: in.SampleCount, Percent: in.SamplePercent}
	if sample.Mode == models.PlacementSamplePercent {
		sample.Stratify = "provider"
	}
	var userID *uuid.UUID
	if inv.UserID != uuid.Nil {
		u := inv.UserID
		userID = &u
	}
	batch, xerr := p.svc.CreateBatch(ctx, placement.BatchInput{
		OrgID:      inv.OrgID,
		UserID:     userID,
		Scope:      scope,
		Sample:     sample,
		CampaignID: campaignID,
		SequenceID: sequenceID,
		Subject:    in.Subject,
		BodyPlain:  in.BodyPlain,
		Panel:      in.Panel,
		Tracking:   in.Tracking,
	})
	if xerr != nil {
		return "", fromErrx(xerr)
	}
	id := batch.ID
	if p.audit != nil {
		p.audit.LogAction(ctx, inv.OrgID, inv.UserID, models.AuditActionCreate, models.AuditEntityPlacementBatch, &id,
			inv.IP, inv.UserAgent, nil, provenanceMeta(inv, map[string]string{"senders": strconv.Itoa(batch.SenderCount), "panel": batch.Panel}))
	}
	return jsonResult(map[string]any{
		"batch": batchSummary(*batch),
		"note":  "Senders start a few at a time; read progress with get_placement_batch.",
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
		SenderAccountID string   `json:"sender_account_id"`
		CampaignID      string   `json:"campaign_id"`
		SequenceID      string   `json:"sequence_id"`
		Subject         string   `json:"subject"`
		BodyPlain       string   `json:"body_plain"`
		Tracking        string   `json:"tracking"`
		Panel           string   `json:"panel"`
		Pace            string   `json:"pace"`
		Families        []string `json:"families"`
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
		Pace:            in.Pace,
		Families:        in.Families,
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
				inv.IP, inv.UserAgent, nil, provenanceMeta(inv, map[string]string{"sender": t.SenderEmail, "panel": t.Panel}))
		}
		out = append(out, placementSummary(t))
	}
	return jsonResult(map[string]any{
		"tests": out,
		"note":  "Copies go out one at a time; read the result with get_placement_test in a few minutes.",
	})
}
