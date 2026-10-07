package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrRedirectTaken is returned when another workspace already serves a verified redirect for the domain.
var ErrRedirectTaken = errors.New("domain redirect already verified elsewhere")

// ErrRedirectOwned is returned when an upsert meets the workspace's row for the domain held by another owner (a linked instance, or none).
var ErrRedirectOwned = errors.New("domain redirect held by another owner")

// DomainRedirectRepository stores sending-domain redirects. Reads a caller can
// reach are scoped by organization, or by the linked instance on Cloud; Lookup
// serves the tracking service by host.
type DomainRedirectRepository interface {
	Upsert(ctx context.Context, r *models.DomainRedirect, createdBy *uuid.UUID) error
	// UpsertLinked is Upsert for a linked instance's row, refused (false) when a new one would pass limit.
	UpsertLinked(ctx context.Context, r *models.DomainRedirect, createdBy *uuid.UUID, limit int) (bool, error)
	// Get is the workspace's row for the domain, including one a linked instance owns.
	Get(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, error)
	// List is the workspace's own redirects; rows a linked instance owns are not the workspace's to show.
	List(ctx context.Context, orgID uuid.UUID) ([]models.DomainRedirect, error)
	Delete(ctx context.Context, orgID uuid.UUID, domain string) (bool, error)
	// SetCheck records a verification; ErrRedirectTaken when another workspace holds the verified domain.
	SetCheck(ctx context.Context, id uuid.UUID, verified bool, lastError string) error
	// SetReach records the last HTTP check of the domain; nil clears it.
	SetReach(ctx context.Context, id uuid.UUID, reach *models.RedirectReach) error
	// SetRemote mirrors Warmbly Cloud's host and records for a cloud-served row.
	SetRemote(ctx context.Context, id uuid.UUID, host string, records []models.DNSRecord) error
	// UnverifyCloudServed stops every cloud-served row claiming to be live, when the link to Cloud ends.
	UnverifyCloudServed(ctx context.Context, instanceID uuid.UUID, lastError string) error
	// CloudServedDomains are the domains this instance has Cloud serve, across workspaces.
	CloudServedDomains(ctx context.Context) (map[string]uuid.UUID, error)
	// CloudServedElsewhere reports another workspace on this instance having the domain served by Cloud.
	CloudServedElsewhere(ctx context.Context, orgID uuid.UUID, domain string) (bool, error)
	// Due are rows whose last check is older than their state allows.
	Due(ctx context.Context, limit int) ([]models.DomainRedirect, error)
	// Lookup is the target for a verified host this instance serves: the domain itself, or its www when included.
	Lookup(ctx context.Context, host string) (string, bool, error)

	// The Cloud side: rows a linked instance asked Cloud to serve.
	ListLinked(ctx context.Context, instanceID uuid.UUID) ([]models.DomainRedirect, error)
	GetLinked(ctx context.Context, instanceID uuid.UUID, domain string) (*models.DomainRedirect, error)
	DeleteLinked(ctx context.Context, instanceID uuid.UUID, domain string) (bool, error)
	CountLinked(ctx context.Context, instanceID uuid.UUID) (int, error)
}

type domainRedirectRepository struct{ DB *db.DB }

func NewDomainRedirectRepository(database *db.DB) DomainRedirectRepository {
	return &domainRedirectRepository{DB: database}
}

const redirectColumns = `id, organization_id, domain, target_url, include_www, verify_token, verified, verified_at,
	last_checked_at, last_error, created_at, created_by, served_by, remote_host, remote_records, linked_instance_id,
	reach_status, reach_hint, reach_detail, reach_proxy, reach_checked_at`

