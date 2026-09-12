package email

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/crypt"
	"golang.org/x/oauth2"
)

// OAuthStart issues a fresh state nonce and returns the provider-specific authorization URL.
// The caller is expected to redirect the user to the URL and post back to OAuthFinish on return.
func (s *emailService) OAuthStart(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider) (*models.EmailOnboardingStartResponse, *errx.Error) {
	cfg, xerr := s.oauthConfigFor(provider)
	if xerr != nil {
		return nil, xerr
	}

	// Refuse early so we don't waste an OAuth round-trip on a request
	// that the inbox-limit guard would reject after callback.
	if _, xerr := s.guardInboxLimit(ctx, orgID); xerr != nil {
		return nil, xerr
	}

	state, err := crypt.Nonce()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}

	if xerr := s.saveOnboardingState(ctx, state, &models.EmailOnboardingState{
		UserID:         userID,
		OrganizationID: orgID,
		Provider:       string(provider),
		Nonce:          state,
	}); xerr != nil {
		return nil, xerr
	}

	url := cfg.AuthCodeURL(state, authCodeOptions(provider, "")...)
	return &models.EmailOnboardingStartResponse{URL: url, State: state}, nil
}

// guardInboxLimit refuses a connect that would take the workspace past its
// mailbox allowance (fair use for paid plans, FreeWorkspaceMailboxLimit for
// free ones, unlimited without billing) and returns the resolved allowance so
// the insert can enforce it again under the organization's lock. The
// allowance is counted per org, so no org means it cannot be applied and the
// connect is refused. Without an allowance source wired, the feature gate's
// free-or-paid split stands in and the insert is not re-checked.
func (s *emailService) guardInboxLimit(ctx context.Context, orgID *uuid.UUID) (*models.MailboxAllowance, *errx.Error) {
	if orgID == nil {
		return nil, errx.ErrNoOrganization
	}
	if s.allowance != nil {
		a, xerr := s.allowance.MailboxAllowance(ctx, *orgID)
		if xerr != nil {
			return nil, xerr
		}
		if a.CanAdd(1) {
			return a, nil
		}
		return nil, errx.MailboxAllowanceReached(a.Used, *a.Allowance, a.Paid)
	}
	if s.featureGate == nil {
		return nil, nil
	}
	count, xerr := s.emailRepository.CountForOrganization(ctx, *orgID)
	if xerr != nil {
		return nil, xerr
	}
	allowed, xerr := s.featureGate.CanAddInbox(ctx, *orgID, count)
	if xerr != nil {
		return nil, xerr
	}
	if allowed {
		return nil, nil
	}
	return nil, errx.MailboxAllowanceReached(count, models.FreeWorkspaceMailboxLimit, false)
}

