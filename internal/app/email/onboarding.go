package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/dailythrottle"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
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
	if xerr := s.guardInboxLimit(ctx, orgID); xerr != nil {
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
	}); xerr != nil {
		return nil, xerr
	}

	url := cfg.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce, // force refresh_token issuance on reconnect
	)
	return &models.EmailOnboardingStartResponse{URL: url, State: state}, nil
}

// guardMailboxThrottle bounds new-mailbox connection rate per org per
// day so abuse paths (or accidents) can't connect 200 mailboxes in
// one tab session. The budget is keyed by org, so a request without
// one is refused rather than exempted. The check fires only on the
// actual create paths, not on OAuthStart, so retrying a failed flow
// doesn't consume the day's budget.
func (s *emailService) guardMailboxThrottle(ctx context.Context, orgID *uuid.UUID) *errx.Error {
	if orgID == nil {
		return errx.ErrNoOrganization
	}
	if s.throttle == nil {
		return nil
	}
	return s.throttle.CheckAndIncrement(ctx, *orgID, dailythrottle.ResourceMailbox, config.DailyThrottleNewMailboxes)
}

// guardInboxLimit enforces the per-org inbox cap for free-trial users.
// Returns nil (allowed) for paid orgs and for trial orgs under the cap.
// Trial orgs that have already connected one inbox get
// ErrEmailOnboardInboxLimit; orgs without an active subscription or trial
// get ErrEmailOnboardTrialExpired. The cap is counted per org, so no org
// means the cap cannot be applied and the connect is refused.
func (s *emailService) guardInboxLimit(ctx context.Context, orgID *uuid.UUID) *errx.Error {
	if orgID == nil {
		return errx.ErrNoOrganization
	}
	if s.featureGate == nil {
		return nil
	}
	count, xerr := s.emailRepository.CountForOrganization(ctx, *orgID)
	if xerr != nil {
		return xerr
	}
	allowed, xerr := s.featureGate.CanAddInbox(ctx, *orgID, count)
	if xerr != nil {
		return xerr
	}
	if allowed {
		return nil
	}
	return errx.ErrEmailOnboardInboxLimit
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
	if sess.EmailAccountID == nil {
		if xerr := s.guardInboxLimit(ctx, sess.OrganizationID); xerr != nil {
			return nil, false, xerr
		}
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

	name := strings.TrimSpace(owner.Name)
	if name == "" {
		name = deriveNameFromEmail(owner.Email)
	}

	if xerr := s.guardMailboxThrottle(ctx, sess.OrganizationID); xerr != nil {
		return nil, false, xerr
	}

	acc, xerr := s.emailRepository.NewOauthAccount(ctx, userID, models.NewOauthAccount{
		OrganizationID: sess.OrganizationID,
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

	if xerr := s.guardInboxLimit(ctx, orgID); xerr != nil {
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

	// Pick any healthy worker for the one-shot validation handshake. Tier is
	// irrelevant here (nothing is placed yet, the worker just dials the
	// credentials once), so fall back to the other tier rather than failing:
	// asking only for free-tier workers made onboarding impossible on any
	// deployment whose workers all register as premium, which includes a stock
	// self-host install.
	w, werr := s.workerAssignment.SelectSharedWorker(ctx, false)
	if werr != nil || w == nil {
		w, werr = s.workerAssignment.SelectSharedWorker(ctx, true)
	}
	if werr != nil || w == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}

	creds := &models.SmtpImap{SMTP: data.SMTP, IMAP: data.IMAP}
	if xerr := s.ValidateCredentials(ctx, *orgID, w.ID.String(), creds); xerr != nil {
		return nil, xerr
	}

	if xerr := s.guardMailboxThrottle(ctx, orgID); xerr != nil {
		return nil, xerr
	}

	data.OrganizationID = orgID

	acc, xerr := s.emailRepository.NewSMTPIMAPAccount(ctx, userID, *data)
	if xerr != nil {
		return nil, xerr
	}

	// Assign the long-term worker (free vs paid tier). Failure here is non-fatal:
	// the scheduler will pick the account up on its next pass.
	if orgID != nil {
		if _, err := s.workerAssignment.AssignWorkerToEmail(ctx, acc.ID, *orgID); err != nil {
			sentry.CaptureException(err)
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
		sentry.CaptureException(err)
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
func validateMailSecurity(smtp, imap *models.Service) *errx.Error {
	if smtp.Security != "" && !models.ValidMailSecurity(smtp.Security) {
		return errx.ErrEmailSMTPSecurity
	}
	if imap.Security != "" && !models.ValidMailSecurity(imap.Security) {
		return errx.ErrEmailIMAPSecurity
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

var httpClient = &http.Client{Timeout: 10 * time.Second}

func fetchGmailOwner(ctx context.Context, token string) (*inboxOwner, *errx.Error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	var out struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	if out.EmailAddress == "" {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	return &inboxOwner{Email: out.EmailAddress}, nil
}

func fetchOutlookOwner(ctx context.Context, token string) (*inboxOwner, *errx.Error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://graph.microsoft.com/v1.0/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	var out struct {
		Mail              string `json:"mail"`
		UserPrincipalName string `json:"userPrincipalName"`
		DisplayName       string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	addr := out.Mail
	if addr == "" {
		addr = out.UserPrincipalName
	}
	if addr == "" {
		return nil, errx.ErrEmailOnboardUserInfo
	}
	return &inboxOwner{Email: addr, Name: out.DisplayName}, nil
}
