package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// campaignOpenerArmsSQL lists every arm of every opening email step of the
// campaigns campaignsSQL selects: the entry step and any step that starts its
// own thread. Follow-ups that reply in the lead's thread are left out, since
// warmup has no lead thread to reply in. An arm is the step's own copy, unless
// its paused Original takes it out of the split, plus each active step-scoped
// A/B variant; a variant with no body of its own sends the step's.
func campaignOpenerArmsSQL(campaignsSQL string) string {
	return `
		steps AS (
			SELECT s.id, s.campaign_id, s.name, s.position, s.subject, s.body_html, s.body_plain, s.thread_reply,
			       ROW_NUMBER() OVER (PARTITION BY s.campaign_id ORDER BY s.position, s.created_at, s.id) AS n
			FROM sequences s
			JOIN (` + campaignsSQL + `) camp ON camp.id = s.campaign_id
			WHERE s.kind IN ('', 'email')
		), openers AS (
			SELECT * FROM steps WHERE n = 1 OR NOT thread_reply
		), arms AS (
			SELECT o.campaign_id, o.id AS sequence_id, o.name::text AS step_name, o.position, NULL::uuid AS variant_id, ''::text AS variant_name,
			       o.subject, o.body_html, o.body_plain
			FROM openers o
			WHERE NOT EXISTS (SELECT 1 FROM campaign_ab_variants v WHERE v.sequence_id = o.id AND v.is_control AND NOT v.is_active)
			UNION ALL
			SELECT o.campaign_id, o.id, o.name::text, o.position, v.id, v.name,
			       COALESCE(NULLIF(btrim(v.subject), ''), o.subject),
			       CASE WHEN btrim(v.body_html) = '' AND btrim(v.body_plain) = '' THEN o.body_html ELSE v.body_html END,
			       CASE WHEN btrim(v.body_html) = '' AND btrim(v.body_plain) = '' THEN o.body_plain ELSE v.body_plain END
			FROM openers o
			JOIN campaign_ab_variants v ON v.sequence_id = o.id AND v.is_active AND NOT v.is_control
		)`
}