// OAuthFinish validates the state, exchanges the code for tokens, fetches the
// inbox owner, and persists a new email account — or, when the state carries an
// account id (OAuthReauth), renews that mailbox's tokens in place instead.
func (s *emailService) OAuthFinish(ctx context.Context, userID, code, state string) (*models.Email, bool, *errx.Error) {
	if code = strings.TrimSpace(code); code == "" {
		return nil, false, errx.ErrEmailOnboardCode
	}
	if state = strings.TrimSpace(state); state == "" {
		return nil, false, errx.ErrEmailOnboardState
	}

	sess, xerr := s.takeOnboardingState(ctx, state)
	if xerr != nil {
		return nil, false, xerr
	}
	if sess.UserID != userID {
		return nil, false, errx.ErrEmailOnboardState
	}

	// A reauth adds no mailbox, so an org over its inbox cap can still fix one.
	var allowance *models.MailboxAllowance
	if sess.EmailAccountID == nil {
		a, xerr := s.guardInboxLimit(ctx, sess.OrganizationID)
		if xerr != nil {
			return nil, false, xerr
		}
		allowance = a
	}

	provider := models.InboxProvider(sess.Provider)
	cfg, xerr := s.oauthConfigFor(provider)
	if xerr != nil {
		return nil, false, xerr
	}

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, false, errx.ErrEmailOnboardExchange
	}

	// A consent screen lets the person untick individual permissions and still
	// returns a token, so this is the last point at which a half-granted
	// mailbox can be refused. Storing one instead means a mailbox that looks
	// connected and fails on its first send, days later, with a provider 403
	// nobody can trace back to a checkbox.
	if xerr := checkGrantedScopes(ctx, provider, cfg.Scopes, tok); xerr != nil {
		return nil, false, xerr
	}

	owner, xerr := fetchInboxOwner(ctx, provider, tok.AccessToken)
	if xerr != nil {
		return nil, false, xerr
	}

	if sess.EmailAccountID != nil {
		acc, xerr := s.finishReauth(ctx, sess, provider, tok, owner)
		return acc, true, xerr
	}

	if exists, xerr := s.emailRepository.ExistsForUser(ctx, userID, owner.Email); xerr != nil {
		return nil, false, xerr
	} else if exists {
		return nil, false, errx.ErrEmailOnboardAlreadyExists
	}

	// A first connect with no refresh token is a mailbox with about an hour to
	// live: the access token expires and nothing can renew it. reauth.go
	// deliberately tolerates an absent one because it keeps the stored value,
	// but here there is nothing stored to fall back on, so refuse now rather
	// than hand back a mailbox that stops by itself.
	if strings.TrimSpace(tok.RefreshToken) == "" {
		log.Warn().
			Str("provider", string(provider)).
			Msg("mailbox onboarding: first connect returned no refresh token")
		errs.CaptureMessageContext(ctx, fmt.Sprintf("%s first connect returned no refresh token", provider),
			errs.Tag("provider", string(provider)))
		return nil, false, errx.New(errx.BadRequest,
			"The provider did not return a long-lived token for this mailbox, so it would stop working within the hour. "+
				"Remove Warmbly's access in your account settings and connect it again.")
	}

	name := strings.TrimSpace(owner.Name)
	if name == "" {
		name = deriveNameFromEmail(owner.Email)
	}

	acc, xerr := s.emailRepository.NewOauthAccount(ctx, userID, models.NewOauthAccount{
		OrganizationID: sess.OrganizationID,
		Allowance:      allowance,
		Provider:       provider,
		Name:           name,
		Email:          owner.Email,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		ExpiresAt:      tok.Expiry,
	})
	if xerr == nil && acc != nil {
		s.syncWarmupPoolMembership(ctx, acc)
		s.publishAccountEvent(ctx, pubsub.EventAccountConnected, acc)
		s.dispatchAccountConnected(ctx, sess.OrganizationID, acc)
		// Assign a worker and load the mailbox so it starts sending/syncing
		// immediately; the reconciler is the fallback if this fails.
		s.loadAccountBestEffort(ctx, acc.ID)
	}
	return acc, false, xerr
}

// OnboardSMTPIMAP validates the supplied SMTP/IMAP credentials against a live worker, then
// persists the email account on success. Returns ErrEmailCredentials if the worker reports failure.
func (s *emailService) OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	if xerr := validateSMTPIMAPInput(data); xerr != nil {
		return nil, xerr
	}

	allowance, xerr := s.guardInboxLimit(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}

	if exists, xerr := s.emailRepository.ExistsForUser(ctx, userID, data.Email); xerr != nil {
		return nil, xerr
	} else if exists {
		return nil, errx.ErrEmailOnboardAlreadyExists
	}

	if s.workerAssignment == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}

	// Any live worker can run the one-shot validation handshake: nothing is
	// placed yet, the worker just dials the credentials once and reports back.
	w, werr := s.workerAssignment.SelectValidationWorker(ctx)
	if werr != nil || w == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}

	creds := &models.SmtpImap{SMTP: data.SMTP, IMAP: data.IMAP}
	if xerr := s.ValidateCredentials(ctx, *orgID, w.ID.String(), creds); xerr != nil {
		return nil, xerr
	}

	data.OrganizationID = orgID
	data.Allowance = allowance

	acc, xerr := s.emailRepository.NewSMTPIMAPAccount(ctx, userID, *data)
	if xerr != nil {
		return nil, xerr
	}

	// Place the mailbox for real. Failure here is non-fatal: the scheduler
	// picks the account up on its next pass.
	if orgID != nil {
		if _, err := s.workerAssignment.AssignWorkerToEmail(ctx, acc.ID, *orgID); err != nil {
			errs.CaptureException(err)
		}
	}

	s.syncWarmupPoolMembership(ctx, acc)
	s.publishAccountEvent(ctx, pubsub.EventAccountConnected, acc)
	s.dispatchAccountConnected(ctx, orgID, acc)
	// Load the mailbox onto its assigned worker so it starts sending/syncing
	// immediately; the reconciler is the fallback if this fails.
	s.loadAccountBestEffort(ctx, acc.ID)
	return acc, nil
}

