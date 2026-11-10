package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EdgeKind is which edge zone a set of hostnames is registered on; each kind keeps its own table.
type EdgeKind string

const (
	// EdgeTracking is the zone in front of the tracking host: tracking domains and instance-served redirects.
	EdgeTracking EdgeKind = "tracking"
	// EdgePortal is the zone the portal is served from on Warmbly Cloud: verified agency portal domains.
	EdgePortal EdgeKind = "portal"
)

func (k EdgeKind) table() string {
	if k == EdgePortal {
		return "portal_edge_hostnames"
	}
	return "edge_hostnames"
}

// EdgeHostname is one customer hostname registered at the edge provider.
type EdgeHostname struct {
	Hostname       string
	Zone           string
	Status         string
	OwnershipName  string
	OwnershipValue string
	LastError      string
	CheckedAt      *time.Time
	UnwantedSince  *time.Time
}

// EdgeHostnameRepository keeps the edge provider's verdict on the customer hostnames this instance serves.
type EdgeHostnameRepository interface {
	Kind() EdgeKind
	// Wanted are the hostnames this kind's zone serves for a workspace: for tracking, verified tracking hosts and instance-served
	// redirects with their www; for portal, verified agency portal domains.
	Wanted(ctx context.Context) ([]string, error)
	List(ctx context.Context) ([]EdgeHostname, error)
	Get(ctx context.Context, hosts []string) ([]EdgeHostname, error)
	// Save records the provider's verdict and clears any removal clock.
	Save(ctx context.Context, h EdgeHostname) error
	// Failed records an attempt the provider did not answer, so the host waits its turn like a pending one.
	Failed(ctx context.Context, host, zone, reason string) error
	// MarkUnwanted starts the removal clock on hosts that do not have one.
	MarkUnwanted(ctx context.Context, hosts []string) error
	// DeleteUnwanted drops a host only while its removal clock still runs, reporting whether it did.
	DeleteUnwanted(ctx context.Context, host string) (bool, error)
}

type edgeHostnameRepository struct {
	db   *pgxpool.Pool
	kind EdgeKind
}

func NewEdgeHostnameRepository(db *pgxpool.Pool, kind EdgeKind) EdgeHostnameRepository {
	if kind != EdgePortal {
		kind = EdgeTracking
	}
	return &edgeHostnameRepository{db: db, kind: kind}
}

func (r *edgeHostnameRepository) Kind() EdgeKind { return r.kind }

const wantedTrackingSQL = `
	SELECT lower(tracking_domain) FROM email_accounts WHERE tracking_domain_verified AND tracking_domain <> ''
	UNION
	SELECT lower(tracking_domain) FROM campaigns WHERE tracking_domain_verified AND tracking_domain <> ''
	UNION
	SELECT domain FROM domain_redirects WHERE verified AND served_by = 'instance'
	UNION
	SELECT 'www.' || domain FROM domain_redirects WHERE verified AND served_by = 'instance' AND include_www
`

const wantedPortalSQL = `
	SELECT lower(portal_domain) FROM agency_brands WHERE portal_domain_verified AND portal_domain <> ''
`

func (r *edgeHostnameRepository) Wanted(ctx context.Context) ([]string, error) {
	query := wantedTrackingSQL
	if r.kind == EdgePortal {
		query = wantedPortalSQL
	}
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// sql names this kind's table in query; the table comes from a closed set, never from input.
func (r *edgeHostnameRepository) sql(query string) string { return fmt.Sprintf(query, r.kind.table()) }

const edgeHostnameColumns = `hostname, zone, status, ownership_name, ownership_value, last_error, checked_at, unwanted_since`

func (r *edgeHostnameRepository) scan(ctx context.Context, query string, args ...any) ([]EdgeHostname, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EdgeHostname
	for rows.Next() {
		var h EdgeHostname
		if err := rows.Scan(&h.Hostname, &h.Zone, &h.Status, &h.OwnershipName, &h.OwnershipValue, &h.LastError, &h.CheckedAt, &h.UnwantedSince); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *edgeHostnameRepository) List(ctx context.Context) ([]EdgeHostname, error) {
	return r.scan(ctx, r.sql(`SELECT `+edgeHostnameColumns+` FROM %[1]s`))
}

func (r *edgeHostnameRepository) Get(ctx context.Context, hosts []string) ([]EdgeHostname, error) {
	if len(hosts) == 0 {
		return nil, nil
	}
	return r.scan(ctx, r.sql(`SELECT `+edgeHostnameColumns+` FROM %[1]s WHERE hostname = ANY($1)`), hosts)
}

func (r *edgeHostnameRepository) Save(ctx context.Context, h EdgeHostname) error {
	_, err := r.db.Exec(ctx, r.sql(`
		INSERT INTO %[1]s (hostname, zone, status, ownership_name, ownership_value, last_error, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (hostname) DO UPDATE SET
			zone = EXCLUDED.zone, status = EXCLUDED.status, ownership_name = EXCLUDED.ownership_name, ownership_value = EXCLUDED.ownership_value,
			last_error = EXCLUDED.last_error, checked_at = now(), unwanted_since = NULL, updated_at = now()
	`), h.Hostname, h.Zone, h.Status, h.OwnershipName, h.OwnershipValue, h.LastError)
	return err
}

// Failed keeps the last verdict and its ownership record from the same zone; only a fresh answer replaces them.
func (r *edgeHostnameRepository) Failed(ctx context.Context, host, zone, reason string) error {
	_, err := r.db.Exec(ctx, r.sql(`
		INSERT INTO %[1]s (hostname, zone, last_error, checked_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (hostname) DO UPDATE SET
			last_error = EXCLUDED.last_error, checked_at = now(), unwanted_since = NULL, updated_at = now(),
			status = CASE WHEN %[1]s.zone = EXCLUDED.zone THEN %[1]s.status ELSE '' END,
			ownership_name = CASE WHEN %[1]s.zone = EXCLUDED.zone THEN %[1]s.ownership_name ELSE '' END,
			ownership_value = CASE WHEN %[1]s.zone = EXCLUDED.zone THEN %[1]s.ownership_value ELSE '' END,
			zone = EXCLUDED.zone
	`), host, zone, reason)
	return err
}

func (r *edgeHostnameRepository) MarkUnwanted(ctx context.Context, hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, r.sql(`UPDATE %[1]s SET unwanted_since = now(), updated_at = now() WHERE hostname = ANY($1) AND unwanted_since IS NULL`), hosts)
	return err
}

func (r *edgeHostnameRepository) DeleteUnwanted(ctx context.Context, host string) (bool, error) {
	tag, err := r.db.Exec(ctx, r.sql(`DELETE FROM %[1]s WHERE hostname = $1 AND unwanted_since IS NOT NULL`), host)
	return tag.RowsAffected() > 0, err
}
