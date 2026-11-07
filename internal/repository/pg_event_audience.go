package repository

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// resourceOrgTTL bounds how long a resource's organization is remembered; nothing moves a resource between organizations.
const (
	resourceOrgTTL     = time.Minute
	resourceOrgEntries = 50_000
)

type EventAudienceRepository struct {
	db *pgxpool.Pool

	mu   sync.Mutex
	orgs map[resourceKey]cachedOrg
}

type resourceKey struct {
	kind string
	id   uuid.UUID
}

type cachedOrg struct {
	org     uuid.UUID
	expires time.Time
}

func NewEventAudienceRepository(db *pgxpool.Pool) *EventAudienceRepository {
	return &EventAudienceRepository{db: db, orgs: map[resourceKey]cachedOrg{}}
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
	key := resourceKey{kind: kind, id: id}
	now := time.Now()
	r.mu.Lock()
	cached, ok := r.orgs[key]
	r.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.org, nil
	}
	var orgID uuid.UUID
	err := r.db.QueryRow(ctx, query, id).Scan(&orgID)
	if err == nil && orgID != uuid.Nil {
		r.mu.Lock()
		if len(r.orgs) >= resourceOrgEntries {
			clear(r.orgs)
		}
		r.orgs[key] = cachedOrg{org: orgID, expires: now.Add(resourceOrgTTL)}
		r.mu.Unlock()
	}
	return orgID, err
}

// CanAddressUser is never cached: removing a member stops their events at once.
func (r *EventAudienceRepository) CanAddressUser(ctx context.Context, orgID, userID uuid.UUID) (bool, error) {
	var allowed bool
	err := r.db.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM organization_members
		WHERE organization_id = $1 AND user_id = $2 AND accepted_at IS NOT NULL
			AND (role = 'owner' OR access_scope = 'workspace')
	)`, orgID, userID).Scan(&allowed)
	return allowed, err
}
