package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/app/mailcred"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

func (r *emailRepository) GetSMTPIMAP(ctx context.Context, userID, emailAccountID string) (*models.SmtpImap, *errx.Error) {
	var imap models.Service
	var smtp models.Service
	var ts time.Time
	var orgID *uuid.UUID

	query := `
		SELECT
    	 ea.organization_id,
    	 smtp.smtp_host,
    	 smtp.smtp_port,
    	 smtp.smtp_user,
    	 smtp.smtp_password,
    	 smtp.smtp_security,
   		 smtp.imap_host,
    	 smtp.imap_port,
    	 smtp.imap_user,
    	 smtp.imap_password,
    	 smtp.imap_security,
    	 smtp.smtp_tls_unverified,
    	 COALESCE(smtp.smtp_tls_pin, ''),
    	 smtp.imap_tls_unverified,
    	 COALESCE(smtp.imap_tls_pin, ''),
    	 smtp.updated_at
	 	FROM 
    	 email_accounts ea
		JOIN 
    	 email_accounts_smtp_imap smtp ON ea.id = smtp.email_account_id
		WHERE 
     	 ea.user_id = $1
    	 AND ea.id = $2
	`

	params := []any{
		userID,
		emailAccountID,
	}

	err := r.DB.QueryRow(
		ctx,
		query,
		params...,
	).Scan(
		&orgID, &smtp.Host, &smtp.Port, &smtp.Username, &smtp.Password, &smtp.Security,
		&imap.Host, &imap.Port, &imap.Username, &imap.Password, &imap.Security,
		&smtp.TLSUnverified, &smtp.TLSPin, &imap.TLSUnverified, &imap.TLSPin,
		&ts,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}
	if r.Creds == nil {
		errs.CaptureException(mailcred.ErrNoKey)
		return nil, errx.InternalError()
	}
	if err := r.openCredentials(ctx, orgID, &smtp.Host, &smtp.Username, &smtp.Password, &imap.Host, &imap.Username, &imap.Password); err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &models.SmtpImap{
		SMTP: &smtp,
		IMAP: &imap,
	}, nil
}

func (r *emailRepository) RevokeOauth(ctx context.Context, id string) *errx.Error {
	tx, err := r.DB.Begin(ctx)
	if err != nil {
		db.CaptureError(err, "", nil, "begin")
		return errx.InternalError()
	}
	defer tx.Rollback(ctx)

	query := `
		UPDATE email_accounts 
		SET status = 'revoked' WHERE id = $1
	`

	params := []any{
		id,
	}
	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	query = `
		DELETE FROM email_accounts_oauth
		WHERE email_account_id = $1
	`

	params = []any{
		id,
	}

	_, err = tx.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return errx.InternalError()
	}

	if err := tx.Commit(ctx); err != nil {
		db.CaptureError(err, "", nil, "commit")
		return errx.InternalError()
	}

	return nil
}

