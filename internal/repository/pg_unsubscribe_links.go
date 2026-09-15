package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UnsubscribeTicket is one minted short unsubscribe link: the email carries
// only the opaque token, this row says who it opts out.
type UnsubscribeTicket struct {
	Token          string
	OrganizationID uuid.UUID
	CampaignID     uuid.UUID
	// ContactID is the nil uuid for a test send, whose link is deliberately
	// tied to nobody.
	ContactID uuid.UUID
	ExpiresAt time.Time
}

// UnsubscribeLinkRepository is the store behind short unsubscribe links. Only
// the send pipeline writes; the recipient-facing route reads. There is no
// update path from any request-facing surface, and no delete: a link a
// recipient holds is a promise for as long as it says it is good for.
type UnsubscribeLinkRepository interface {
	// Mint returns the recipient's ticket for this campaign, creating it with
	// the given token on the first send and reusing it on every later one.
	// token is ignored when a row already exists, so every step of a sequence
	// carries the same address and only the expiry moves.
	Mint(ctx context.Context, token string, orgID, campaignID, contactID uuid.UUID, expiresAt time.Time) (string, error)
	// Resolve returns the ticket, or nil when the token is unknown. Expiry is
	// the caller's to judge, so an expired link can be told apart from an
	// invented one.
	Resolve(ctx context.Context, token string) (*UnsubscribeTicket, error)
}

type unsubscribeLinkRepository struct {
	db *pgxpool.Pool
}

func NewUnsubscribeLinkRepository(db *pgxpool.Pool) UnsubscribeLinkRepository {
	return &unsubscribeLinkRepository{db: db}
}

// Mint upserts on the recipient. A token that collides with an unrelated row
// raises a primary-key violation rather than handing out someone else's link,
// and the caller answers that by sending the signed long link instead.
func (r *unsubscribeLinkRepository) Mint(ctx context.Context, token string, orgID, campaignID, contactID uuid.UUID, expiresAt time.Time) (string, error) {
	query := `
		INSERT INTO unsubscribe_links (token, organization_id, campaign_id, contact_id, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (organization_id, campaign_id, contact_id)
		DO UPDATE SET expires_at = GREATEST(unsubscribe_links.expires_at, EXCLUDED.expires_at)
		RETURNING token
	`

	var stored string
	if err := r.db.QueryRow(ctx, query, token, orgID, campaignID, contactID, expiresAt).Scan(&stored); err != nil {
		return "", err
	}
	return stored, nil
}

func (r *unsubscribeLinkRepository) Resolve(ctx context.Context, token string) (*UnsubscribeTicket, error) {
	query := `
		SELECT token, organization_id, campaign_id, contact_id, expires_at
		FROM unsubscribe_links
		WHERE token = $1
	`

	var t UnsubscribeTicket
	err := r.db.QueryRow(ctx, query, token).Scan(&t.Token, &t.OrganizationID, &t.CampaignID, &t.ContactID, &t.ExpiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
