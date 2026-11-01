package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

// WebsiteDomain is a website a workspace proved with its TXT record.
type WebsiteDomain struct {
	OrganizationID uuid.UUID
	Domain         string
	VerifiedAt     time.Time
	LastCheckedAt  time.Time
}

// WebsiteDomainRepository stores the websites a workspace proved; every read a caller reaches is scoped by organization.
type WebsiteDomainRepository interface {
	// Verified lists the workspace's proved websites.
	Verified(ctx context.Context, orgID uuid.UUID) ([]string, error)
	// Mark records a proof seen now, keeping the first verification time.
	Mark(ctx context.Context, orgID uuid.UUID, domain string) error
	// Touch records a check that could not tell, without claiming the proof was seen.
	Touch(ctx context.Context, orgID uuid.UUID, domain string) error
	// Drop forgets a website whose proof is gone.
	Drop(ctx context.Context, orgID uuid.UUID, domain string) error
	// Due are proofs last checked before the cutoff, oldest first.
	Due(ctx context.Context, before time.Time, limit int) ([]WebsiteDomain, error)
}

type websiteDomainRepository struct{ DB *db.DB }

func NewWebsiteDomainRepository(database *db.DB) WebsiteDomainRepository {
	return &websiteDomainRepository{DB: database}
}

func (r *websiteDomainRepository) Verified(ctx context.Context, orgID uuid.UUID) ([]string, error) {
	query := `SELECT domain FROM website_domains WHERE organization_id = $1 ORDER BY domain`
	rows, err := r.DB.Query(ctx, query, orgID)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *websiteDomainRepository) Mark(ctx context.Context, orgID uuid.UUID, domain string) error {
	query := `INSERT INTO website_domains (organization_id, domain) VALUES ($1, lower($2))
		ON CONFLICT (organization_id, domain) DO UPDATE SET last_checked_at = now()`
	if _, err := r.DB.Exec(ctx, query, orgID, domain); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *websiteDomainRepository) Touch(ctx context.Context, orgID uuid.UUID, domain string) error {
	query := `UPDATE website_domains SET last_checked_at = now() WHERE organization_id = $1 AND domain = lower($2)`
	if _, err := r.DB.Exec(ctx, query, orgID, domain); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *websiteDomainRepository) Drop(ctx context.Context, orgID uuid.UUID, domain string) error {
	query := `DELETE FROM website_domains WHERE organization_id = $1 AND domain = lower($2)`
	if _, err := r.DB.Exec(ctx, query, orgID, domain); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

func (r *websiteDomainRepository) Due(ctx context.Context, before time.Time, limit int) ([]WebsiteDomain, error) {
	query := `SELECT organization_id, domain, verified_at, last_checked_at FROM website_domains
		WHERE last_checked_at < $1 ORDER BY last_checked_at LIMIT $2`
	rows, err := r.DB.Query(ctx, query, before, limit)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []WebsiteDomain
	for rows.Next() {
		var w WebsiteDomain
		if err := rows.Scan(&w.OrganizationID, &w.Domain, &w.VerifiedAt, &w.LastCheckedAt); err != nil {
			db.CaptureError(err, query, nil, "scan")
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}