// dispatchAccountConnected fires an email_account.connected webhook event
// to any subscribed endpoints. Failures here are best-effort and never
// block the onboarding flow.
func (s *emailService) dispatchAccountConnected(ctx context.Context, orgID *uuid.UUID, acc *models.Email) {
	if s.webhookService == nil || orgID == nil || acc == nil {
		return
	}
	payload := map[string]any{
		"email_account_id": acc.ID,
		"email":            acc.Email,
		"provider":         acc.Provider,
		"name":             acc.Name,
		"created_at":       acc.CreatedAt,
	}
	if _, err := s.webhookService.Dispatch(ctx, *orgID, models.WebhookEventEmailAccountConnected, payload); err != nil {
		errs.CaptureException(err)
	}
}

// oauthConfigured reports whether an OAuth client is actually usable, i.e. both
// halves of the credential are present.
func oauthConfigured(cfg *oauth2.Config) bool {
	return cfg != nil && cfg.ClientID != "" && cfg.ClientSecret != ""
}

func (s *emailService) oauthConfigFor(provider models.InboxProvider) (*oauth2.Config, *errx.Error) {
	// LoadOauth2Inbox always returns a config, populated with empty strings when
	// the variables are unset, so the credentials themselves are what decides
	// whether the provider is actually available here.
	switch provider {
	case models.InboxProviderGoogle:
		if s.oauthInbox == nil || !oauthConfigured(s.oauthInbox.Google) {
			return nil, errx.ErrEmailOnboardGoogleNotConfigured
		}
		return s.oauthInbox.Google, nil
	case models.InboxProviderOutlook:
		if s.oauthInbox == nil || !oauthConfigured(s.oauthInbox.Outlook) {
			return nil, errx.ErrEmailOnboardOutlookNotConfigured
		}
		return s.oauthInbox.Outlook, nil
	default:
		return nil, errx.ErrEmailOnboardProvider
	}
}

func validateSMTPIMAPInput(data *models.NewSMTPIMAPAccount) *errx.Error {
	if data == nil || data.SMTP == nil || data.IMAP == nil {
		return errx.ErrEmailCredentialsRequired
	}
	data.Email = strings.TrimSpace(data.Email)
	if _, err := mail.ParseAddress(data.Email); err != nil {
		return errx.ErrEmail
	}
	if !validNameLen(&data.Name) {
		return errx.ErrEmailName
	}
	if strings.TrimSpace(data.SMTP.Host) == "" {
		return errx.ErrEmailSMTPHost
	}
	if !validPort(data.SMTP.Port) {
		return errx.ErrEmailSMTPPort
	}
	if strings.TrimSpace(data.IMAP.Host) == "" {
		return errx.ErrEmailIMAPHost
	}
	if !validPort(data.IMAP.Port) {
		return errx.ErrEmailIMAPPort
	}
	return validateMailSecurity(data.SMTP, data.IMAP)
}

// validPort accepts any routable TCP port. Mail submission is conventionally
// 465/587 and IMAP 993/143, but plenty of providers and self-hosted servers
// use 2525, 25, or something else entirely, and the security mode (not the
// port) is what decides how we connect.
func validPort(port int) bool {
	return port > 0 && port <= 65535
}

// validateMailSecurity rejects an unknown security mode. Empty is allowed and
// means "infer from the port", which is how existing clients behave.
//
// "none" carries two extra conditions, because it is the one mode that puts a
// password on an unencrypted socket. It is legal only against a loopback host,
// where the socket never reaches a wire, and only on a self-hosted instance,
// where the worker runs on the operator's own machine. On the hosted product
// the worker is never the customer's machine, so a loopback address there is
// the WORKER's loopback: the mode could not reach the relay it was meant for
// and would only be a way to speak plaintext to whatever answers on that port.
func validateMailSecurity(smtp, imap *models.Service) *errx.Error {
	if smtp.Security != "" && !models.ValidMailSecurity(smtp.Security) {
		return errx.ErrEmailSMTPSecurity
	}
	if imap.Security != "" && !models.ValidMailSecurity(imap.Security) {
		return errx.ErrEmailIMAPSecurity
	}
	if err := validateCleartextHost(smtp.Security, smtp.Host, errx.ErrEmailSMTPSecurityNotLocal, errx.ErrEmailSMTPSecurityHosted); err != nil {
		return err
	}
	return validateCleartextHost(imap.Security, imap.Host, errx.ErrEmailIMAPSecurityNotLocal, errx.ErrEmailIMAPSecurityHosted)
}

