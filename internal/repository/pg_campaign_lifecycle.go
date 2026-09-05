package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// DuplicateCampaignInput describes one campaign copy. Attachments carry the
// source rows with S3Key already pointing at the copied object; rows for
// attachments whose bytes could not be copied are simply left out.
type DuplicateCampaignInput struct {
	SourceID    uuid.UUID
	NewID       uuid.UUID
	UserID      uuid.UUID
	Name        string
	Attachments []models.CampaignAttachment
	// OrganizationID and StorageLimitBytes make the copied attachments count
	// against the quota inside the same transaction that inserts them. A nil
	// limit skips the check.
	OrganizationID    uuid.UUID
	StorageLimitBytes *int64
}

// ErrStorageQuotaExceeded is returned by Duplicate when the copied attachments
// would take the organization past StorageLimitBytes. Nothing is written.
var ErrStorageQuotaExceeded = errors.New("storage quota exceeded")

// Delete removes a campaign and everything that only means something inside
// it. The pending tasks parked for the campaign (its wakeup chain and any
// not-yet-dispatched sends) go in the same transaction: campaign_tasks only
// nulls its link on delete, so without this the rows would still fire and
// then fail on a campaign that no longer exists. A wakeup tick that is
// executing right now is marked cancelled so it is not left claimed forever.
// Completed task rows stay, they back the sent mail already in the inbox.
func (r *campaignRepository) Delete(ctx context.Context, campaignID uuid.UUID) error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return err
	}
	defer tx.Rollback(ctx)

	const cancelPending = `
		DELETE FROM tasks
		WHERE status = 'pending'
		  AND id IN (SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)
	`
	if _, err := tx.Exec(ctx, cancelPending, campaignID); err != nil {
		db.CaptureError(err, cancelPending, []any{campaignID}, "exec")
		return err
	}
	const cancelClaimed = `
		UPDATE tasks SET status = 'cancelled', updated_at = NOW()
		WHERE status = 'active' AND task_type = 'campaign'
		  AND id IN (SELECT task_id FROM campaign_tasks WHERE campaign_id = $1)
	`
	if _, err := tx.Exec(ctx, cancelClaimed, campaignID); err != nil {
		db.CaptureError(err, cancelClaimed, []any{campaignID}, "exec")
		return err
	}

	const del = `DELETE FROM campaigns WHERE id = $1`
	cmd, err := tx.Exec(ctx, del, campaignID)
	if err != nil {
		db.CaptureError(err, del, []any{campaignID}, "exec")
		return err
	}
	if cmd.RowsAffected() == 0 {
		return errx.ErrResourceNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return err
	}
	return nil
}

