package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// ClientConnectLinkRepository stores client connect links. Every dashboard read and
// write is scoped by organization; only ByTokenHash is not, because the token is the capability.
type ClientConnectLinkRepository interface {
	Create(ctx context.Context, link *models.ClientConnectLink) (*models.ClientConnectLink, *errx.Error)
	List(ctx context.Context, orgID uuid.UUID, afterAt time.Time, afterID uuid.UUID, limit int) ([]models.ClientConnectLink, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.ClientConnectLink, *errx.Error)
	// Revoke stamps revoked_at once; a repeat returns the link unchanged.
	Revoke(ctx context.Context, orgID, id uuid.UUID) (*models.ClientConnectLink, *errx.Error)
	// ByTokenHash returns nil, nil for a hash no link carries.
	ByTokenHash(ctx context.Context, hash []byte) (*models.ClientConnectLink, *errx.Error)
	// Reserve takes one mailbox slot on a live link; false when it is revoked, expired or full.
	Reserve(ctx context.Context, orgID, id uuid.UUID) (bool, *errx.Error)
	// Release gives back a slot whose connect failed.
	Release(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	// ReserveMany takes n slots at once, all or none; ReleaseMany gives n back.
	ReserveMany(ctx context.Context, orgID, id uuid.UUID, n int) (bool, *errx.Error)
	ReleaseMany(ctx context.Context, orgID, id uuid.UUID, n int) *errx.Error
	RecordUse(ctx context.Context, use *models.ClientConnectLinkUse, orgID uuid.UUID, ipHash string) *errx.Error
	ListUses(ctx context.Context, orgID, linkID uuid.UUID, limit int) ([]models.ClientConnectLinkUse, *errx.Error)
}

type clientConnectLinkRepository struct {
	DB *db.DB
}

func NewClientConnectLinkRepository(d *db.DB) ClientConnectLinkRepository {
	return &clientConnectLinkRepository{DB: d}
}

const clientConnectLinkColumns = `l.id, l.organization_id, l.created_by, l.label, l.token_hash, l.allowed_providers,
	l.max_mailboxes, l.mailboxes_connected, l.tag_ids, l.warmup, l.expires_at, l.revoked_at, l.created_at,
	(SELECT max(u.connected_at) FROM client_connect_link_uses u WHERE u.link_id = l.id)`

func scanClientConnectLink(row pgx.Row) (*models.ClientConnectLink, error) {
	var l models.ClientConnectLink
	if err := row.Scan(&l.ID, &l.OrganizationID, &l.CreatedBy, &l.Label, &l.TokenHash, &l.AllowedProviders,
		&l.MaxMailboxes, &l.MailboxesConnected, &l.TagIDs, &l.Warmup, &l.ExpiresAt, &l.RevokedAt, &l.CreatedAt,
		&l.LastUsedAt); err != nil {
		return nil, err
	}
	if l.TagIDs == nil {
		l.TagIDs = []uuid.UUID{}
	}
	return &l, nil
}

func (r *clientConnectLinkRepository) one(ctx context.Context, op, query string, args ...any) (*models.ClientConnectLink, *errx.Error) {
	l, err := scanClientConnectLink(r.DB.QueryRow(ctx, query, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, op, nil, "queryrow")
		return nil, errx.InternalError()
	}
	return l, nil
}

func (r *clientConnectLinkRepository) Create(ctx context.Context, link *models.ClientConnectLink) (*models.ClientConnectLink, *errx.Error) {
	// Tags are kept only when they are the workspace's own.
	l, xerr := r.one(ctx, "connect links create", `
		WITH l AS (
			INSERT INTO client_connect_links (organization_id, created_by, label, token_hash, allowed_providers,
				max_mailboxes, tag_ids, warmup, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6,
				COALESCE((SELECT array_agg(t.id) FROM tags t WHERE t.organization_id = $1 AND t.id = ANY($7::uuid[])), '{}'),
				$8, $9)
			RETURNING *
		)
		SELECT `+clientConnectLinkColumns+` FROM l`,
		link.OrganizationID, link.CreatedBy, link.Label, link.TokenHash, link.AllowedProviders,
		link.MaxMailboxes, link.TagIDs, link.Warmup, link.ExpiresAt)
	if xerr == nil && l == nil {
		return nil, errx.InternalError()
	}
	return l, xerr
}

func (r *clientConnectLinkRepository) List(ctx context.Context, orgID uuid.UUID, afterAt time.Time, afterID uuid.UUID, limit int) ([]models.ClientConnectLink, *errx.Error) {
	const query = `SELECT ` + clientConnectLinkColumns + `
		FROM client_connect_links l
		WHERE l.organization_id = $1
		  AND ($2::uuid IS NULL OR (l.created_at, l.id) < ($3, $2::uuid))
		ORDER BY l.created_at DESC, l.id DESC
		LIMIT $4`
	var after *uuid.UUID
	if afterID != uuid.Nil {
		after = &afterID
	}
	rows, err := r.DB.Query(ctx, query, orgID, after, afterAt, limit)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.ClientConnectLink{}
	for rows.Next() {
		l, err := scanClientConnectLink(rows)
		if err != nil {
			db.CaptureError(err, query, []any{orgID}, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, *l)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, []any{orgID}, "rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

func (r *clientConnectLinkRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.ClientConnectLink, *errx.Error) {
	return r.one(ctx, "connect links get", `SELECT `+clientConnectLinkColumns+`
		FROM client_connect_links l WHERE l.organization_id = $1 AND l.id = $2`, orgID, id)
}

func (r *clientConnectLinkRepository) Revoke(ctx context.Context, orgID, id uuid.UUID) (*models.ClientConnectLink, *errx.Error) {
	if _, err := r.DB.Exec(ctx, `UPDATE client_connect_links SET revoked_at = now()
		WHERE organization_id = $1 AND id = $2 AND revoked_at IS NULL`, orgID, id); err != nil {
		db.CaptureError(err, "connect links revoke", []any{orgID}, "exec")
		return nil, errx.InternalError()
	}
	return r.Get(ctx, orgID, id)
}

func (r *clientConnectLinkRepository) ByTokenHash(ctx context.Context, hash []byte) (*models.ClientConnectLink, *errx.Error) {
	return r.one(ctx, "connect links by token", `SELECT `+clientConnectLinkColumns+`
		FROM client_connect_links l WHERE l.token_hash = $1`, hash)
}

func (r *clientConnectLinkRepository) Reserve(ctx context.Context, orgID, id uuid.UUID) (bool, *errx.Error) {
	tag, err := r.DB.Exec(ctx, `UPDATE client_connect_links SET mailboxes_connected = mailboxes_connected + 1
		WHERE organization_id = $1 AND id = $2 AND revoked_at IS NULL AND created_by IS NOT NULL
		  AND expires_at > now() AND mailboxes_connected < max_mailboxes`, orgID, id)
	if err != nil {
		db.CaptureError(err, "connect links reserve", []any{orgID}, "exec")
		return false, errx.InternalError()
	}
	return tag.RowsAffected() == 1, nil
}

func (r *clientConnectLinkRepository) Release(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	if _, err := r.DB.Exec(ctx, `UPDATE client_connect_links SET mailboxes_connected = mailboxes_connected - 1
		WHERE organization_id = $1 AND id = $2 AND mailboxes_connected > 0`, orgID, id); err != nil {
		db.CaptureError(err, "connect links release", []any{orgID}, "exec")
		return errx.InternalError()
	}
	return nil
}

func (r *clientConnectLinkRepository) ReserveMany(ctx context.Context, orgID, id uuid.UUID, n int) (bool, *errx.Error) {
	if n < 1 {
		return false, nil
	}
	tag, err := r.DB.Exec(ctx, `UPDATE client_connect_links SET mailboxes_connected = mailboxes_connected + $3
		WHERE organization_id = $1 AND id = $2 AND revoked_at IS NULL AND created_by IS NOT NULL
		  AND expires_at > now() AND mailboxes_connected + $3 <= max_mailboxes`, orgID, id, n)
	if err != nil {
		db.CaptureError(err, "connect links reserve many", []any{orgID}, "exec")
		return false, errx.InternalError()
	}
	return tag.RowsAffected() == 1, nil
}

func (r *clientConnectLinkRepository) ReleaseMany(ctx context.Context, orgID, id uuid.UUID, n int) *errx.Error {
	if n < 1 {
		return nil
	}
	if _, err := r.DB.Exec(ctx, `UPDATE client_connect_links SET mailboxes_connected = GREATEST(mailboxes_connected - $3, 0)
		WHERE organization_id = $1 AND id = $2`, orgID, id, n); err != nil {
		db.CaptureError(err, "connect links release many", []any{orgID}, "exec")
		return errx.InternalError()
	}
	return nil
}

func (r *clientConnectLinkRepository) RecordUse(ctx context.Context, use *models.ClientConnectLinkUse, orgID uuid.UUID, ipHash string) *errx.Error {
	err := r.DB.QueryRow(ctx, `
		INSERT INTO client_connect_link_uses (link_id, organization_id, email_account_id, email, provider, remote_ip_hash)
		SELECT l.id, l.organization_id, $3, $4, $5, $6 FROM client_connect_links l
		WHERE l.organization_id = $1 AND l.id = $2
		RETURNING id, connected_at`,
		orgID, use.LinkID, use.EmailAccountID, use.Email, use.Provider, ipHash).Scan(&use.ID, &use.ConnectedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return errx.ErrNotFound
	}
	if err != nil {
		db.CaptureError(err, "connect links record use", []any{orgID}, "queryrow")
		return errx.InternalError()
	}
	return nil
}

func (r *clientConnectLinkRepository) ListUses(ctx context.Context, orgID, linkID uuid.UUID, limit int) ([]models.ClientConnectLinkUse, *errx.Error) {
	const query = `SELECT id, link_id, email_account_id, email, provider, connected_at
		FROM client_connect_link_uses WHERE organization_id = $1 AND link_id = $2
		ORDER BY connected_at DESC LIMIT $3`
	rows, err := r.DB.Query(ctx, query, orgID, linkID, limit)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.ClientConnectLinkUse{}
	for rows.Next() {
		var u models.ClientConnectLinkUse
		if err := rows.Scan(&u.ID, &u.LinkID, &u.EmailAccountID, &u.Email, &u.Provider, &u.ConnectedAt); err != nil {
			db.CaptureError(err, query, []any{orgID}, "scan")
			return nil, errx.InternalError()
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, []any{orgID}, "rows")
		return nil, errx.InternalError()
	}
	return out, nil
}