func scanRedirect(row pgx.Row, r *models.DomainRedirect) error {
	var (
		servedBy    string
		remote      []byte
		reachStatus *string
		reachHint   string
		reachDetail string
		reachProxy  string
		reach       = &models.RedirectReach{}
	)
	if err := row.Scan(&r.ID, &r.OrganizationID, &r.Domain, &r.TargetURL, &r.IncludeWWW, &r.VerifyToken, &r.Verified, &r.VerifiedAt,
		&r.LastCheckedAt, &r.LastError, &r.CreatedAt, &r.CreatedBy, &servedBy, &r.RemoteHost, &remote, &r.LinkedInstanceID,
		&reachStatus, &reachHint, &reachDetail, &reachProxy, &reach.CheckedAt); err != nil {
		return err
	}
	r.ServedBy = models.RedirectServer(servedBy)
	r.RemoteRecords = nil
	if len(remote) > 0 {
		_ = json.Unmarshal(remote, &r.RemoteRecords)
	}
	r.Reach = nil
	if reachStatus != nil {
		reach.Status, reach.Hint, reach.Detail, reach.Proxy = models.RedirectReachStatus(*reachStatus), models.RedirectReachHint(reachHint), reachDetail, reachProxy
		r.Reach = reach
	}
	return nil
}

