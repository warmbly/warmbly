package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventAudienceRepository struct {
	db *pgxpool.Pool
}

func NewEventAudienceRepository(db *pgxpool.Pool) *EventAudienceRepository {
	return &EventAudienceRepository{db: db}
}

func (r *EventAudienceRepository) ResourceOrganization(ctx context.Context, kind string, id uuid.UUID) (uuid.UUID, error) {
	var query string
	switch kind {
	case "campaign":
		query = `SELECT organization_id FROM campaigns WHERE id = $1`
	case "mailbox":
		query = `SELECT organization_id FROM email_accounts WHERE id = $1`
	case "task":
		query = `SELECT ea.organization_id FROM tasks t JOIN email_accounts ea ON ea.id = t.email_account_id WHERE t.id = $1`
	case "contact":
		query = `SELECT organization_id FROM contacts WHERE id = $1`
	case "booking":
		query = `SELECT organization_id FROM meeting_bookings WHERE id = $1`
	default:
		return uuid.Nil, fmt.Errorf("unsupported event resource kind %q", kind)
	}
	var orgID uuid.UUID
	err := r.db.QueryRow(ctx, query, id).Scan(&orgID)
	return orgID, err
}

func (r *EventAudienceRepository) CanAddressUser(ctx context.Context, orgID, userID uuid.UUID) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND accepted_at IS NOT NULL
			AND (role = 'owner' OR access_scope = 'workspace')
	)`, orgID, userID).Scan(&allowed)
	return allowed, err
}