func (r *warmupContentRepository) CampaignCopyCandidates(ctx context.Context, orgID, senderID uuid.UUID, allowRepeat bool) ([]models.WarmupCampaignCopyCandidate, error) {
	active := `SELECT a.id FROM (` + activeCampaignsForAccountSQL + `) a JOIN campaigns c ON c.id = a.id AND c.organization_id = $2`
	query := `
		WITH ` + campaignOpenerArmsSQL(active) + `,
		sent AS (
			SELECT sequence_id, variant_id, MAX(created_at) AS last_sent
			FROM warmup_tokens
			WHERE sender_account_id = $1 AND content_source = 'campaign' AND created_at > NOW() - INTERVAL '30 days'
			GROUP BY 1, 2
		)
		SELECT a.campaign_id, a.sequence_id, a.variant_id, a.subject, a.body_html, a.body_plain, sent.last_sent
		FROM arms a
		LEFT JOIN sent ON sent.sequence_id = a.sequence_id AND sent.variant_id IS NOT DISTINCT FROM a.variant_id
		WHERE $3 OR sent.last_sent IS NULL OR sent.last_sent < NOW() - INTERVAL '1 day'
		ORDER BY sent.last_sent ASC NULLS FIRST, random()
		LIMIT 20`
	rows, err := r.db.Query(ctx, query, senderID, orgID, allowRepeat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WarmupCampaignCopyCandidate
	for rows.Next() {
		var c models.WarmupCampaignCopyCandidate
		if err := rows.Scan(&c.CampaignID, &c.SequenceID, &c.VariantID, &c.Subject, &c.BodyHTML, &c.BodyPlain, &c.LastSent); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// campaignCopyPlacementSQL is templatePlacementSQL for campaign copy: each
// content_source='campaign' send of the organization's steps since $2, joined
// to the receipt its recipient filed, by step, arm and recipient group.
func campaignCopyPlacementSQL() string {
	return `
		SELECT wt.sequence_id, wt.variant_id, ` + recipientGroupSQL("rec") + ` AS grp,
		       COUNT(*)::int AS sent,
		       COUNT(wr.email_account_id)::int AS reported,
		       COUNT(*) FILTER (WHERE wr.first_landing = 'inbox')::int AS inbox,
		       COUNT(*) FILTER (WHERE wr.first_landing = 'tabs')::int AS tabs,
		       COUNT(*) FILTER (WHERE wr.first_landing = 'spam' OR wr.landed_spam)::int AS spam,
		       COUNT(*) FILTER (WHERE wr.rescue_outcome IN ('moved', 'already_out'))::int AS rescued
		FROM warmup_tokens wt
		JOIN sequences s ON s.id = wt.sequence_id
		JOIN campaigns c ON c.id = s.campaign_id AND c.organization_id = $1
		JOIN email_accounts snd ON snd.id = wt.sender_account_id AND snd.organization_id = $1
		JOIN email_accounts rec ON rec.id = wt.recipient_account_id
		LEFT JOIN warmup_received wr ON wr.sender_account_id = wt.sender_account_id
		     AND wr.email_account_id = wt.recipient_account_id
		     AND btrim(wr.message_id, '<>') = btrim(wt.sent_message_id, '<>')
		WHERE wt.content_source = 'campaign' AND wt.sent_message_id <> '' AND wt.created_at >= $2
		GROUP BY 1, 2, 3`
}

type copyArmKey struct {
	sequenceID uuid.UUID
	variantID  uuid.UUID
}

func armKey(sequenceID uuid.UUID, variantID *uuid.UUID) copyArmKey {
	k := copyArmKey{sequenceID: sequenceID}
	if variantID != nil {
		k.variantID = *variantID
	}
	return k
}

func (r *warmupContentRepository) CampaignCopySteps(ctx context.Context, orgID uuid.UUID, since time.Time) ([]models.WarmupCampaignCopyStep, error) {
	rows, err := r.db.Query(ctx, campaignCopyPlacementSQL(), orgID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	placement := map[copyArmKey][]models.WarmupTemplateGroupPlacement{}
	for rows.Next() {
		var seq uuid.UUID
		var variant *uuid.UUID
		var g models.WarmupTemplateGroupPlacement
		if err := rows.Scan(&seq, &variant, &g.Group, &g.Sent, &g.Reported, &g.Inbox, &g.Tabs, &g.Spam, &g.Rescued); err != nil {
			return nil, err
		}
		k := armKey(seq, variant)
		placement[k] = append(placement[k], g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Every running campaign's openers, and any other campaign's that warmup carried in the window.
	listed := `SELECT c.id FROM campaigns c WHERE c.organization_id = $1 AND (c.status = 'active' OR EXISTS (
		SELECT 1 FROM sequences s JOIN warmup_tokens wt ON wt.sequence_id = s.id
		WHERE s.campaign_id = c.id AND wt.content_source = 'campaign' AND wt.created_at >= $2))`
	armRows, err := r.db.Query(ctx, `
		WITH `+campaignOpenerArmsSQL(listed)+`
		SELECT a.campaign_id, c.name, c.status, a.sequence_id, a.step_name, a.position, a.variant_id, a.variant_name,
		       a.subject, a.body_html, a.body_plain
		FROM arms a
		JOIN campaigns c ON c.id = a.campaign_id
		ORDER BY (c.status = 'active') DESC, c.created_at DESC, a.position, a.variant_id NULLS FIRST`, orgID, since)
	if err != nil {
		return nil, err
	}
	defer armRows.Close()
	out := []models.WarmupCampaignCopyStep{}
	for armRows.Next() {
		var s models.WarmupCampaignCopyStep
		if err := armRows.Scan(&s.CampaignID, &s.CampaignName, &s.CampaignStatus, &s.SequenceID, &s.StepName, &s.Position, &s.VariantID, &s.VariantName,
			&s.Subject, &s.BodyHTML, &s.BodyPlain); err != nil {
			return nil, err
		}
		s.ByGroup = placement[armKey(s.SequenceID, s.VariantID)]
		if s.ByGroup == nil {
			s.ByGroup = []models.WarmupTemplateGroupPlacement{}
		}
		s.Placement = models.MajorProviderPlacement(s.ByGroup)
		out = append(out, s)
	}
	return out, armRows.Err()
}
