package repository

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func (r *emailRepository) GetSMTPIMAP(ctx context.Context, userID, emailAccountID string) (*models.SmtpImap, *errx.Error) {
	var imap models.Service
	var smtp models.Service
	var ts time.Time

	query := `
		SELECT
    	 smtp.smtp_host,
    	 smtp.smtp_port,
    	 smtp.smtp_user,
    	 smtp.smtp_password,
   		 smtp.imap_host,
    	 smtp.imap_port,
    	 smtp.imap_user,
    	 smtp.imap_password,
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
		&smtp.Host, &smtp.Port, &smtp.Username, &smtp.Password,
		&imap.Host, &imap.Port, &imap.Username, &imap.Password,
		&ts,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}
	imap.Username, err = r.Encrypt.Decrypt(imap.Username)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	imap.Password, err = r.Encrypt.Decrypt(imap.Password)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	imap.Host, err = r.Encrypt.Decrypt(imap.Host)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	smtp.Username, err = r.Encrypt.Decrypt(smtp.Username)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	smtp.Password, err = r.Encrypt.Decrypt(smtp.Password)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	smtp.Host, err = r.Encrypt.Decrypt(smtp.Host)
	if err != nil {
		sentry.CaptureException(err)
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
	query := `
		UPDATE email_accounts_oauth
		SET access_token = $1, refresh_token = $2, expires_at = $3
		WHERE email_account_id = $4
	`

	params := []any{
		accessToken,
		refreshToken,
		expiresAt,
		id,
	}

	_, err := r.DB.Exec(
		ctx,
		query,
		params...,
	)
	if err != nil {
		db.CaptureError(err, query, params, "exec")
		return err
	}
	return nil
}
