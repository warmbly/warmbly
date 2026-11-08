package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// Cluster is a set of organizations that share something an unrelated set of
// customers would not.
type Cluster struct {
	// Key is the shared value, for the evidence record.
	Key string
	// OrganizationIDs are every member. A cluster of one is not a cluster and
	// is never returned.
	OrganizationIDs []uuid.UUID
}

// CorrelationRepository finds organizations linked across accounts.
//
// Every per-entity control in the platform watches one subject, so an actor
// spreading activity across several accounts sits under all of them. These
// queries are the view that sees the group.
type CorrelationRepository interface {
	// ClustersBySignupIP groups organizations whose owners signed up from the
	// same address. Private and loopback addresses are excluded: a self-hosted
	// install signing up over a LAN is the normal case, not a signal.
	ClustersBySignupIP(ctx context.Context, minMembers int, since time.Time) ([]Cluster, error)
	// ClustersBySignupIdentity groups organizations whose owners' addresses
	// collapse to the same person once plus-tags and Gmail dots are removed.
	ClustersBySignupIdentity(ctx context.Context, minMembers int, since time.Time) ([]Cluster, error)
	// OrgsConnectingMailboxesFast finds organizations that added an unusual
	// number of mailboxes in a short window, which is what setting up a
	// throwaway sending fleet looks like.
	OrgsConnectingMailboxesFast(ctx context.Context, minMailboxes int, within time.Duration) ([]Cluster, error)
}

type correlationRepository struct {
	DB *db.DB
}

func NewCorrelationRepository(database *db.DB) CorrelationRepository {
	return &correlationRepository{DB: database}
}

func (r *correlationRepository) ClustersBySignupIP(ctx context.Context, minMembers int, since time.Time) ([]Cluster, error) {
	return r.clusters(ctx, `
		SELECT host(u.signup_ip) AS key, array_agg(DISTINCT o.id) AS orgs
		  FROM organizations o
		  JOIN users u ON u.id = o.owner_user_id
		 WHERE u.signup_ip IS NOT NULL
		   -- A LAN or loopback signup is a self-hosted install, not a signal.
		   -- Both families: an IPv6-only LAN clusters just as wrongly.
		   -- <<= not <<: the strict operator excludes an address from a prefix
		   -- that describes exactly it, so ::1 is not "inside" ::1/128.
		   AND NOT (u.signup_ip <<= '10.0.0.0/8'::inet
		         OR u.signup_ip <<= '172.16.0.0/12'::inet
		         OR u.signup_ip <<= '192.168.0.0/16'::inet
		         OR u.signup_ip <<= '127.0.0.0/8'::inet
		         OR u.signup_ip <<= '169.254.0.0/16'::inet
		         OR u.signup_ip <<= '::1/128'::inet
		         OR u.signup_ip <<= 'fc00::/7'::inet
		         OR u.signup_ip <<= 'fe80::/10'::inet)
		   AND o.created_at >= $2
		   AND `+agencyClientExempt()+`
		 GROUP BY 1
		HAVING COUNT(DISTINCT o.id) >= $1
	`, minMembers, since)
}

func (r *correlationRepository) ClustersBySignupIdentity(ctx context.Context, minMembers int, since time.Time) ([]Cluster, error) {
	return r.clusters(ctx, `
		SELECT u.signup_email_normalized AS key, array_agg(DISTINCT o.id) AS orgs
		  FROM organizations o
		  JOIN users u ON u.id = o.owner_user_id
		 WHERE u.signup_email_normalized IS NOT NULL
		   AND u.signup_email_normalized <> ''
		   AND o.created_at >= $2
		   AND `+agencyClientExempt()+`
		 GROUP BY 1
		HAVING COUNT(DISTINCT o.id) >= $1
	`, minMembers, since)
}

// agencyClientExempt drops an agency's client workspaces from the signup
// clusters: one operator opening many of them is the product working. On a
// billed instance only a paying agency's clients are exempt.
func agencyClientExempt() string {
	billed := ""
	if config.BillingProvider() == "stripe" {
		billed = " AND a.status IN ('active', 'trialing', 'past_due')"
	}
	return `NOT EXISTS (SELECT 1 FROM agency_clients ac JOIN agencies a ON a.id = ac.agency_id
		WHERE ac.organization_id = o.id` + billed + `)`
}

func (r *correlationRepository) OrgsConnectingMailboxesFast(ctx context.Context, minMailboxes int, within time.Duration) ([]Cluster, error) {
	rows, err := r.DB.Pool.Query(ctx, `
		SELECT o.id::text, COUNT(*) AS n
		  FROM email_accounts ea
		  JOIN organizations o ON o.id = ea.organization_id
		 WHERE ea.created_at >= NOW() - $2::interval
		 GROUP BY o.id
		HAVING COUNT(*) >= $1
	`, minMailboxes, within.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Cluster
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		orgID, perr := uuid.Parse(id)
		if perr != nil {
			continue
		}
		out = append(out, Cluster{Key: id, OrganizationIDs: []uuid.UUID{orgID}})
	}
	return out, rows.Err()
}

func (r *correlationRepository) clusters(ctx context.Context, query string, minMembers int, since time.Time) ([]Cluster, error) {
	if minMembers < 2 {
		// A cluster of one is just an organization.
		minMembers = 2
	}
	rows, err := r.DB.Pool.Query(ctx, query, minMembers, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.Key, &c.OrganizationIDs); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
