package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// SigninSwitchCandidate is a vendor-linked Google mailbox the automatic switch may reconnect.
type SigninSwitchCandidate struct {
	VendorReconnect
	// OwnerID is the workspace owner, in whose name the vendor's sign-in lands.
	OwnerID uuid.UUID
}

// SigninSwitchRetry is how long a mailbox waits after a vendor reconnect before Warmbly asks again on its own.
const SigninSwitchRetry = 7 * 24 * time.Hour

// signinSwitchDue is a mailbox the automatic switch has not tried within SigninSwitchRetry and never connected; $3 is the retry.
const signinSwitchDue = `(ea.signin_switch_at IS NULL OR (ea.signin_switch_state IS DISTINCT FROM 'connected' AND ea.signin_switch_at < now() - $3::interval))`

// googleWithoutFullMail is a Google mailbox on a grant, an app password, or its own sign-in without full mail access recorded; $4 is the scope.
const googleWithoutFullMail = `((ea.provider = 'smtp_imap' AND ea.auth_method = 'app_password' AND ea.mail_host = 'google_workspace') OR (ea.provider = 'gmail' AND (ea.auth_method = 'delegated' OR
	(ea.auth_method = 'oauth' AND NOT COALESCE((SELECT $4 = ANY(o.granted_scopes) FROM email_accounts_oauth o WHERE o.email_account_id = ea.id), false)))))`

// AutoSwitchCandidates are active Google mailboxes, linked to a healthy connection of one of vendors, that
// lack full mail access and are due for an automatic reconnect, oldest attempt first, at most limit.
func (r *vendorConnectionRepository) AutoSwitchCandidates(ctx context.Context, vendors []string, limit int) ([]SigninSwitchCandidate, error) {
	query := `
		SELECT ea.id, ea.organization_id, ea.vendor_connection_id, ea.vendor_mailbox_id, ea.email, org.owner_user_id
		FROM email_accounts ea
		JOIN mailbox_vendor_connections c ON c.id = ea.vendor_connection_id AND c.organization_id = ea.organization_id
		JOIN organizations org ON org.id = ea.organization_id
		WHERE c.status = 'active' AND c.vendor = ANY($1) AND org.deletion_scheduled_at IS NULL
		  AND ea.vendor_mailbox_id <> '' AND ea.status = 'active'
		  AND lower(split_part(ea.email, '@', 2)) NOT IN ('gmail.com', 'googlemail.com')
		  AND ` + googleWithoutFullMail + `
		  AND NOT ` + managedExpr + `
		  AND ` + signinSwitchDue + `
		ORDER BY ea.signin_switch_at NULLS FIRST, ea.organization_id, ea.vendor_connection_id, ea.id
		LIMIT $2`
	rows, err := r.DB.Query(ctx, query, vendors, limit, SigninSwitchRetry, models.GoogleFullMailScope)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []SigninSwitchCandidate
	for rows.Next() {
		var c SigninSwitchCandidate
		if err := rows.Scan(&c.AccountID, &c.OrgID, &c.ConnectionID, &c.VendorMailboxID, &c.Email, &c.OwnerID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// StartSigninSwitch records a vendor reconnect starting for the organization's mailboxes and returns the ones it
// recorded. An automatic start claims only mailboxes still due, so two runs never start the same one twice.
func (r *vendorConnectionRepository) StartSigninSwitch(ctx context.Context, orgID uuid.UUID, ids []uuid.UUID, automatic bool) ([]uuid.UUID, error) {
	query := `UPDATE email_accounts ea SET signin_switch_state = 'queued', signin_switch_at = now(), signin_switch_message = '',
		       signin_switch_automatic = $4
		WHERE ea.organization_id = $1 AND ea.id = ANY($2) AND (NOT $4 OR ` + signinSwitchDue + `)
		RETURNING ea.id`
	rows, err := r.DB.Query(ctx, query, orgID, ids, SigninSwitchRetry, automatic)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetSigninSwitch records where one mailbox's vendor reconnect now stands.
func (r *vendorConnectionRepository) SetSigninSwitch(ctx context.Context, orgID, accountID uuid.UUID, status, message string) error {
	query := `UPDATE email_accounts SET signin_switch_state = $3, signin_switch_message = $4, signin_switch_at = now()
		WHERE organization_id = $1 AND id = $2`
	if _, err := r.DB.Exec(ctx, query, orgID, accountID, status, message); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}

// SigninState is what deciding one mailbox's sign-in options reads; nil when the organization has no such mailbox.
func (r *vendorConnectionRepository) SigninState(ctx context.Context, orgID, accountID uuid.UUID) (*models.MailboxSigninState, error) {
	query := `
		SELECT ea.id, ea.email, ea.provider, ea.auth_method, ea.mail_host, ea.status, ` + managedExpr + `,
		       o.granted_scopes, ea.vendor_mailbox_id,
		       c.id, COALESCE(c.vendor, ''), COALESCE(c.label, ''), COALESCE(c.status, ''),
		       ea.signin_switch_state, ea.signin_switch_message, ea.signin_switch_automatic, ea.signin_switch_at
		FROM email_accounts ea
		LEFT JOIN email_accounts_oauth o ON o.email_account_id = ea.id
		LEFT JOIN mailbox_vendor_connections c ON c.id = ea.vendor_connection_id AND c.organization_id = ea.organization_id
		WHERE ea.organization_id = $1 AND ea.id = $2`
	var (
		st       models.MailboxSigninState
		provider string
		connID   *uuid.UUID
		vendor   models.VendorConnection
		state    *string
		message  string
		auto     bool
		at       *time.Time
	)
	err := r.DB.QueryRow(ctx, query, orgID, accountID).Scan(&st.AccountID, &st.Email, &provider, &st.AuthMethod, &st.MailHost, &st.Status,
		&st.Managed, &st.GrantedScopes, &st.VendorMailboxID, &connID, &vendor.Vendor, &vendor.Label, &vendor.Status,
		&state, &message, &auto, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	st.Provider = models.InboxProvider(provider)
	if connID != nil {
		vendor.ID, vendor.OrganizationID = *connID, orgID
		st.Vendor = &vendor
	}
	if state != nil && at != nil {
		st.Switch = &models.MailboxSigninSwitch{Status: *state, Message: message, Automatic: auto, At: *at}
	}
	return &st, nil
}
