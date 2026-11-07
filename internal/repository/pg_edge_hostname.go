package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
	// Wanted are the hostnames this instance serves for a workspace: verified tracking hosts and instance-served redirects with their www.
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
	db *pgxpool.Pool
}

func NewEdgeHostnameRepository(db *pgxpool.Pool) EdgeHostnameRepository {
	return &edgeHostnameRepository{db: db}
}

func (r *edgeHostnameRepository) Wanted(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT lower(tracking_domain) FROM email_accounts WHERE tracking_domain_verified AND tracking_domain <> ''
		UNION
		SELECT lower(tracking_domain) FROM campaigns WHERE tracking_domain_verified AND tracking_domain <> ''
		UNION
		SELECT domain FROM domain_redirects WHERE verified AND served_by = 'instance'
		UNION
		SELECT 'www.' || domain FROM domain_redirects WHERE verified AND served_by = 'instance' AND include_www
	`)
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
	return r.scan(ctx, `SELECT `+edgeHostnameColumns+` FROM edge_hostnames`)
}

func (r *edgeHostnameRepository) Get(ctx context.Context, hosts []string) ([]EdgeHostname, error) {
	if len(hosts) == 0 {
		return nil, nil
	}
	return r.scan(ctx, `SELECT `+edgeHostnameColumns+` FROM edge_hostnames WHERE hostname = ANY($1)`, hosts)
}

func (r *edgeHostnameRepository) Save(ctx context.Context, h EdgeHostname) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO edge_hostnames (hostname, zone, status, ownership_name, ownership_value, last_error, checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (hostname) DO UPDATE SET
			zone = EXCLUDED.zone, status = EXCLUDED.status, ownership_name = EXCLUDED.ownership_name, ownership_value = EXCLUDED.ownership_value,
			last_error = EXCLUDED.last_error, checked_at = now(), unwanted_since = NULL, updated_at = now()
	`, h.Hostname, h.Zone, h.Status, h.OwnershipName, h.OwnershipValue, h.LastError)
	return err
}

// Failed keeps the last verdict and its ownership record from the same zone; only a fresh answer replaces them.
func (r *edgeHostnameRepository) Failed(ctx context.Context, host, zone, reason string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO edge_hostnames (hostname, zone, last_error, checked_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (hostname) DO UPDATE SET
			last_error = EXCLUDED.last_error, checked_at = now(), unwanted_since = NULL, updated_at = now(),
			status = CASE WHEN edge_hostnames.zone = EXCLUDED.zone THEN edge_hostnames.status ELSE '' END,
			ownership_name = CASE WHEN edge_hostnames.zone = EXCLUDED.zone THEN edge_hostnames.ownership_name ELSE '' END,
			ownership_value = CASE WHEN edge_hostnames.zone = EXCLUDED.zone THEN edge_hostnames.ownership_value ELSE '' END,
			zone = EXCLUDED.zone
	`, host, zone, reason)
	return err
}

func (r *edgeHostnameRepository) MarkUnwanted(ctx context.Context, hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `UPDATE edge_hostnames SET unwanted_since = now(), updated_at = now() WHERE hostname = ANY($1) AND unwanted_since IS NULL`, hosts)
	return err
}

func (r *edgeHostnameRepository) DeleteUnwanted(ctx context.Context, host string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM edge_hostnames WHERE hostname = $1 AND unwanted_since IS NOT NULL`, host)
	return tag.RowsAffected() > 0, err
}
