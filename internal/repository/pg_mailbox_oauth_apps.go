package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// ErrMailboxOAuthAppInUse refuses a change that would strand mailboxes whose grant the client issued.
var ErrMailboxOAuthAppInUse = errors.New("oauth app issued grants mailboxes still hold")

// MailboxOAuthAppRepository stores workspaces' own OAuth clients. ClientSecret is read and written sealed.
type MailboxOAuthAppRepository interface {
	ListMailboxOAuthApps(ctx context.Context, orgID uuid.UUID) ([]*models.MailboxOAuthApp, error)
	// GetMailboxOAuthApp returns nil, nil when the workspace has no client for the provider.
	GetMailboxOAuthApp(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (*models.MailboxOAuthApp, error)
	// GetMailboxOAuthAppByID returns nil, nil for an unknown id.
	GetMailboxOAuthAppByID(ctx context.Context, id uuid.UUID) (*models.MailboxOAuthApp, error)
	// UpsertMailboxOAuthApp refuses with ErrMailboxOAuthAppInUse a new client id while mailboxes hold grants from the old one.
	UpsertMailboxOAuthApp(ctx context.Context, app *models.MailboxOAuthApp) (*models.MailboxOAuthApp, error)
	// DeleteMailboxOAuthApp refuses with ErrMailboxOAuthAppInUse while mailboxes hold grants from the client.
	DeleteMailboxOAuthApp(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (bool, error)
}

type mailboxOAuthAppRepository struct {
	db *pgxpool.Pool
}

// NewMailboxOAuthAppRepository returns the Postgres OAuth client store.
func NewMailboxOAuthAppRepository(db *pgxpool.Pool) MailboxOAuthAppRepository {
	return &mailboxOAuthAppRepository{db: db}
}

const mailboxOAuthAppColumns = `a.id, a.organization_id, a.provider, a.client_id, a.client_secret, a.app_name, a.redirect_url,
	a.created_by, a.created_at, a.updated_at,
	(SELECT count(*) FROM email_accounts_oauth o WHERE o.oauth_app_id = a.id)`

func scanMailboxOAuthApp(row pgx.Row) (*models.MailboxOAuthApp, error) {
	var a models.MailboxOAuthApp
	var provider string
	if err := row.Scan(&a.ID, &a.OrganizationID, &provider, &a.ClientID, &a.ClientSecret, &a.AppName, &a.RedirectURL,
		&a.CreatedBy, &a.CreatedAt, &a.UpdatedAt, &a.Mailboxes); err != nil {
		return nil, err
	}
	a.Provider = models.InboxProvider(provider)
	a.CustomRedirect = a.RedirectURL != ""
	a.HasSecret = a.ClientSecret != ""
	return &a, nil
}

func (r *mailboxOAuthAppRepository) ListMailboxOAuthApps(ctx context.Context, orgID uuid.UUID) ([]*models.MailboxOAuthApp, error) {
	rows, err := r.db.Query(ctx, `SELECT `+mailboxOAuthAppColumns+`
		FROM organization_oauth_apps a WHERE a.organization_id = $1 ORDER BY a.provider`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	apps := []*models.MailboxOAuthApp{}
	for rows.Next() {
		a, err := scanMailboxOAuthApp(rows)
		if err != nil {
			return nil, err
		}
		apps = append(apps, a)
	}
	return apps, rows.Err()
}

func (r *mailboxOAuthAppRepository) GetMailboxOAuthApp(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (*models.MailboxOAuthApp, error) {
	a, err := scanMailboxOAuthApp(r.db.QueryRow(ctx, `SELECT `+mailboxOAuthAppColumns+`
		FROM organization_oauth_apps a WHERE a.organization_id = $1 AND a.provider = $2`, orgID, string(provider)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (r *mailboxOAuthAppRepository) GetMailboxOAuthAppByID(ctx context.Context, id uuid.UUID) (*models.MailboxOAuthApp, error) {
	a, err := scanMailboxOAuthApp(r.db.QueryRow(ctx, `SELECT `+mailboxOAuthAppColumns+`
		FROM organization_oauth_apps a WHERE a.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (r *mailboxOAuthAppRepository) UpsertMailboxOAuthApp(ctx context.Context, app *models.MailboxOAuthApp) (*models.MailboxOAuthApp, error) {
	// The update arm is skipped, and no row returned, when it would re-point grants at another client.
	a, err := scanMailboxOAuthApp(r.db.QueryRow(ctx, `
		WITH saved AS (
			INSERT INTO organization_oauth_apps (organization_id, provider, client_id, client_secret, app_name, redirect_url, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (organization_id, provider) DO UPDATE SET
				client_id = EXCLUDED.client_id,
				client_secret = EXCLUDED.client_secret,
				app_name = EXCLUDED.app_name,
				redirect_url = EXCLUDED.redirect_url,
				updated_at = now()
			WHERE organization_oauth_apps.client_id = EXCLUDED.client_id
			   OR NOT EXISTS (SELECT 1 FROM email_accounts_oauth o WHERE o.oauth_app_id = organization_oauth_apps.id)
			RETURNING *
		)
		SELECT `+mailboxOAuthAppColumns+` FROM saved a`,
		app.OrganizationID, string(app.Provider), app.ClientID, app.ClientSecret, app.AppName, app.RedirectURL, app.CreatedBy))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMailboxOAuthAppInUse
	}
	return a, err
}

func (r *mailboxOAuthAppRepository) DeleteMailboxOAuthApp(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM organization_oauth_apps WHERE organization_id = $1 AND provider = $2`, orgID, string(provider))
	if IsForeignKeyViolation(err) {
		return false, ErrMailboxOAuthAppInUse
	}
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
