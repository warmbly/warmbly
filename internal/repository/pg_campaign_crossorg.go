package repository

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// CopyCampaignAcrossInput describes one campaign copied into another
// workspace. Attachments carry the source rows with S3Key already pointing at
// the destination's copy of the bytes; URLRewrites maps source image URLs in
// the step and variant bodies to the destination's copies.
type CopyCampaignAcrossInput struct {
	SourceOrgID  uuid.UUID
	DestOrgID    uuid.UUID
	SourceID     uuid.UUID
	NewID        uuid.UUID
	UserID       uuid.UUID
	Attachments  []models.CampaignAttachment
	StorageLimit StorageLimitFunc
	URLRewrites  map[string]string
}

// CopyToOrganization copies a campaign's configuration into a draft owned by
// another workspace, in one transaction: the campaign row, its steps with the
// branch graph rewritten onto the new step ids, A/B variants, advanced
// settings and attachments. Nothing that only means something in the source
// travels: no leads, progress, logs, counters, tasks, ramp level, guardrail
// trip, tracking domain, sender pool, mailbox tags or folders. A step setting
// that names a source-workspace row is cleared and reported for review.
func (r *campaignRepository) CopyToOrganization(ctx context.Context, in CopyCampaignAcrossInput) (*models.Campaign, []models.CopyReview, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	const copyCampaign = `
		INSERT INTO campaigns (
			id, user_id, organization_id, name, description, status, kind,
			stop_on_reply, stop_on_meeting, open_tracking, link_tracking, text_only,
			daily_limit, unsubscribe_header, unsubscribe_mode, risky_emails,
			cc_addr, bcc_addr,
			start_date, end_date, timezone, days, start_time, end_time, schedule_windows,
			contact_order_by, contact_order_dir, contact_order_field,
			sender_strategy, rotation_mode, follow_up_sender,
			ramp_enabled, ramp_start, ramp_increment, ramp_ceiling, ramp_level, ramp_level_date,
			esp_match_mode, max_new_leads_per_day, prioritize_new_leads, continuous, entry_delay_minutes,
			tracking_domain, tracking_domain_verified, tracking_domain_verified_at,
			guardrail_enabled, guardrail_bounce_rate_max, guardrail_complaint_rate_max,
			guardrail_reply_rate_min, guardrail_min_sample, guardrail_window_days,
			guardrail_tripped_at, guardrail_reason,
			utm_tracking, utm_source, utm_medium, utm_campaign,
			last_status_change_at, updated_at, created_at
		)
		SELECT
			$2, $3, $4, name, description, 'draft', kind,
			stop_on_reply, stop_on_meeting, open_tracking, link_tracking, text_only,
			daily_limit, unsubscribe_header, unsubscribe_mode, risky_emails,
			cc_addr, bcc_addr,
			CASE WHEN start_date > NOW() THEN start_date END,
			CASE WHEN end_date > NOW() THEN end_date END,
			timezone, days, start_time, end_time, schedule_windows,
			contact_order_by, contact_order_dir, contact_order_field,
			sender_strategy, rotation_mode, follow_up_sender,
			ramp_enabled, ramp_start, ramp_increment, ramp_ceiling, 0, NULL,
			esp_match_mode, max_new_leads_per_day, prioritize_new_leads, continuous, entry_delay_minutes,
			'', false, NULL,
			guardrail_enabled, guardrail_bounce_rate_max, guardrail_complaint_rate_max,
			guardrail_reply_rate_min, guardrail_min_sample, guardrail_window_days,
			NULL, '',
			utm_tracking, utm_source, utm_medium, utm_campaign,
			NULL, NOW(), NOW()
		FROM campaigns
		WHERE id = $1 AND organization_id = $5
	`
	args := []any{in.SourceID, in.NewID, in.UserID, in.DestOrgID, in.SourceOrgID}
	cmd, err := tx.Exec(ctx, copyCampaign, args...)
	if err != nil {
		db.CaptureError(err, copyCampaign, args, "exec")
		return nil, nil, err
	}
	if cmd.RowsAffected() == 0 {
		return nil, nil, errx.ErrResourceNotFound
	}

	// Within one workspace every reference a step holds still resolves.
	var scrubFor *uuid.UUID
	if in.DestOrgID != in.SourceOrgID {
		scrubFor = &in.DestOrgID
	}
	stepIDs, review, err := copyCampaignStepsTx(ctx, tx, in.SourceID, in.NewID, scrubFor)
	if err != nil {
		return nil, nil, err
	}
	if err := copyCampaignVariantsTx(ctx, tx, in.SourceID, in.NewID, stepIDs); err != nil {
		return nil, nil, err
	}
	const copySettings = `
		INSERT INTO campaign_advanced_settings (campaign_id, settings, updated_at)
		SELECT $2, settings, NOW() FROM campaign_advanced_settings WHERE campaign_id = $1
	`
	if _, err := tx.Exec(ctx, copySettings, in.SourceID, in.NewID); err != nil {
		db.CaptureError(err, copySettings, []any{in.SourceID, in.NewID}, "exec")
		return nil, nil, err
	}
	if err := rewriteCopiedBodiesTx(ctx, tx, in.NewID, in.URLRewrites); err != nil {
		return nil, nil, err
	}
	if err := insertCopiedAttachmentsTx(ctx, tx, in.DestOrgID, in.NewID, in.UserID, in.Attachments, stepIDs, in.StorageLimit); err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, nil, err
	}
	campaign, err := r.GetByID(ctx, in.NewID)
	if err != nil {
		return nil, nil, err
	}
	return campaign, review, nil
}