// Duplicate copies a campaign's configuration into a fresh draft in one
// transaction: the campaign row, steps (with the branch graph rewritten onto
// the new step ids), tags, folders, sender pool, A/B variants, advanced
// settings and attachment rows. Execution state is never copied: no leads,
// progress, logs, daily counters, tasks, ramp level or guardrail trip. Dates
// already in the past are dropped so the copy does not finish the moment it
// starts.
func (r *campaignRepository) Duplicate(ctx context.Context, in DuplicateCampaignInput) (*models.Campaign, error) {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return nil, err
	}
	defer tx.Rollback(ctx)

	const copyCampaign = `
		INSERT INTO campaigns (
			id, user_id, organization_id, name, description, status,
			stop_on_reply, open_tracking, link_tracking, text_only,
			daily_limit, unsubscribe_header, risky_emails,
			cc_addr, bcc_addr,
			start_date, end_date, timezone, days, start_time, end_time, schedule_windows,
			contact_order_by, contact_order_dir, contact_order_field,
			sender_strategy, rotation_mode,
			ramp_enabled, ramp_start, ramp_increment, ramp_ceiling, ramp_level, ramp_level_date,
			esp_match_mode, max_new_leads_per_day, prioritize_new_leads,
			tracking_domain, tracking_domain_verified, tracking_domain_verified_at,
			guardrail_enabled, guardrail_bounce_rate_max, guardrail_complaint_rate_max,
			guardrail_reply_rate_min, guardrail_min_sample, guardrail_window_days,
			guardrail_tripped_at, guardrail_reason,
			utm_tracking, utm_source, utm_medium, utm_campaign,
			last_status_change_at, updated_at, created_at, kind
		)
		SELECT
			$2, $3, organization_id, $4, description, 'draft',
			stop_on_reply, open_tracking, link_tracking, text_only,
			daily_limit, unsubscribe_header, risky_emails,
			cc_addr, bcc_addr,
			CASE WHEN start_date > NOW() THEN start_date END,
			CASE WHEN end_date > NOW() THEN end_date END,
			timezone, days, start_time, end_time, schedule_windows,
			contact_order_by, contact_order_dir, contact_order_field,
			sender_strategy, rotation_mode,
			ramp_enabled, ramp_start, ramp_increment, ramp_ceiling, 0, NULL,
			esp_match_mode, max_new_leads_per_day, prioritize_new_leads,
			tracking_domain, tracking_domain_verified, tracking_domain_verified_at,
			guardrail_enabled, guardrail_bounce_rate_max, guardrail_complaint_rate_max,
			guardrail_reply_rate_min, guardrail_min_sample, guardrail_window_days,
			NULL, '',
			utm_tracking, utm_source, utm_medium, utm_campaign,
			NULL, NOW(), NOW(), kind
		FROM campaigns
		WHERE id = $1
	`
	cmd, err := tx.Exec(ctx, copyCampaign, in.SourceID, in.NewID, in.UserID, in.Name)
	if err != nil {
		db.CaptureError(err, copyCampaign, []any{in.SourceID, in.NewID}, "exec")
		return nil, err
	}
	if cmd.RowsAffected() == 0 {
		return nil, errx.ErrResourceNotFound
	}

	stepIDs, err := copyCampaignStepsTx(ctx, tx, in.SourceID, in.NewID)
	if err != nil {
		return nil, err
	}

	for _, q := range []string{
		`INSERT INTO campaign_email_tags (campaign_id, tag_id)
		 SELECT $2, tag_id FROM campaign_email_tags WHERE campaign_id = $1`,
		`INSERT INTO campaign_folders (campaign_id, folder_id)
		 SELECT $2, folder_id FROM campaign_folders WHERE campaign_id = $1`,
		`INSERT INTO campaign_senders (campaign_id, email_account_id, weight, enabled)
		 SELECT $2, email_account_id, weight, enabled FROM campaign_senders WHERE campaign_id = $1`,
		`INSERT INTO campaign_advanced_settings (campaign_id, settings, updated_at)
		 SELECT $2, settings, NOW() FROM campaign_advanced_settings WHERE campaign_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, in.SourceID, in.NewID); err != nil {
			db.CaptureError(err, q, []any{in.SourceID, in.NewID}, "exec")
			return nil, err
		}
	}

	if err := copyCampaignVariantsTx(ctx, tx, in.SourceID, in.NewID, stepIDs); err != nil {
		return nil, err
	}

	if len(in.Attachments) > 0 && in.StorageLimitBytes != nil {
		if err := LockStorageQuota(ctx, tx, in.OrganizationID); err != nil {
			db.CaptureError(err, "", nil, "exec")
			return nil, err
		}
		used, err := storageUsedTx(ctx, tx, in.OrganizationID)
		if err != nil {
			db.CaptureError(err, "", nil, "queryrow")
			return nil, err
		}
		var adding int64
		for _, att := range in.Attachments {
			adding += att.Size
		}
		if used+adding > *in.StorageLimitBytes {
			return nil, fmt.Errorf("%w: %d of %d bytes used, %d to add", ErrStorageQuotaExceeded, used, *in.StorageLimitBytes, adding)
		}
	}

	const insertAttachment = `
		INSERT INTO campaign_attachments (campaign_id, sequence_id, user_id, filename, size, mime_type, s3_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	for _, att := range in.Attachments {
		var seqID *uuid.UUID
		if att.SequenceID != nil {
			if mapped, ok := stepIDs[*att.SequenceID]; ok {
				seqID = &mapped
			}
		}
		args := []any{in.NewID, seqID, in.UserID, att.Filename, att.Size, att.MimeType, att.S3Key}
		if _, err := tx.Exec(ctx, insertAttachment, args...); err != nil {
			db.CaptureError(err, insertAttachment, args, "exec")
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return nil, err
	}

	campaign, err := r.GetByID(ctx, in.NewID)
	if err != nil {
		return nil, err
	}
	if campaign.Senders, err = r.GetCampaignSenders(ctx, in.NewID); err != nil {
		return nil, err
	}
	return campaign, nil
}

// copyCampaignStepsTx copies every step of src under dst with fresh ids and
// returns the old-to-new id map. Every new id is chosen before the first
// insert so each step's branch targets can be rewritten in the same statement.
func copyCampaignStepsTx(ctx context.Context, tx pgx.Tx, src, dst uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	const list = `
		SELECT id, conditions
		FROM sequences
		WHERE campaign_id = $1
		ORDER BY position ASC, created_at ASC
	`
	rows, err := tx.Query(ctx, list, src)
	if err != nil {
		db.CaptureError(err, list, []any{src}, "query")
		return nil, err
	}
	type step struct {
		id         uuid.UUID
		conditions []byte
	}
	var steps []step
	for rows.Next() {
		var s step
		if err := rows.Scan(&s.id, &s.conditions); err != nil {
			rows.Close()
			db.CaptureError(err, list, []any{src}, "scan")
			return nil, err
		}
		steps = append(steps, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	idMap := make(map[uuid.UUID]uuid.UUID, len(steps))
	for _, s := range steps {
		idMap[s.id] = uuid.New()
	}

	const copyStep = `
		INSERT INTO sequences (
			id, campaign_id, organization_id, name, subject,
			body_plain, body_html, body_sync, body_code,
			wait_after, position, conditions, kind, action, x, y,
			created_at, updated_at
		)
		SELECT
			$2, $3, organization_id, name, subject,
			body_plain, body_html, body_sync, body_code,
			wait_after, position, $4::jsonb, kind, action, x, y,
			NOW(), NOW()
		FROM sequences
		WHERE id = $1
	`
	for _, s := range steps {
		newID := idMap[s.id]
		conditions, err := RemapBranchTargets(s.conditions, idMap)
		if err != nil {
			return nil, fmt.Errorf("step %s conditions: %w", s.id, err)
		}
		if _, err := tx.Exec(ctx, copyStep, s.id, newID, dst, conditions); err != nil {
			db.CaptureError(err, copyStep, []any{s.id, newID, dst}, "exec")
			return nil, err
		}
	}
	return idMap, nil
}

// copyCampaignVariantsTx copies the A/B variants, pointing per-step variants
// at the copied step.
func copyCampaignVariantsTx(ctx context.Context, tx pgx.Tx, src, dst uuid.UUID, stepIDs map[uuid.UUID]uuid.UUID) error {
	const list = `SELECT id, sequence_id FROM campaign_ab_variants WHERE campaign_id = $1 ORDER BY created_at ASC`
	rows, err := tx.Query(ctx, list, src)
	if err != nil {
		db.CaptureError(err, list, []any{src}, "query")
		return err
	}
	type variant struct {
		id    uuid.UUID
		seqID *uuid.UUID
	}
	var variants []variant
	for rows.Next() {
		var v variant
		if err := rows.Scan(&v.id, &v.seqID); err != nil {
			rows.Close()
			db.CaptureError(err, list, []any{src}, "scan")
			return err
		}
		variants = append(variants, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	const copyVariant = `
		INSERT INTO campaign_ab_variants (
			campaign_id, sequence_id, name, weight, subject, body_html, body_plain,
			is_control, is_active, metadata, created_at, updated_at
		)
		SELECT $2, $3, name, weight, subject, body_html, body_plain,
		       is_control, is_active, metadata, NOW(), NOW()
		FROM campaign_ab_variants
		WHERE id = $1
	`
	for _, v := range variants {
		var seqID *uuid.UUID
		if v.seqID != nil {
			if mapped, ok := stepIDs[*v.seqID]; ok {
				seqID = &mapped
			}
		}
		if _, err := tx.Exec(ctx, copyVariant, v.id, dst, seqID); err != nil {
			db.CaptureError(err, copyVariant, []any{v.id, dst}, "exec")
			return err
		}
	}
	return nil
}

// RemapBranchTargets rewrites every branch target_step_id in a step's
// conditions jsonb through idMap. It edits the generic JSON rather than the
// typed BranchConditions so fields the editor stores that the Go struct does
// not know about survive the copy. A target with no mapping (a dangling
// reference to a deleted step) becomes null, which routing already treats as
// STOP.
func RemapBranchTargets(conditions []byte, idMap map[uuid.UUID]uuid.UUID) ([]byte, error) {
	if len(conditions) == 0 {
		return []byte(`{}`), nil
	}
	var root map[string]any
	if err := json.Unmarshal(conditions, &root); err != nil {
		return nil, err
	}
	if root == nil {
		return []byte(`{}`), nil
	}
	branches, _ := root["branches"].([]any)
	for _, b := range branches {
		branch, ok := b.(map[string]any)
		if !ok {
			continue
		}
		raw, present := branch["target_step_id"]
		if !present || raw == nil {
			continue
		}
		var target *uuid.UUID
		if s, ok := raw.(string); ok {
			if old, err := uuid.Parse(s); err == nil {
				if mapped, ok := idMap[old]; ok {
					target = &mapped
				}
			}
		}
		if target == nil {
			branch["target_step_id"] = nil
		} else {
			branch["target_step_id"] = target.String()
		}
	}
	return json.Marshal(root)
}
