package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GuardrailCampaign is one active campaign with guardrails switched on,
// alongside the counts its thresholds are evaluated against. Thresholds and
// counts travel together so the sweep can decide without a second round trip
// per campaign.
type GuardrailCampaign struct {
	ID             uuid.UUID
	OrganizationID *uuid.UUID
	UserID         string
	Name           string

	BounceRateMax    float64
	ComplaintRateMax float64
	ReplyRateMin     float64
	MinSample        int
	WindowDays       int

	Sent       int
	Bounced    int
	Replied    int
	Complaints int
}

// GuardrailRepository reads the campaigns an auto-pause sweep has to consider
// and applies the pause when one breaches a band.
type GuardrailRepository interface {
	// ListGuardrailCampaigns returns active, guardrail-enabled campaigns with
	// their windowed engagement counts, newest first, capped at limit.
	ListGuardrailCampaigns(ctx context.Context, limit int) ([]GuardrailCampaign, error)
	// TripGuardrail pauses a campaign and records why. Returns false when the
	// campaign is no longer active, so a campaign someone paused or completed
	// between the read and the write is never reopened or overwritten.
	TripGuardrail(ctx context.Context, campaignID uuid.UUID, reason string) (bool, error)
	// ClearGuardrail wipes the tripped marker. Called when a campaign is
	// started again, so the badge does not outlive the pause it describes.
	ClearGuardrail(ctx context.Context, campaignID uuid.UUID) error
}

type guardrailRepository struct {
	db *pgxpool.Pool
}

func NewGuardrailRepository(db *pgxpool.Pool) GuardrailRepository {
	return &guardrailRepository{db: db}
}

func (r *guardrailRepository) ListGuardrailCampaigns(ctx context.Context, limit int) ([]GuardrailCampaign, error) {
	if limit <= 0 {
		limit = 500
	}

	// Counts are windowed by each campaign's own guardrail_window_days, with 0
	// meaning "the campaign's whole history". Bounces and replies are attributed
	// through campaign_contact_progress; complaints come from
	// deliverability_events, which is where ESP feedback loops land.
	query := `
		SELECT c.id, c.organization_id, c.user_id, c.name,
		       c.guardrail_bounce_rate_max, c.guardrail_complaint_rate_max, c.guardrail_reply_rate_min,
		       c.guardrail_min_sample, c.guardrail_window_days,
		       COALESCE(f.sent, 0), COALESCE(f.bounced, 0), COALESCE(f.replied, 0),
		       COALESCE(cx.complaints, 0)
		FROM campaigns c
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*)                                            AS sent,
				COUNT(*) FILTER (WHERE ccp.bounced_at IS NOT NULL)  AS bounced,
				COUNT(*) FILTER (WHERE ccp.replied_at IS NOT NULL)  AS replied
			FROM campaign_contact_progress ccp
			WHERE ccp.campaign_id = c.id
			  AND ccp.sent_at IS NOT NULL
			  AND (c.guardrail_window_days = 0
			       OR ccp.sent_at > NOW() - make_interval(days => c.guardrail_window_days))
		) f ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS complaints
			FROM deliverability_events de
			WHERE de.campaign_id = c.id
			  AND de.event_type = 'complaint'
			  AND (c.guardrail_window_days = 0
			       OR de.created_at > NOW() - make_interval(days => c.guardrail_window_days))
		) cx ON true
		WHERE c.guardrail_enabled AND c.status = 'active'
		ORDER BY c.created_at DESC
		LIMIT $1`

	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []GuardrailCampaign{}
	for rows.Next() {
		var g GuardrailCampaign
		if err := rows.Scan(
			&g.ID, &g.OrganizationID, &g.UserID, &g.Name,
			&g.BounceRateMax, &g.ComplaintRateMax, &g.ReplyRateMin,
			&g.MinSample, &g.WindowDays,
			&g.Sent, &g.Bounced, &g.Replied, &g.Complaints,
		); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *guardrailRepository) TripGuardrail(ctx context.Context, campaignID uuid.UUID, reason string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE campaigns
		SET status = 'paused_guardrail',
		    guardrail_tripped_at = now(),
		    guardrail_reason = $2,
		    last_status_change_at = now(),
		    updated_at = now()
		WHERE id = $1 AND status = 'active'`, campaignID, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *guardrailRepository) ClearGuardrail(ctx context.Context, campaignID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE campaigns
		SET guardrail_tripped_at = NULL, guardrail_reason = ''
		WHERE id = $1 AND (guardrail_tripped_at IS NOT NULL OR guardrail_reason <> '')`, campaignID)
	return err
}