func (r *emailRepository) RefreshBoxToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt time.Time) error {
	// Refreshed tokens land here from the worker's OnTokenRefresh callback and
	// must be sealed on the same terms as the ones written at connect time,
	// otherwise the first refresh silently reverts the row to plaintext.
	orgID, err := r.mailboxOrganization(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the mailbox is gone, so there is nothing to update
	}
	if err != nil {
		return err
	}
	sealed, err := r.sealCredentials(ctx, orgID, accessToken, refreshToken)
	if err != nil {
		db.CaptureError(err, "", nil, "encrypt-oauth-tokens")
		return err
	}
	encAccessToken, encRefreshToken := sealed[0], sealed[1]

	query := `
		UPDATE email_accounts_oauth
		SET access_token = $1, refresh_token = $2, expires_at = $3
		WHERE email_account_id = $4
	`

	params := []any{
		encAccessToken,
		encRefreshToken,
		expiresAt,
		id,
	}

	if _, err := r.DB.Exec(
		ctx,
		query,
		params...,
	); err != nil {
		// params holds the sealed tokens; keep them out of the error report.
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

// ErrNoOAuthRow is a token replacement for a mailbox that no longer signs in with OAuth.
var ErrNoOAuthRow = errors.New("mailbox has no OAuth sign-in to replace")

// ReplaceBoxToken is RefreshBoxToken for a new consent, which also records what it granted.
func (r *emailRepository) ReplaceBoxToken(ctx context.Context, id uuid.UUID, accessToken, refreshToken string, expiresAt time.Time, scopes []string) error {
	orgID, err := r.mailboxOrganization(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	sealed, err := r.sealCredentials(ctx, orgID, accessToken, refreshToken)
	if err != nil {
		db.CaptureError(err, "", nil, "encrypt-oauth-tokens")
		return err
	}
	query := `UPDATE email_accounts_oauth
		SET access_token = $1, refresh_token = $2, expires_at = $3, granted_scopes = $4, updated_at = now()
		WHERE email_account_id = $5`
	tag, err := r.DB.Exec(ctx, query, sealed[0], sealed[1], expiresAt, scopes, id)
	if err != nil {
		// The parameters hold the sealed tokens; keep them out of the error report.
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	// A mailbox whose sign-in changed meanwhile has no token row; the new consent must not read as stored.
	if tag.RowsAffected() == 0 {
		return ErrNoOAuthRow
	}
	return nil
}

func (r *emailRepository) RefreshBoxTokenSealed(ctx context.Context, id uuid.UUID, sealed string, expiresAt time.Time) error {
	orgID, err := r.mailboxOrganization(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if orgID == nil {
		return errors.New("sealed token for a mailbox with no organization")
	}
	c, err := r.Creds.Cipher(ctx, *orgID)
	if err != nil {
		return err
	}
	v, err := mailcred.OpenWorkerSecrets(ctx, c, sealed)
	if err != nil {
		return err
	}
	if v.Token == nil {
		return errors.New("sealed token update carries no token")
	}
	return r.RefreshBoxToken(ctx, id, v.Token.AccessToken, v.Token.RefreshToken, expiresAt)
}

func (r *emailRepository) ReplaceSMTPIMAPCredentials(ctx context.Context, id uuid.UUID, creds *models.SmtpImap) error {
	if creds == nil || creds.SMTP == nil || creds.IMAP == nil {
		return errors.New("smtp and imap credentials are required")
	}
	orgID, err := r.mailboxOrganization(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // the mailbox is gone, so there is nothing to update
	}
	if err != nil {
		return err
	}
	sealed, err := r.sealCredentials(ctx, orgID, creds.SMTP.Host, creds.SMTP.Username, creds.SMTP.Password, creds.IMAP.Host, creds.IMAP.Username, creds.IMAP.Password)
	if err != nil {
		db.CaptureError(err, "", nil, "encrypt-smtp-imap")
		return err
	}
	query := `
		UPDATE email_accounts_smtp_imap
		SET smtp_host = $1, smtp_port = $2, smtp_user = $3, smtp_password = $4, smtp_security = $5,
		    imap_host = $6, imap_port = $7, imap_user = $8, imap_password = $9, imap_security = $10,
		    smtp_tls_unverified = $11, smtp_tls_pin = $12, imap_tls_unverified = $13, imap_tls_pin = $14
		WHERE email_account_id = $15
	`
	if _, err := r.DB.Exec(ctx, query,
		sealed[0], creds.SMTP.Port, sealed[1], sealed[2], models.ResolveSMTPSecurity(creds.SMTP.Security, creds.SMTP.Port),
		sealed[3], creds.IMAP.Port, sealed[4], sealed[5], models.ResolveIMAPSecurity(creds.IMAP.Security, creds.IMAP.Port),
		creds.SMTP.TLSUnverified, creds.SMTP.StoredTLSPin(), creds.IMAP.TLSUnverified, creds.IMAP.StoredTLSPin(),
		id); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

// mailboxOrganization returns the organization whose key seals a mailbox's credentials; nil for a personal mailbox.
func (r *emailRepository) mailboxOrganization(ctx context.Context, id uuid.UUID) (*uuid.UUID, error) {
	var orgID *uuid.UUID
	const query = `SELECT organization_id FROM email_accounts WHERE id = $1`
	if err := r.DB.QueryRow(ctx, query, id).Scan(&orgID); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			db.CaptureError(err, query, nil, "queryrow")
		}
		return nil, err
	}
	return orgID, nil
}
