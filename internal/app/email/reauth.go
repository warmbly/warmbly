package email

// Reconnecting a broken mailbox (issue #274): a provider-side credential change
// (password reset, revoked grant, expired app password) deactivates the account
// and leaves an error row behind. The flows here renew the credential in place,
// resolve exactly the errors that credential caused, and put the mailbox back
// to work — never creating a second account for the same address.

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"golang.org/x/oauth2"
)

// OAuthReauth issues an authorization URL that renews an existing mailbox's
// tokens. Same round trip as OAuthStart, but the state carries the account id
// so the finish leg updates in place instead of connecting a duplicate.
func (s *emailService) OAuthReauth(ctx context.Context, userID string, orgID *uuid.UUID, accountID uuid.UUID, returnOrigin string) (*models.EmailOnboardingStartResponse, *errx.Error) {
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
	if returnOrigin != "" && config.DashboardOrigin(returnOrigin) == "" {
		return nil, errx.ErrEmailOnboardReturnOrigin
	}
	if provider == models.InboxProviderSMTPIMAP {
		return nil, errx.ErrEmailReauthProvider
	}
	if account.AuthMethod == models.MailAuthDelegated {
		return nil, errx.ErrEmailReauthDelegated
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

	// Only the dashboard renews a mailbox's sign-in.
	state, err := newState(true)
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	// PKCE, same as the first-connect path: the verifier never leaves the
	// server, so an intercepted code is not redeemable.
	verifier := oauth2.GenerateVerifier()

	if xerr := s.saveOnboardingState(ctx, state, &models.EmailOnboardingState{
		UserID:         userID,
		OrganizationID: orgID,
		Provider:       string(provider),
		Nonce:          state,
		EmailAccountID: &accountID,
		CodeVerifier:   verifier,
		ReturnOrigin:   returnOrigin,
	}); xerr != nil {
		return nil, xerr
	}

	opts := append(authCodeOptions(provider, account.Email), oauth2.S256ChallengeOption(verifier))
	url := cfg.AuthCodeURL(state, opts...)
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

	if err := s.emailRepository.ReplaceBoxToken(ctx, account.ID, tok.AccessToken, refresh, tok.Expiry, recordedScopes(tok)); err != nil {
		return nil, errx.InternalError()
	}

	// The send-as list can have changed while the mailbox was disconnected,
	// and a stale one is what would put a removed alias on the From header.
	// The signature is deliberately not re-imported: a reconnect is a repair,
	// not a moment to overwrite what someone has since edited here.
	s.captureSendIdentityList(ctx, account, tok)

	return s.reconnectAccount(ctx, account.ID)
}

// UpdateSMTPIMAPCredentials is the SMTP/IMAP counterpart of the OAuth reauth:
// validate the replacement credentials against a live worker, store them, and
// put the mailbox back to work.
func (s *emailService) UpdateSMTPIMAPCredentials(ctx context.Context, orgID *uuid.UUID, accountID uuid.UUID, creds *models.SmtpImap) (*models.Email, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
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
	s.inheritTLSPolicy(ctx, accountID, creds)

	if s.workerAssignment == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}
	if xerr := s.checkCredentials(ctx, *orgID, account.WorkerID, creds); xerr != nil {
		return nil, xerr
	}

	if err := s.emailRepository.ReplaceSMTPIMAPCredentials(ctx, accountID, creds); err != nil {
		return nil, errx.InternalError()
	}

	account, xerr = s.reconnectAccount(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	// Warmbly Cloud sends this mailbox's warmup with its own copy of the credential.
	if s.cloudCredentials != nil {
		if xerr := s.cloudCredentials.RefreshCredentials(ctx, *orgID, accountID); xerr != nil {
			log.Warn().Str("account_id", accountID.String()).Str("code", xerr.Identifier).Msg("cloud link: new credential not handed to Warmbly Cloud; it keeps the old one until the mailbox is enrolled again")
		}
	}
	return account, nil
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
	if account.OrganizationID == nil {
		return nil, errx.ErrNoOrganization
	}
	status := "active"
	updated, xerr := s.Update(ctx, account.OrganizationID.String(), account.UserID, account.ID.String(), &models.UpdateEmail{Status: &status})
	if xerr != nil {
		return nil, xerr
	}
	s.resolveCredentialErrors(ctx, account.ID)
	// Both reconnect flows only get here with a credential the server just accepted; a send the provider still refuses holds the mailbox again.
	if releaser, ok := s.emailRepository.(interface {
		ReleaseReconnectedHold(context.Context, uuid.UUID, uuid.UUID) (bool, error)
	}); ok {
		if _, err := releaser.ReleaseReconnectedHold(ctx, *account.OrganizationID, account.ID); err != nil {
			log.Warn().Err(err).Str("account_id", account.ID.String()).Msg("reconnect succeeded but the send hold could not be released")
		}
	}
	return updated, nil
}

// resolveCredentialErrors clears the credential-class error rows; unrelated
// errors (domain auth, sync fair use) stay visible because a reconnect does
// not fix them.
func (s *emailService) resolveCredentialErrors(ctx context.Context, accountID uuid.UUID) {
	if s.accountErrors == nil {
		return
	}
	// The certificate errors clear too: every reconnect path first passed a
	// connection test under the TLS settings it stored.
	codes := make([]string, 0, len(errx.CredentialMailErrorCodes)+len(errx.TLSCertificateMailErrorCodes))
	for _, c := range append(append([]errx.MailErrorCode{}, errx.CredentialMailErrorCodes...), errx.TLSCertificateMailErrorCodes...) {
		codes = append(codes, string(c))
	}
	if xerr := s.accountErrors.ResolveByCodes(ctx, accountID, codes, "reconnect"); xerr != nil {
		log.Warn().Str("account_id", accountID.String()).Str("error", xerr.Message).Msg("could not resolve credential errors after reconnect")
	}
}

// inheritTLSPolicy keeps a server's certificate opt-out and pin across a
// credential change that does not mention them, as long as the host is the
// same. The check that follows clears both once the chain passes CA verification.
func (s *emailService) inheritTLSPolicy(ctx context.Context, accountID uuid.UUID, creds *models.SmtpImap) {
	stored, xerr := s.emailRepository.GetSMTPCredentials(ctx, accountID)
	if xerr != nil || stored == nil {
		return
	}
	inheritTLS(creds.SMTP, stored.SMTPHost, stored.SMTPTLSUnverified, stored.SMTPTLSPin)
	inheritTLS(creds.IMAP, stored.IMAPHost, stored.IMAPTLSUnverified, stored.IMAPTLSPin)
}

func inheritTLS(svc *models.Service, host string, unverified bool, pin string) {
	if svc == nil || svc.TLSUnverified || !unverified {
		return
	}
	if !strings.EqualFold(models.NormalizeMailHost(svc.Host), models.NormalizeMailHost(host)) {
		return
	}
	svc.TLSUnverified, svc.TLSPin = true, pin
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
	normalizeMailPasswords(creds.SMTP, creds.IMAP)
	return validateMailSecurity(creds.SMTP, creds.IMAP)
}
