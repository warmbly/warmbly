package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// WarmupCampaignCopyRepository reads the campaign copy a sender's warmup can
// open threads with, and where that copy landed.
type WarmupCampaignCopyRepository interface {
	// CampaignCopyCandidates lists the opening-step arms of the sender's
	// running campaigns that it has not carried within a day and that the
	// recipient has not been sent within the repeat window, least recently
	// carried first.
	CampaignCopyCandidates(ctx context.Context, orgID, senderID, recipientID uuid.UUID) ([]models.WarmupCampaignCopyCandidate, error)
	// CampaignStepPlacement is where each arm of one campaign's steps landed in warmup since a time.
	CampaignStepPlacement(ctx context.Context, orgID, campaignID uuid.UUID, since time.Time) ([]models.WarmupStepPlacement, error)
}

// campaignOpenerArmsSQL lists every arm of every opening email step of the
// campaigns campaignsSQL selects: the entry step and any step that starts its
// own thread. Follow-ups that reply in the lead's thread are left out, since
// warmup has no lead thread to reply in. An arm is the step's own copy, unless
// its paused Original takes it out of the split, plus each active step-scoped
// A/B variant; a variant with no body of its own sends the step's.
func campaignOpenerArmsSQL(campaignsSQL string) string {
	return `
		steps AS (
			SELECT s.id, s.campaign_id, s.position, s.subject, s.body_html, s.body_plain, s.thread_reply,
			       ROW_NUMBER() OVER (PARTITION BY s.campaign_id ORDER BY s.position, s.created_at, s.id) AS n
			FROM sequences s
			JOIN (` + campaignsSQL + `) camp ON camp.id = s.campaign_id
			WHERE s.kind IN ('', 'email')
		), openers AS (
			SELECT * FROM steps WHERE n = 1 OR NOT thread_reply
		), arms AS (
			SELECT o.campaign_id, o.id AS sequence_id, NULL::uuid AS variant_id, o.subject, o.body_html, o.body_plain
			FROM openers o
			WHERE NOT EXISTS (SELECT 1 FROM campaign_ab_variants v WHERE v.sequence_id = o.id AND v.is_control AND NOT v.is_active)
			UNION ALL
			SELECT o.campaign_id, o.id, v.id,
			       COALESCE(NULLIF(btrim(v.subject), ''), o.subject),
			       CASE WHEN btrim(v.body_html) = '' AND btrim(v.body_plain) = '' THEN o.body_html ELSE v.body_html END,
			       CASE WHEN btrim(v.body_html) = '' AND btrim(v.body_plain) = '' THEN o.body_plain ELSE v.body_plain END
			FROM openers o
			JOIN campaign_ab_variants v ON v.sequence_id = o.id AND v.is_active AND NOT v.is_control
		)`
}