// rewriteCopiedBodiesTx points the copied steps and variants at the
// destination's copies of the images they show.
func rewriteCopiedBodiesTx(ctx context.Context, tx pgx.Tx, campaignID uuid.UUID, rewrites map[string]string) error {
	const steps = `
		UPDATE sequences SET body_html = replace(body_html, $2, $3), body_plain = replace(body_plain, $2, $3)
		WHERE campaign_id = $1
	`
	const variants = `
		UPDATE campaign_ab_variants SET body_html = replace(body_html, $2, $3), body_plain = replace(body_plain, $2, $3)
		WHERE campaign_id = $1
	`
	for from, to := range rewrites {
		if from == "" || from == to {
			continue
		}
		for _, q := range []string{steps, variants} {
			if _, err := tx.Exec(ctx, q, campaignID, from, to); err != nil {
				db.CaptureError(err, q, []any{campaignID}, "exec")
				return err
			}
		}
	}
	return nil
}

// stepForeignRefs are the action settings that name a row of the workspace
// the step was written in.
var stepForeignRefs = []string{
	"category_id", "segment_id", "label_ids",
	"task_assigned_to", "task_assigned_team_id",
	"deal_pipeline_id", "deal_stage_id",
	"automation_id",
	"ai_add_tags", "ai_remove_tags", "ai_labels",
}

// aiActionNeeds is what an AI agent step's tool needs pinned. A tool whose
// setting was cleared is withdrawn rather than left to run unrestricted.
var aiActionNeeds = map[string][]string{
	"add_tag":         {"category_id", "ai_add_tags"},
	"remove_tag":      {"category_id", "ai_remove_tags"},
	"label_email":     {"label_ids", "ai_labels"},
	"create_deal":     {"deal_pipeline_id", "deal_stage_id"},
	"move_deal_stage": {"deal_pipeline_id", "deal_stage_id"},
}

// ScrubForeignStepReferences removes every setting of a step's action config
// that names a row of the source workspace, and withdraws any AI agent tool
// that depended on one, so a copy never acts on another workspace's tags,
// segments, pipelines, automations or members. It edits the generic JSON so
// settings the Go struct does not know about survive. It returns the config
// and the setting names it removed.
func ScrubForeignStepReferences(action []byte) ([]byte, []string, error) {
	if len(action) == 0 {
		return []byte(`{}`), nil, nil
	}
	var root map[string]any
	if err := json.Unmarshal(action, &root); err != nil {
		return nil, nil, err
	}
	if root == nil {
		return []byte(`{}`), nil, nil
	}
	var cleared []string
	for _, key := range stepForeignRefs {
		v, ok := root[key]
		if !ok {
			continue
		}
		delete(root, key)
		if setValue(v) {
			cleared = append(cleared, key)
		}
	}
	if allowed, ok := root["ai_allowed_actions"].([]any); ok && len(cleared) > 0 {
		kept := make([]any, 0, len(allowed))
		for _, a := range allowed {
			name, _ := a.(string)
			if needs, gated := aiActionNeeds[name]; gated && slices.ContainsFunc(needs, func(f string) bool { return slices.Contains(cleared, f) }) {
				continue
			}
			kept = append(kept, a)
		}
		if len(kept) != len(allowed) {
			root["ai_allowed_actions"] = kept
			cleared = append(cleared, "ai_allowed_actions")
		}
	}
	out, err := json.Marshal(root)
	if err != nil {
		return nil, nil, err
	}
	return out, cleared, nil
}

// setValue reports whether a JSON value carries a setting rather than an
// empty placeholder.
func setValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	}
	return true
}