// validateCleartextHost is the "none" gate for one leg.
func validateCleartextHost(security, host string, notLocal, hosted *errx.Error) *errx.Error {
	if security != models.MailSecurityNone {
		return nil
	}
	if !config.SelfHosted() {
		return hosted
	}
	if !models.LoopbackMailHost(host) {
		return notLocal
	}
	return nil
}

func validNameLen(name *string) bool {
	*name = strings.TrimSpace(*name)
	if *name == "" {
		return false
	}
	r := []rune(*name)
	return len(r) >= 2 && len(r) <= 100
}

func deriveNameFromEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 0 {
		return email
	}
	local := email[:at]
	if local == "" {
		return email
	}
	local = strings.ReplaceAll(local, ".", " ")
	local = strings.ReplaceAll(local, "_", " ")
	return strings.Title(local)
}

// inboxOwner is the per-provider user info shape we normalize on.
type inboxOwner struct {
	Email string
	Name  string
}

func fetchInboxOwner(ctx context.Context, provider models.InboxProvider, accessToken string) (*inboxOwner, *errx.Error) {
	switch provider {
	case models.InboxProviderGoogle:
		return fetchGmailOwner(ctx, accessToken)
	case models.InboxProviderOutlook:
		return fetchOutlookOwner(ctx, accessToken)
	default:
		return nil, errx.ErrEmailOnboardProvider
	}
}

// grantedScopes reads what the provider actually authorised. Both Google and
// Microsoft return a space-separated "scope" alongside the token; an empty or
// absent one means the provider did not say, which is not the same as "nothing
// was granted" and must not be read as a denial.
func grantedScopes(tok *oauth2.Token) (map[string]bool, bool) {
	raw, _ := tok.Extra("scope").(string)
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	out := make(map[string]bool)
	for _, sc := range strings.Fields(raw) {
		out[sc] = true
	}
	return out, true
}

// scopeSatisfiedBy maps a scope we ask for to every scope that confers it.
// Google's Gmail scopes nest: gmail.modify covers reading and sending, and
// gmail.compose covers sending. Without this, someone who granted the broader
// permission but not the narrower one would be turned away for a capability
// they actually have.
var scopeSatisfiedBy = map[string][]string{
	"https://www.googleapis.com/auth/gmail.readonly": {
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.modify",
		"https://mail.google.com/",
	},
	"https://www.googleapis.com/auth/gmail.send": {
		"https://www.googleapis.com/auth/gmail.send",
		"https://www.googleapis.com/auth/gmail.compose",
		"https://www.googleapis.com/auth/gmail.modify",
		"https://mail.google.com/",
	},
	"https://www.googleapis.com/auth/gmail.metadata": {
		"https://www.googleapis.com/auth/gmail.metadata",
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/gmail.modify",
		"https://mail.google.com/",
	},
	"https://www.googleapis.com/auth/gmail.modify": {
		"https://www.googleapis.com/auth/gmail.modify",
		"https://mail.google.com/",
	},
}

// scopeLabel names a permission the way the consent screen does, so the error
// tells someone which checkbox to go back and tick.
var scopeLabel = map[string]string{
	"https://www.googleapis.com/auth/gmail.readonly":       "read your email",
	"https://www.googleapis.com/auth/gmail.send":           "send email on your behalf",
	"https://www.googleapis.com/auth/gmail.modify":         "manage your email",
	"https://www.googleapis.com/auth/gmail.compose":        "compose and send email",
	"https://www.googleapis.com/auth/gmail.metadata":       "read email metadata",
	"https://www.googleapis.com/auth/gmail.settings.basic": "manage your mail settings",
	"https://graph.microsoft.com/Mail.Send":                "send mail",
	"https://graph.microsoft.com/Mail.ReadWrite":           "read and write mail",
	"https://graph.microsoft.com/User.Read":                "read your profile",
}