func (r *warmupContentRepository) CampaignCopyCandidates(ctx context.Context, orgID, senderID, recipientID uuid.UUID) ([]models.WarmupCampaignCopyCandidate, error) {
	active := `SELECT a.id FROM (` + activeCampaignsForAccountSQL + `) a JOIN campaigns c ON c.id = a.id AND c.organization_id = $2`
	// A prospect gets one arm of a step once, so a recipient is never sent the same step twice in the window.
	query := `
		WITH ` + campaignOpenerArmsSQL(active) + `,
		sent AS (
			SELECT sequence_id, variant_id, MAX(created_at) AS last_sent
			FROM warmup_tokens
			WHERE sender_account_id = $1 AND content_source = 'campaign' AND sequence_id IS NOT NULL AND created_at > NOW() - $4::interval
			GROUP BY 1, 2
		)
		SELECT a.campaign_id, a.sequence_id, a.variant_id, a.subject, a.body_html, a.body_plain
		FROM arms a
		LEFT JOIN sent ON sent.sequence_id = a.sequence_id AND sent.variant_id IS NOT DISTINCT FROM a.variant_id
		WHERE (sent.last_sent IS NULL OR sent.last_sent < NOW() - INTERVAL '1 day')
		  AND NOT EXISTS (SELECT 1 FROM warmup_tokens seen
		      WHERE seen.recipient_account_id = $3 AND seen.sequence_id = a.sequence_id AND seen.created_at > NOW() - $4::interval)
		ORDER BY sent.last_sent ASC NULLS FIRST, random()
		LIMIT 20`
	rows, err := r.db.Query(ctx, query, senderID, orgID, recipientID, WarmupContentRepeatWindow.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.WarmupCampaignCopyCandidate
	for rows.Next() {
		var c models.WarmupCampaignCopyCandidate
		if err := rows.Scan(&c.CampaignID, &c.SequenceID, &c.VariantID, &c.Subject, &c.BodyHTML, &c.BodyPlain); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *warmupContentRepository) CampaignStepPlacement(ctx context.Context, orgID, campaignID uuid.UUID, since time.Time) ([]models.WarmupStepPlacement, error) {
	// Each opening a sender of the organization carried for one of the campaign's steps, joined to the receipt its recipient filed.
	// Reported at business inboxes on Google Workspace and Microsoft 365 only, the inboxes the copy is written for.
	rows, err := r.db.Query(ctx, `
		SELECT wt.sequence_id, wt.variant_id, `+recipientGroupSQL("rec")+` AS grp,
		       COUNT(*)::int AS sent,
		       COUNT(wr.email_account_id)::int AS reported,
		       COUNT(*) FILTER (WHERE wr.first_landing = 'inbox')::int AS inbox,
		       COUNT(*) FILTER (WHERE wr.first_landing = 'tabs')::int AS tabs,
		       COUNT(*) FILTER (WHERE wr.first_landing = 'spam' OR wr.landed_spam)::int AS spam,
		       COUNT(*) FILTER (WHERE wr.rescue_outcome IN ('moved', 'already_out'))::int AS rescued
		FROM sequences s
		JOIN campaigns c ON c.id = s.campaign_id AND c.id = $2 AND c.organization_id = $1
		JOIN warmup_tokens wt ON wt.sequence_id = s.id AND wt.created_at >= $3
		JOIN email_accounts snd ON snd.id = wt.sender_account_id AND snd.organization_id = $1
		JOIN email_accounts rec ON rec.id = wt.recipient_account_id
		LEFT JOIN warmup_received wr ON wr.sender_account_id = wt.sender_account_id
		     AND wr.email_account_id = wt.recipient_account_id
		     AND btrim(wr.message_id, '<>') = btrim(wt.sent_message_id, '<>')
		WHERE wt.content_source = 'campaign' AND wt.conversation_turn = 0 AND wt.sent_message_id <> ''
		  AND `+recipientGroupSQL("rec")+` IN ('google', 'microsoft') AND rec.mail_host NOT IN ('gmail', 'outlook')
		  AND NOT `+consumerAddressSQL("rec")+`
		GROUP BY 1, 2, 3
		ORDER BY 1, 2 NULLS FIRST, 3`, orgID, campaignID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.WarmupStepPlacement{}
	for rows.Next() {
		var seq uuid.UUID
		var variant *uuid.UUID
		var g models.WarmupCopyGroupPlacement
		if err := rows.Scan(&seq, &variant, &g.Group, &g.Sent, &g.Reported, &g.Inbox, &g.Tabs, &g.Spam, &g.Rescued); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].SequenceID != seq || !sameVariant(out[n-1].VariantID, variant) {
			out = append(out, models.WarmupStepPlacement{SequenceID: seq, VariantID: variant})
		}
		last := &out[len(out)-1]
		last.ByGroup = append(last.ByGroup, g)
		last.Placement = models.TotalCopyPlacement(last.ByGroup)
	}
	return out, rows.Err()
}

func sameVariant(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