func (r *domainRedirectRepository) Upsert(ctx context.Context, d *models.DomainRedirect, createdBy *uuid.UUID) error {
	return upsertRedirect(ctx, r.DB, d, createdBy)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func upsertRedirect(ctx context.Context, q queryRower, d *models.DomainRedirect, createdBy *uuid.UUID) error {
	servedBy := d.ServedBy
	if servedBy == "" {
		servedBy = models.RedirectServedByInstance
	}
	// Moving between servers drops what the old one said, its verdict included: the new server proves the domain itself.
	query := `
		INSERT INTO domain_redirects (id, organization_id, domain, target_url, include_www, verify_token, created_by, served_by, linked_instance_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (organization_id, domain) DO UPDATE
		SET target_url = EXCLUDED.target_url, include_www = EXCLUDED.include_www, served_by = EXCLUDED.served_by, updated_at = now(),
		    verified = domain_redirects.verified AND domain_redirects.served_by = EXCLUDED.served_by,
		    verified_at = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.verified_at END,
		    last_error = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.last_error ELSE '' END,
		    last_checked_at = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.last_checked_at END,
		    remote_host = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.remote_host ELSE '' END,
		    remote_records = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.remote_records END,
		    reach_status = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.reach_status END,
		    reach_hint = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.reach_hint ELSE '' END,
		    reach_detail = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.reach_detail ELSE '' END,
		    reach_proxy = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.reach_proxy ELSE '' END,
		    reach_checked_at = CASE WHEN domain_redirects.served_by = EXCLUDED.served_by THEN domain_redirects.reach_checked_at END,
		    cloud_link_instance_id = CASE WHEN EXCLUDED.served_by = 'cloud' AND domain_redirects.served_by = 'cloud' THEN domain_redirects.cloud_link_instance_id END
		WHERE domain_redirects.linked_instance_id IS NOT DISTINCT FROM EXCLUDED.linked_instance_id
		RETURNING ` + redirectColumns
	if err := scanRedirect(q.QueryRow(ctx, query, d.ID, d.OrganizationID, strings.ToLower(d.Domain), d.TargetURL, d.IncludeWWW, d.VerifyToken, createdBy,
		string(servedBy), d.LinkedInstanceID), d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRedirectOwned
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrRedirectTaken // another workspace already has Cloud serve the domain
		}
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	return nil
}

// UpsertLinked counts and writes under one lock per instance, so concurrent saves cannot pass the limit together.
func (r *domainRedirectRepository) UpsertLinked(ctx context.Context, d *models.DomainRedirect, createdBy *uuid.UUID, limit int) (bool, error) {
	if d.LinkedInstanceID == nil {
		return false, errors.New("linked redirect without an instance")
	}
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, d.LinkedInstanceID.String()); err != nil {
		db.CaptureError(err, "lock linked redirects", nil, "exec")
		return false, err
	}
	var exists bool
	var n int
	query := `SELECT EXISTS (SELECT 1 FROM domain_redirects WHERE linked_instance_id = $1 AND domain = lower($2)),
		(SELECT count(*) FROM domain_redirects WHERE linked_instance_id = $1)`
	if err := tx.QueryRow(ctx, query, *d.LinkedInstanceID, d.Domain).Scan(&exists, &n); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return false, err
	}
	if !exists && n >= limit {
		return false, nil
	}
	if err := upsertRedirect(ctx, tx, d, createdBy); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *domainRedirectRepository) one(ctx context.Context, query string, args ...any) (*models.DomainRedirect, error) {
	var d models.DomainRedirect
	if err := scanRedirect(r.DB.QueryRow(ctx, query, args...), &d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &d, nil
}

func (r *domainRedirectRepository) Get(ctx context.Context, orgID uuid.UUID, domain string) (*models.DomainRedirect, error) {
	return r.one(ctx, `SELECT `+redirectColumns+` FROM domain_redirects WHERE organization_id = $1 AND domain = lower($2)`, orgID, domain)
}

func (r *domainRedirectRepository) list(ctx context.Context, query string, args ...any) ([]models.DomainRedirect, error) {
	rows, err := r.DB.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := make([]models.DomainRedirect, 0)
	for rows.Next() {
		var d models.DomainRedirect
		if err := scanRedirect(rows, &d); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *domainRedirectRepository) List(ctx context.Context, orgID uuid.UUID) ([]models.DomainRedirect, error) {
	return r.list(ctx, `SELECT `+redirectColumns+` FROM domain_redirects
		WHERE organization_id = $1 AND linked_instance_id IS NULL ORDER BY domain`, orgID)
}

// Due re-checks pending and not-reaching rows every 10 minutes for two weeks, and every row every 6 hours.
func (r *domainRedirectRepository) Due(ctx context.Context, limit int) ([]models.DomainRedirect, error) {
	return r.list(ctx, `SELECT `+redirectColumns+` FROM domain_redirects
		WHERE last_checked_at IS NULL
		   OR (NOT verified AND last_checked_at < now() - interval '10 minutes' AND created_at > now() - interval '14 days')
		   OR (verified AND (reach_status IN ('not_reaching', 'https_error') OR reach_hint = 'settling')
		       AND last_checked_at < now() - interval '10 minutes' AND COALESCE(verified_at, created_at) > now() - interval '14 days')
		   OR last_checked_at < now() - interval '6 hours'
		ORDER BY last_checked_at NULLS FIRST
		LIMIT $1`, limit)
}

func (r *domainRedirectRepository) Delete(ctx context.Context, orgID uuid.UUID, domain string) (bool, error) {
	tag, err := r.DB.Exec(ctx, `DELETE FROM domain_redirects WHERE organization_id = $1 AND domain = lower($2) AND linked_instance_id IS NULL`, orgID, domain)
	if err != nil {
		db.CaptureError(err, "delete domain_redirects", nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *domainRedirectRepository) SetCheck(ctx context.Context, id uuid.UUID, verified bool, lastError string) error {
	query := `
		UPDATE domain_redirects
		SET verified = $2, last_error = $3, last_checked_at = now(), updated_at = now(),
		    verified_at = CASE WHEN $2 AND NOT verified THEN now() WHEN NOT $2 THEN NULL ELSE verified_at END
		WHERE id = $1`
	if _, err := r.DB.Exec(ctx, query, id, verified, lastError); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			_, _ = r.DB.Exec(ctx, `UPDATE domain_redirects SET last_checked_at = now(), last_error = $2 WHERE id = $1`, id,
				"Another workspace on this instance already redirects this domain.")
			return ErrRedirectTaken
		}
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *domainRedirectRepository) SetReach(ctx context.Context, id uuid.UUID, reach *models.RedirectReach) error {
	var (
		status              *string
		hint, detail, proxy string
		checkedAtExpr       = "NULL"
	)
	if reach != nil && knownReachStatus(reach.Status) {
		s := string(reach.Status)
		status, hint, detail, proxy, checkedAtExpr = &s, knownHint(reach.Hint), reach.Detail, knownProxy(reach.Proxy), "now()"
	}
	query := `UPDATE domain_redirects SET reach_status = $2, reach_hint = $3, reach_detail = $4, reach_proxy = $5, reach_checked_at = ` + checkedAtExpr + ` WHERE id = $1`
	if _, err := r.DB.Exec(ctx, query, id, status, hint, detail, proxy); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

// knownReachStatus and knownHint keep a verdict mirrored from a newer Cloud inside the columns' CHECKs.
func knownReachStatus(s models.RedirectReachStatus) bool {
	switch s {
	case models.RedirectReachOK, models.RedirectReachNotReaching, models.RedirectReachHTTPSError, models.RedirectReachUnreachable:
		return true
	}
	return false
}

func knownHint(h models.RedirectReachHint) string {
	switch h {
	case models.RedirectHintNotRouted, models.RedirectHintHostHeader, models.RedirectHintWrongTarget, models.RedirectHintCertificate,
		models.RedirectHintNoListener, models.RedirectHintSettling:
		return string(h)
	}
	return ""
}

// knownProxy keeps a mirrored proxy name inside the column's CHECK; an unknown one is dropped, not refused.
func knownProxy(p string) string {
	switch p {
	case "traefik", "nginx", "caddy", "apache", "cloudflare", "iis", "litespeed":
		return p
	}
	return ""
}

func (r *domainRedirectRepository) SetRemote(ctx context.Context, id uuid.UUID, host string, records []models.DNSRecord) error {
	raw, err := json.Marshal(records)
	if err != nil {
		return err
	}
	query := `UPDATE domain_redirects SET remote_host = $2, remote_records = $3 WHERE id = $1`
	if _, err := r.DB.Exec(ctx, query, id, host, raw); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *domainRedirectRepository) UnverifyCloudServed(ctx context.Context, instanceID uuid.UUID, lastError string) error {
	query := `UPDATE domain_redirects
		SET verified = false, verified_at = NULL, last_error = $1, updated_at = now(),
		    reach_status = NULL, reach_hint = '', reach_detail = '', reach_proxy = '', reach_checked_at = NULL, cloud_link_instance_id = NULL
		WHERE served_by = 'cloud' AND cloud_link_instance_id = $2`
	if _, err := r.DB.Exec(ctx, query, lastError, instanceID); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *domainRedirectRepository) CloudServedDomains(ctx context.Context) (map[string]uuid.UUID, error) {
	query := `SELECT domain, cloud_link_instance_id FROM domain_redirects WHERE served_by = 'cloud'`
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := map[string]uuid.UUID{}
	for rows.Next() {
		var d string
		var id *uuid.UUID
		if err := rows.Scan(&d, &id); err != nil {
			return nil, err
		}
		if id != nil {
			out[d] = *id
		}
	}
	return out, rows.Err()
}

func (r *domainRedirectRepository) CloudServedElsewhere(ctx context.Context, orgID uuid.UUID, domain string) (bool, error) {
	var ok bool
	query := `SELECT EXISTS (SELECT 1 FROM domain_redirects WHERE domain = lower($2) AND organization_id <> $1 AND served_by = 'cloud')`
	if err := r.DB.QueryRow(ctx, query, orgID, domain).Scan(&ok); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return false, err
	}
	return ok, nil
}

func (r *domainRedirectRepository) Lookup(ctx context.Context, host string) (string, bool, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	query := `
		SELECT target_url FROM domain_redirects
		WHERE verified AND served_by = 'instance' AND (domain = $1 OR (include_www AND 'www.' || domain = $1))
		LIMIT 1`
	var target string
	if err := r.DB.QueryRow(ctx, query, host).Scan(&target); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return "", false, err
	}
	return target, true, nil
}

func (r *domainRedirectRepository) ListLinked(ctx context.Context, instanceID uuid.UUID) ([]models.DomainRedirect, error) {
	return r.list(ctx, `SELECT `+redirectColumns+` FROM domain_redirects WHERE linked_instance_id = $1 ORDER BY domain`, instanceID)
}

func (r *domainRedirectRepository) GetLinked(ctx context.Context, instanceID uuid.UUID, domain string) (*models.DomainRedirect, error) {
	return r.one(ctx, `SELECT `+redirectColumns+` FROM domain_redirects WHERE linked_instance_id = $1 AND domain = lower($2)`, instanceID, domain)
}

func (r *domainRedirectRepository) DeleteLinked(ctx context.Context, instanceID uuid.UUID, domain string) (bool, error) {
	tag, err := r.DB.Exec(ctx, `DELETE FROM domain_redirects WHERE linked_instance_id = $1 AND domain = lower($2)`, instanceID, domain)
	if err != nil {
		db.CaptureError(err, "delete linked domain_redirects", nil, "exec")
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *domainRedirectRepository) CountLinked(ctx context.Context, instanceID uuid.UUID) (int, error) {
	var n int
	query := `SELECT count(*) FROM domain_redirects WHERE linked_instance_id = $1`
	if err := r.DB.QueryRow(ctx, query, instanceID).Scan(&n); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return 0, err
	}
	return n, nil
}
