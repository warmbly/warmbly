package email

// Reconnecting a broken mailbox (issue #274): a provider-side credential change
// (password reset, revoked grant, expired app password) deactivates the account
// and leaves an error row behind. The flows here renew the credential in place,
// resolve exactly the errors that credential caused, and put the mailbox back
// to work — never creating a second account for the same address.

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"golang.org/x/oauth2"
)

// OAuthReauth issues an authorization URL that renews an existing mailbox's
// tokens. Same round trip as OAuthStart, but the state carries the account id
// so the finish leg updates in place instead of connecting a duplicate.
func (s *emailService) OAuthReauth(ctx context.Context, userID string, orgID *uuid.UUID, accountID uuid.UUID) (*models.EmailOnboardingStartResponse, *errx.Error) {
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}

	account, xerr := s.emailRepository.Get(ctx, orgID.String(), accountID.String())
	if xerr != nil {
		return nil, xerr
	}
	if account == nil {
		return nil, errx.ErrNotFound
	}

	provider := models.InboxProvider(account.Provider)
	if provider == models.InboxProviderSMTPIMAP {
		return nil, errx.ErrEmailReauthProvider
	}
	// A cloud-managed mailbox has no local token row to renew; its sign-in
	// lives on Warmbly Cloud.
	if s.cloudLink != nil {
		if m, err := s.cloudLink.GetByAccount(ctx, accountID); err == nil && m != nil && m.Managed {
			return nil, errx.ErrEmailReauthCloudManaged
		}
	}

	cfg, xerr := s.oauthConfigFor(provider)
	if xerr != nil {
		return nil, xerr
	}

	state, err := crypt.Nonce()
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	if xerr := s.saveOnboardingState(ctx, state, &models.EmailOnboardingState{
		UserID:         userID,
		OrganizationID: orgID,
		Provider:       string(provider),
		Nonce:          state,
		EmailAccountID: &accountID,
	}); xerr != nil {
		return nil, xerr
	}

	url := cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce, // force refresh_token issuance on reconnect
		// Preselect the mailbox being renewed in the provider's picker.
		oauth2.SetAuthURLParam("login_hint", account.Email),
	)
	return &models.EmailOnboardingStartResponse{URL: url, State: state}, nil
}

// finishReauth lands a reauth round trip: same-address check, token rewrite,
// error resolution, reactivation.
func (s *emailService) finishReauth(ctx context.Context, sess *models.EmailOnboardingState, provider models.InboxProvider, tok *oauth2.Token, owner *inboxOwner) (*models.Email, *errx.Error) {
	account, xerr := s.emailRepository.GetByID(ctx, *sess.EmailAccountID)
	if xerr != nil {
		return nil, xerr
	}
	if account == nil || sess.OrganizationID == nil || account.OrganizationID == nil || *account.OrganizationID != *sess.OrganizationID {
		return nil, errx.ErrNotFound
	}
	if models.InboxProvider(account.Provider) != provider {
		return nil, errx.ErrEmailOnboardProvider
	}

	// The consent must be for this mailbox's own address: tokens for any other
	// account would read as connected and then fail every send and sync.
	if !strings.EqualFold(strings.TrimSpace(owner.Email), strings.TrimSpace(account.Email)) {
		return nil, errx.ErrEmailReauthWrongAccount
	}

	// A repeat consent may omit the refresh token; keep the stored one rather
	// than blanking the row. Writing without one would seal an empty string
	// over the stored token and end all future access-token refreshes, so a
	// failed fallback read refuses the reauth instead.
	refresh := tok.RefreshToken
	if refresh == "" {
		creds, cerr := s.emailRepository.GetOAuthCredentials(ctx, account.ID)
		if cerr != nil {
			return nil, cerr
		}
		if creds == nil || creds.RefreshToken == "" {
			return nil, errx.ErrEmailReauthNoRefreshToken
		}
		refresh = creds.RefreshToken
	}

	if err := s.emailRepository.RefreshBoxToken(ctx, account.ID, tok.AccessToken, refresh, tok.Expiry); err != nil {
		return nil, errx.InternalError()
	}

	return s.reconnectAccount(ctx, account.ID)
}