// checkGrantedScopes refuses a connection the provider only partly authorised.
func checkGrantedScopes(ctx context.Context, provider models.InboxProvider, want []string, tok *oauth2.Token) *errx.Error {
	granted, told := grantedScopes(tok)
	if !told {
		// Nothing to check against. Recorded rather than guessed at, because a
		// provider that stops returning the scope would otherwise turn this
		// into a silent no-op.
		errs.CaptureMessageContext(ctx, fmt.Sprintf("%s returned no scope with the token; granted permissions were not verified", provider),
			errs.Tag("provider", string(provider)))
		return nil
	}

	var missing []string
	for _, w := range want {
		accepted, ok := scopeSatisfiedBy[w]
		if !ok {
			accepted = []string{w}
		}
		if !anyGranted(granted, accepted) {
			missing = append(missing, w)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	labels := make([]string, 0, len(missing))
	for _, m := range missing {
		if l, ok := scopeLabel[m]; ok {
			labels = append(labels, l)
		} else {
			labels = append(labels, m)
		}
	}
	log.Warn().
		Str("provider", string(provider)).
		Strs("missing", missing).
		Msg("mailbox onboarding: refused a partly granted consent")
	errs.CaptureMessageContext(ctx, fmt.Sprintf("%s mailbox connected with missing permissions", provider),
		errs.Tag("provider", string(provider)),
		errs.Extra("missing", strings.Join(missing, " ")))

	return errx.New(errx.BadRequest, fmt.Sprintf(
		"Warmbly was not given every permission it needs for this mailbox. Missing: %s. Connect it again and leave all the permissions ticked.",
		strings.Join(labels, ", ")))
}

func anyGranted(granted map[string]bool, accepted []string) bool {
	for _, a := range accepted {
		if granted[a] {
			return true
		}
	}
	return false
}

var httpClient = &http.Client{Timeout: 10 * time.Second}

func fetchGmailOwner(ctx context.Context, token string) (*inboxOwner, *errx.Error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, ownerLookupFailed(ctx, "gmail", "transport", 0, nil, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, ownerLookupFailed(ctx, "gmail", "status", resp.StatusCode, body, nil)
	}
	var out struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, ownerLookupFailed(ctx, "gmail", "decode", resp.StatusCode, body, err)
	}
	if out.EmailAddress == "" {
		return nil, ownerLookupFailed(ctx, "gmail", "no_address", resp.StatusCode, body, nil)
	}
	return &inboxOwner{Email: out.EmailAddress}, nil
}

// ownerLookupFailed reports why the provider would not name the mailbox and
// returns the one error the user sees.
//
// The user-facing message stays deliberately vague, because the cause is ours
// and not theirs. What is NOT vague any more is the record: this call used to
// read the status and the body and then discard both, so five distinct
// failures arrived as one sentence with nothing behind it, and the most common
// of them by far ("the Gmail API has not been enabled in this project") was
// indistinguishable from a revoked token. A handled 400 raises no exception,
// so without this there is nothing in the logs or in error tracking either.
func ownerLookupFailed(ctx context.Context, provider, stage string, status int, body []byte, cause error) *errx.Error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 512 {
		detail = detail[:512] + "…"
	}
	opts := []errs.Option{
		errs.Tag("provider", provider),
		errs.Tag("stage", stage),
		errs.Extra("status", status),
	}
	// Only present on a failure response, where it is the provider's own error
	// payload rather than anything belonging to the mailbox owner.
	if detail != "" {
		opts = append(opts, errs.Extra("response", detail))
	}
	if cause != nil {
		errs.CaptureExceptionContext(ctx, fmt.Errorf("%s owner lookup failed at %s: %w", provider, stage, cause), opts...)
	} else {
		errs.CaptureMessageContext(ctx, fmt.Sprintf("%s owner lookup failed at %s (status %d)", provider, stage, status), opts...)
	}
	log.Warn().
		Str("provider", provider).
		Str("stage", stage).
		Int("status", status).
		Str("response", detail).
		Msg("mailbox onboarding: provider would not name the account")
	return errx.ErrEmailOnboardUserInfo
}

func fetchOutlookOwner(ctx context.Context, token string) (*inboxOwner, *errx.Error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, ownerLookupFailed(ctx, "outlook", "transport", 0, nil, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, ownerLookupFailed(ctx, "outlook", "status", resp.StatusCode, body, nil)
	}
	var out struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, ownerLookupFailed(ctx, "outlook", "decode", resp.StatusCode, body, err)
	}
	addr := out.Mail
	if addr == "" {
		addr = out.UserPrincipalName
	}
	if addr == "" {
		return nil, ownerLookupFailed(ctx, "outlook", "no_address", resp.StatusCode, nil, nil)
	}
	return &inboxOwner{Email: addr, Name: out.DisplayName}, nil
}
