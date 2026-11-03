package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// stepArmCountsSQL counts contacts per (step, arm). Every sent step of a contact
// bounds the next: an open, click or reply belongs to the latest step sent at or
// before it, whichever row the tracker stamped it on. Machine opens never count.
const stepArmCountsSQL = `
	WITH sends AS (
		SELECT contact_id, sequence_id, ab_arm, ab_variant_id, bounced_at,
		       LEAST(dispatched_at, sent_at) AS at,
		       LEAD(LEAST(dispatched_at, sent_at)) OVER (
		           PARTITION BY contact_id ORDER BY LEAST(dispatched_at, sent_at), sequence_id
		       ) AS next_at
		FROM campaign_contact_progress
		WHERE campaign_id = $1 AND sent_at IS NOT NULL
	),
	arm_contacts AS (
		SELECT s.sequence_id, s.ab_variant_id, s.bounced_at IS NOT NULL AS bounced,
		       bool_or(e.opened_at >= s.at AND NOT e.opened_machine AND (s.next_at IS NULL OR e.opened_at < s.next_at)) AS opened,
		       bool_or(e.clicked_at >= s.at AND (s.next_at IS NULL OR e.clicked_at < s.next_at)) AS clicked,
		       bool_or(e.replied_at >= s.at AND (s.next_at IS NULL OR e.replied_at < s.next_at)) AS replied
		FROM sends s
		JOIN campaign_contact_progress e ON e.campaign_id = $1 AND e.contact_id = s.contact_id
		WHERE s.ab_arm IS NOT NULL
		GROUP BY s.sequence_id, s.contact_id, s.ab_variant_id, s.bounced_at
	)
	SELECT sequence_id, ab_variant_id,
	       COUNT(*),
	       COUNT(*) FILTER (WHERE bounced),
	       COUNT(*) FILTER (WHERE opened AND NOT bounced),
	       COUNT(*) FILTER (WHERE clicked AND NOT bounced),
	       COUNT(*) FILTER (WHERE replied AND NOT bounced)
	FROM arm_contacts
	GROUP BY sequence_id, ab_variant_id
`

func (r *advancedOutreachRepository) GetStepArmCounts(ctx context.Context, campaignID uuid.UUID) ([]models.ABStepArmCounts, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, stepArmCountsSQL, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ABStepArmCounts
	for rows.Next() {
		var c models.ABStepArmCounts
		if err := rows.Scan(&c.SequenceID, &c.VariantID, &c.Sent, &c.Bounced, &c.Opened, &c.Clicked, &c.Replied); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *advancedOutreachRepository) ListABAutoPromoteCandidates(ctx context.Context, limit int) ([]models.ABAutoPromoteCandidate, error) {
	rows, err := resultDB(ctx, r.db).Query(ctx, `
		SELECT DISTINCT c.id, c.organization_id
		FROM campaign_ab_variants v
		JOIN campaigns c ON c.id = v.campaign_id
		WHERE v.sequence_id IS NOT NULL AND v.is_active AND NOT v.is_control
		  AND c.status = 'active' AND c.organization_id IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM campaign_ab_variants p
		      WHERE p.campaign_id = v.campaign_id AND p.sequence_id = v.sequence_id AND p.promoted_at IS NOT NULL
		  )
		ORDER BY c.id
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ABAutoPromoteCandidate
	for rows.Next() {
		var c models.ABAutoPromoteCandidate
		if err := rows.Scan(&c.CampaignID, &c.OrganizationID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *advancedOutreachRepository) PromoteStepWinner(ctx context.Context, campaignID, stepID uuid.UUID, winner *uuid.UUID) (bool, error) {
	tx, err := beginResultTx(ctx, r.db)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The row locks serialize two promoters and a teammate's edit to the same step.
	rows, err := tx.Query(ctx, `
		SELECT id, name, is_control, is_active, promoted_at IS NOT NULL
		FROM campaign_ab_variants
		WHERE campaign_id = $1 AND sequence_id = $2
		FOR UPDATE
	`, campaignID, stepID)
	if err != nil {
		return false, err
	}
	var controlID *uuid.UUID
	names := map[string]bool{}
	winnerActive, running := false, false
	for rows.Next() {
		var id uuid.UUID
		var name string
		var isControl, isActive, promoted bool
		if err := rows.Scan(&id, &name, &isControl, &isActive, &promoted); err != nil {
			rows.Close()
			return false, err
		}
		if promoted {
			rows.Close()
			return false, nil
		}
		names[name] = true
		switch {
		case isControl:
			controlID = &id
		case isActive:
			running = true
			if winner != nil && *winner == id {
				winnerActive = true
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	if !running || (winner != nil && !winnerActive) {
		return false, nil
	}

	// Every variant but the winner leaves the split.
	if _, err := tx.Exec(ctx, `
		UPDATE campaign_ab_variants
		SET is_active = (id IS NOT DISTINCT FROM $3::uuid),
		    promoted_at = CASE WHEN id IS NOT DISTINCT FROM $3::uuid THEN NOW() END,
		    updated_at = NOW()
		WHERE campaign_id = $1 AND sequence_id = $2 AND NOT is_control
		  AND (is_active OR id IS NOT DISTINCT FROM $3::uuid)
	`, campaignID, stepID, winner); err != nil {
		return false, err
	}

	// The Original's own row carries its state: paused when a variant won, marked when it won itself.
	originalWon := winner == nil
	if controlID != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE campaign_ab_variants
			SET is_active = $2, promoted_at = CASE WHEN $2 THEN NOW() ELSE NULL END, updated_at = NOW()
			WHERE id = $1
		`, *controlID, originalWon); err != nil {
			return false, err
		}
	} else {
		name := "Original"
		if names[name] {
			name = "Original (control)"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO campaign_ab_variants (campaign_id, sequence_id, name, weight, is_control, is_active, promoted_at, created_at, updated_at)
			VALUES ($1, $2, $3, 100, true, $4, CASE WHEN $4 THEN NOW() END, NOW(), NOW())
		`, campaignID, stepID, name, originalWon); err != nil {
			return false, err
		}
	}
	return true, tx.Commit(ctx)
}