// UpdateSMTPIMAPCredentials is the SMTP/IMAP counterpart of the OAuth reauth:
// validate the replacement credentials against a live worker, store them, and
// put the mailbox back to work.
func (s *emailService) UpdateSMTPIMAPCredentials(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, creds *models.SmtpImap) (*models.Email, *errx.Error) {
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}

	// GetByID, not the org-scoped Get: the reconnect tail needs the owner's
	// user id, which Get does not select. Tenancy is enforced right below.
	account, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if account == nil || account.OrganizationID == nil || *account.OrganizationID != *orgID {
		return nil, errx.ErrNotFound
	}
	if models.InboxProvider(account.Provider) != models.InboxProviderSMTPIMAP {
		return nil, errx.ErrEmailReauthOAuthOnly
	}

	if xerr := validateSMTPIMAPCredentials(creds); xerr != nil {
		return nil, xerr
	}

	if s.workerAssignment == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}
	// Any healthy worker can run the one-shot validation handshake, same as at
	// connect time; tier only matters for placement.
	w, werr := s.workerAssignment.SelectSharedWorker(ctx, false)
	if werr != nil || w == nil {
		w, werr = s.workerAssignment.SelectSharedWorker(ctx, true)
	}
	if werr != nil || w == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}
	if xerr := s.ValidateCredentials(ctx, *orgID, w.ID.String(), creds); xerr != nil {
		return nil, xerr
	}

	if err := s.emailRepository.ReplaceSMTPIMAPCredentials(ctx, accountID, creds); err != nil {
		return nil, errx.InternalError()
	}

	return s.reconnectAccount(ctx, accountID)
}

// reconnectAccount is the shared tail of both reconnect flows: reactivate,
// then resolve the credential errors the new secret just fixed — Update
// carries the status through pool membership, the worker, and the realtime
// fanout. Errors resolve only after a successful reactivation, or a failed
// Update would clear the banner (and its reconnect button) while the mailbox
// stays broken. It loads the row itself because the owner-scoped Update needs
// user_id, which not every caller's read path selects.
func (s *emailService) reconnectAccount(ctx context.Context, accountID uuid.UUID) (*models.Email, *errx.Error) {
	account, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if account == nil {
		return nil, errx.ErrNotFound
	}
	status := "active"
	updated, xerr := s.Update(ctx, account.UserID, account.ID.String(), &models.UpdateEmail{Status: &status})
	if xerr != nil {
		return nil, xerr
	}
	s.resolveCredentialErrors(ctx, account.ID)
	return updated, nil
}

// resolveCredentialErrors clears the credential-class error rows; unrelated
// errors (domain auth, sync fair use) stay visible because a reconnect does
// not fix them.
func (s *emailService) resolveCredentialErrors(ctx context.Context, accountID uuid.UUID) {
	if s.accountErrors == nil {
		return
	}
	codes := make([]string, 0, len(errx.CredentialMailErrorCodes))
	for _, c := range errx.CredentialMailErrorCodes {
		codes = append(codes, string(c))
	}
	if xerr := s.accountErrors.ResolveByCodes(ctx, accountID, codes, "reconnect"); xerr != nil {
		log.Warn().Str("account_id", accountID.String()).Str("error", xerr.Message).Msg("could not resolve credential errors after reconnect")
	}
}

// validateSMTPIMAPCredentials checks a replacement credential set: same bar as
// validateSMTPIMAPInput minus the account fields, which a reauth never changes.
func validateSMTPIMAPCredentials(creds *models.SmtpImap) *errx.Error {
	if creds == nil || creds.SMTP == nil || creds.IMAP == nil {
		return errx.ErrEmailCredentialsRequired
	}
	if strings.TrimSpace(creds.SMTP.Host) == "" {
		return errx.ErrEmailSMTPHost
	}
	if !validPort(creds.SMTP.Port) {
		return errx.ErrEmailSMTPPort
	}
	if strings.TrimSpace(creds.IMAP.Host) == "" {
		return errx.ErrEmailIMAPHost
	}
	if !validPort(creds.IMAP.Port) {
		return errx.ErrEmailIMAPPort
	}
	return validateMailSecurity(creds.SMTP, creds.IMAP)
}
