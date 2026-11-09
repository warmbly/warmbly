package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// ConnectLinkClaimStale is how long a claimed round trip holds a link before it may be revoked.
const ConnectLinkClaimStale = 15 * time.Minute

// ErrConnectLinkNotRecorded is a completion for a link that is no longer the claim it was.
var ErrConnectLinkNotRecorded = errors.New("sign-in link completion was not recorded")

// ConnectLinkCursor pages links newest first.
type ConnectLinkCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// MailboxConnectLinkRepository stores sign-in links; only the SHA-256 of a link's secret is kept.
type MailboxConnectLinkRepository interface {
	CreateConnectLink(ctx context.Context, link *models.MailboxConnectLink, tokenHash []byte) (*models.MailboxConnectLink, error)
	// GetConnectLink returns nil, nil when the workspace has no such link.
	GetConnectLink(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxConnectLink, error)
	// GetConnectLinkByToken returns nil, nil for an unknown secret.
	GetConnectLinkByToken(ctx context.Context, tokenHash []byte) (*models.MailboxConnectLink, error)
	ListConnectLinks(ctx context.Context, orgID uuid.UUID, after *ConnectLinkCursor, limit int) ([]*models.MailboxConnectLink, error)
	// RevokeConnectLink ends a link that has not connected and no sign-in is completing; false when there was none to end.
	RevokeConnectLink(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	// ClaimConnectLink takes a pending link for one round trip; a claim is never reopened, so one link connects one mailbox.
	ClaimConnectLink(ctx context.Context, id uuid.UUID) (*models.MailboxConnectLink, error)
	// ReleaseConnectLink hands a claimed link back for another attempt, recording why this one failed.
	ReleaseConnectLink(ctx context.Context, id uuid.UUID, errorCode string) error
	// CompleteConnectLink records the connected mailbox; warning flags one that did not start warming.
	CompleteConnectLink(ctx context.Context, id, accountID uuid.UUID, warning string) error
}

type mailboxConnectLinkRepository struct {
	db *pgxpool.Pool
}

// NewMailboxConnectLinkRepository returns the Postgres sign-in link store.
func NewMailboxConnectLinkRepository(db *pgxpool.Pool) MailboxConnectLinkRepository {
	return &mailboxConnectLinkRepository{db: db}
}

const connectLinkColumns = `id, organization_id, created_by, provider, expected_email, warmup, return_url, status,
	error_code, email_account_id, expires_at, created_at, completed_at`

func scanConnectLink(row pgx.Row) (*models.MailboxConnectLink, error) {
	var l models.MailboxConnectLink
	var provider string
	var warmup []byte
	if err := row.Scan(&l.ID, &l.OrganizationID, &l.CreatedBy, &provider, &l.ExpectedEmail, &warmup, &l.ReturnURL,
		&l.Status, &l.ErrorCode, &l.EmailAccountID, &l.ExpiresAt, &l.CreatedAt, &l.CompletedAt); err != nil {
		return nil, err
	}
	l.Provider = models.InboxProvider(provider)
	if len(warmup) > 0 {
		if err := json.Unmarshal(warmup, &l.Warmup); err != nil {
			return nil, err
		}
	}
	return &l, nil
}

func noLink(l *models.MailboxConnectLink, err error) (*models.MailboxConnectLink, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return l, err
}

func (r *mailboxConnectLinkRepository) CreateConnectLink(ctx context.Context, link *models.MailboxConnectLink, tokenHash []byte) (*models.MailboxConnectLink, error) {
	warmup, err := json.Marshal(link.Warmup)
	if err != nil {
		return nil, err
	}
	return scanConnectLink(r.db.QueryRow(ctx, `
		INSERT INTO mailbox_connect_links (organization_id, created_by, provider, token_hash, expected_email, warmup, return_url, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+connectLinkColumns,
		link.OrganizationID, link.CreatedBy, string(link.Provider), tokenHash, link.ExpectedEmail, warmup, link.ReturnURL, link.ExpiresAt))
}

func (r *mailboxConnectLinkRepository) GetConnectLink(ctx context.Context, orgID, id uuid.UUID) (*models.MailboxConnectLink, error) {
	return noLink(scanConnectLink(r.db.QueryRow(ctx, `SELECT `+connectLinkColumns+`
		FROM mailbox_connect_links WHERE organization_id = $1 AND id = $2`, orgID, id)))
}

func (r *mailboxConnectLinkRepository) GetConnectLinkByToken(ctx context.Context, tokenHash []byte) (*models.MailboxConnectLink, error) {
	return noLink(scanConnectLink(r.db.QueryRow(ctx, `SELECT `+connectLinkColumns+`
		FROM mailbox_connect_links WHERE token_hash = $1`, tokenHash)))
}

func (r *mailboxConnectLinkRepository) ListConnectLinks(ctx context.Context, orgID uuid.UUID, after *ConnectLinkCursor, limit int) ([]*models.MailboxConnectLink, error) {
	var rows pgx.Rows
	var err error
	if after == nil {
		rows, err = r.db.Query(ctx, `SELECT `+connectLinkColumns+`
			FROM mailbox_connect_links WHERE organization_id = $1
			ORDER BY created_at DESC, id DESC LIMIT $2`, orgID, limit)
	} else {
		rows, err = r.db.Query(ctx, `SELECT `+connectLinkColumns+`
			FROM mailbox_connect_links WHERE organization_id = $1 AND (created_at, id) < ($2, $3)
			ORDER BY created_at DESC, id DESC LIMIT $4`, orgID, after.CreatedAt, after.ID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.MailboxConnectLink{}
	for rows.Next() {
		l, err := scanConnectLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *mailboxConnectLinkRepository) RevokeConnectLink(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `UPDATE mailbox_connect_links SET status = 'revoked', updated_at = now()
		WHERE organization_id = $1 AND id = $2
		  AND (status = 'pending' OR (status = 'connecting' AND updated_at < now() - make_interval(secs => $3)))`, orgID, id, ConnectLinkClaimStale.Seconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *mailboxConnectLinkRepository) ClaimConnectLink(ctx context.Context, id uuid.UUID) (*models.MailboxConnectLink, error) {
	return noLink(scanConnectLink(r.db.QueryRow(ctx, `
		UPDATE mailbox_connect_links SET status = 'connecting', updated_at = now()
		WHERE id = $1 AND expires_at > now() AND status = 'pending'
		RETURNING `+connectLinkColumns, id)))
}

func (r *mailboxConnectLinkRepository) ReleaseConnectLink(ctx context.Context, id uuid.UUID, errorCode string) error {
	_, err := r.db.Exec(ctx, `UPDATE mailbox_connect_links SET status = 'pending', error_code = $2, updated_at = now()
		WHERE id = $1 AND status = 'connecting'`, id, errorCode)
	return err
}

func (r *mailboxConnectLinkRepository) CompleteConnectLink(ctx context.Context, id, accountID uuid.UUID, warning string) error {
	tag, err := r.db.Exec(ctx, `UPDATE mailbox_connect_links
		SET status = 'connected', error_code = $3, email_account_id = $2, completed_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'connecting'`, id, accountID, warning)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrConnectLinkNotRecorded
	}
	return nil
}
