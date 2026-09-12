package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
)

// CloudLinkRepository is the self-hosted side of pool link.
type CloudLinkRepository interface {
	// CanStore reports whether the instance token can be sealed at all. The
	// handshake is one-time: a token that cannot be written is gone, and the
	// link is left standing on the cloud with nobody holding it.
	CanStore() error
	Get(ctx context.Context) (*models.CloudLink, error)
	Put(ctx context.Context, link *models.CloudLink) error
	Delete(ctx context.Context) error
	SetSyncResult(ctx context.Context, at time.Time, lastError string) error

	Enroll(ctx context.Context, accountID, remoteID uuid.UUID, managed bool) (*models.CloudLinkMailbox, error)
	Unenroll(ctx context.Context, accountID uuid.UUID) error
	UnenrollAll(ctx context.Context) error
	GetByAccount(ctx context.Context, accountID uuid.UUID) (*models.CloudLinkMailbox, error)
	List(ctx context.Context) ([]models.CloudLinkMailbox, error)
	// IsEnrolled is the hot-path check the warmup task and reconciler use.
	IsEnrolled(ctx context.Context, accountID uuid.UUID) (bool, error)
}

type cloudLinkRepository struct {
	db      *pgxpool.Pool
	encrypt *encrypt.Encrypter
}

var errNoLinkEncrypter = errors.New("credential encrypter not configured (set CREDENTIALS_ENCRYPTION_KEY)")

// NewCloudLinkRepository seals the instance token with the mailbox credential key.
func NewCloudLinkRepository(db *pgxpool.Pool, enc *encrypt.Encrypter) CloudLinkRepository {
	return &cloudLinkRepository{db: db, encrypt: enc}
}

func (r *cloudLinkRepository) CanStore() error {
	if r.encrypt == nil {
		return errNoLinkEncrypter
	}
	return nil
}

func (r *cloudLinkRepository) Get(ctx context.Context) (*models.CloudLink, error) {
	query := `SELECT cloud_url, instance_id, token, organization_name, connected_by, connected_at, last_synced_at, last_error FROM cloud_link WHERE id = true`
	var l models.CloudLink
	var sealed string
	err := r.db.QueryRow(ctx, query).Scan(&l.CloudURL, &l.InstanceID, &sealed, &l.OrganizationName, &l.ConnectedBy, &l.ConnectedAt, &l.LastSyncedAt, &l.LastError)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	if r.encrypt == nil {
		return nil, errNoLinkEncrypter
	}
	plain, err := r.encrypt.Decrypt(sealed)
	if err != nil {
		return nil, err
	}
	l.Token = plain
	return &l, nil
}

func (r *cloudLinkRepository) Put(ctx context.Context, link *models.CloudLink) error {
	if r.encrypt == nil {
		return errNoLinkEncrypter
	}
	sealed, err := r.encrypt.Encrypt(link.Token)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO cloud_link (id, cloud_url, instance_id, token, organization_name, connected_by, connected_at, last_error)
		VALUES (true, $1, $2, $3, $4, $5, NOW(), '')
		ON CONFLICT (id) DO UPDATE SET
		  cloud_url = EXCLUDED.cloud_url, instance_id = EXCLUDED.instance_id, token = EXCLUDED.token,
		  organization_name = EXCLUDED.organization_name, connected_by = EXCLUDED.connected_by,
		  connected_at = NOW(), last_synced_at = NULL, last_error = ''
		RETURNING connected_at
	`
	if err := r.db.QueryRow(ctx, query, link.CloudURL, link.InstanceID, sealed, link.OrganizationName, link.ConnectedBy).Scan(&link.ConnectedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	return nil
}

func (r *cloudLinkRepository) Delete(ctx context.Context) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM cloud_link WHERE id = true`); err != nil {
		db.CaptureError(err, "delete cloud_link", nil, "exec")
		return err
	}
	return nil
}

func (r *cloudLinkRepository) SetSyncResult(ctx context.Context, at time.Time, lastError string) error {
	_, err := r.db.Exec(ctx, `UPDATE cloud_link SET last_synced_at = CASE WHEN $2 = '' THEN $1 ELSE last_synced_at END, last_error = $2 WHERE id = true`, at, lastError)
	return err
}

func (r *cloudLinkRepository) Enroll(ctx context.Context, accountID, remoteID uuid.UUID, managed bool) (*models.CloudLinkMailbox, error) {
	query := `
		INSERT INTO cloud_link_mailboxes (email_account_id, remote_id, managed)
		VALUES ($1, $2, $3)
		ON CONFLICT (email_account_id) DO UPDATE SET remote_id = EXCLUDED.remote_id, managed = EXCLUDED.managed
		RETURNING email_account_id, remote_id, enrolled_at, managed
	`
	var m models.CloudLinkMailbox
	if err := r.db.QueryRow(ctx, query, accountID, remoteID, managed).Scan(&m.EmailAccountID, &m.RemoteID, &m.EnrolledAt, &m.Managed); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return &m, nil
}

func (r *cloudLinkRepository) Unenroll(ctx context.Context, accountID uuid.UUID) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM cloud_link_mailboxes WHERE email_account_id = $1`, accountID); err != nil {
		db.CaptureError(err, "delete cloud_link_mailboxes", []any{accountID}, "exec")
		return err
	}
	return nil
}

func (r *cloudLinkRepository) UnenrollAll(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `DELETE FROM cloud_link_mailboxes`)
	return err
}

func (r *cloudLinkRepository) GetByAccount(ctx context.Context, accountID uuid.UUID) (*models.CloudLinkMailbox, error) {
	query := `SELECT email_account_id, remote_id, enrolled_at, managed FROM cloud_link_mailboxes WHERE email_account_id = $1`
	var m models.CloudLinkMailbox
	if err := r.db.QueryRow(ctx, query, accountID).Scan(&m.EmailAccountID, &m.RemoteID, &m.EnrolledAt, &m.Managed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, []any{accountID}, "queryrow")
		return nil, err
	}
	return &m, nil
}

func (r *cloudLinkRepository) List(ctx context.Context) ([]models.CloudLinkMailbox, error) {
	query := `SELECT email_account_id, remote_id, enrolled_at, managed FROM cloud_link_mailboxes ORDER BY enrolled_at`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.CloudLinkMailbox{}
	for rows.Next() {
		var m models.CloudLinkMailbox
		if err := rows.Scan(&m.EmailAccountID, &m.RemoteID, &m.EnrolledAt, &m.Managed); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *cloudLinkRepository) IsEnrolled(ctx context.Context, accountID uuid.UUID) (bool, error) {
	var ok bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cloud_link_mailboxes WHERE email_account_id = $1)`, accountID).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}
