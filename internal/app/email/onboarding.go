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
	"github.com/warmbly/warmbly/internal/errx"
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

// OAuthFinish validates the state, exchanges the code for tokens, fetches the inbox owner,
// and persists a new email account.
func (s *emailService) OAuthFinish(ctx context.Context, userID, code, state string) (*models.Email, *errx.Error) {
	if code = strings.TrimSpace(code); code == "" {
		return nil, errx.ErrEmailOnboardCode
	}
	if state = strings.TrimSpace(state); state == "" {
		return nil, errx.ErrEmailOnboardState
	}

	sess, xerr := s.takeOnboardingState(ctx, state)
	if xerr != nil {
		return nil, xerr
	}
	if sess.UserID != userID {
		return nil, errx.ErrEmailOnboardState
	}

	provider := models.InboxProvider(sess.Provider)
	cfg, xerr := s.oauthConfigFor(provider)
	if xerr != nil {
		return nil, xerr
	}

	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, errx.ErrEmailOnboardExchange
	}

	owner, xerr := fetchInboxOwner(ctx, provider, tok.AccessToken)
	if xerr != nil {
		return nil, xerr
	}

	if exists, xerr := s.emailRepository.ExistsForUser(ctx, userID, owner.Email); xerr != nil {
		return nil, xerr
	} else if exists {
		return nil, errx.ErrEmailOnboardAlreadyExists
	}

	name := strings.TrimSpace(owner.Name)
	if name == "" {
		name = deriveNameFromEmail(owner.Email)
	}

	return s.emailRepository.NewOauthAccount(ctx, userID, models.NewOauthAccount{
		OrganizationID: sess.OrganizationID,
		Provider:       provider,
		Name:           name,
		Email:          owner.Email,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		ExpiresAt:      tok.Expiry,
	})
}

// OnboardSMTPIMAP validates the supplied SMTP/IMAP credentials against a live worker, then
// persists the email account on success. Returns ErrEmailCredentials if the worker reports failure.
func (s *emailService) OnboardSMTPIMAP(ctx context.Context, userID string, orgID *uuid.UUID, data *models.NewSMTPIMAPAccount) (*models.Email, *errx.Error) {
	if xerr := validateSMTPIMAPInput(data); xerr != nil {
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

	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, errx.ErrUser
	}

	// Pick any healthy worker for the one-shot validation handshake.
	w, werr := s.workerAssignment.SelectSharedWorker(ctx, true)
	if werr != nil || w == nil {
		return nil, errx.ErrEmailOnboardNoWorker
	}

	creds := &models.SmtpImap{SMTP: data.SMTP, IMAP: data.IMAP}
	if xerr := s.ValidateCredentials(ctx, uid, w.ID.String(), creds); xerr != nil {
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

	return acc, nil
}

func (s *emailService) oauthConfigFor(provider models.InboxProvider) (*oauth2.Config, *errx.Error) {
	if s.oauthInbox == nil {
		return nil, errx.InternalError()
	}
	switch provider {
	case models.InboxProviderGoogle:
		if s.oauthInbox.Google == nil {
			return nil, errx.InternalError()
		}
		return s.oauthInbox.Google, nil
	case models.InboxProviderOutlook:
		if s.oauthInbox.Outlook == nil {
			return nil, errx.InternalError()
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
	if data.SMTP.Port != 465 && data.SMTP.Port != 587 {
		return errx.ErrEmailSMTPPort
	}
	if strings.TrimSpace(data.IMAP.Host) == "" {
		return errx.ErrEmailIMAPHost
	}
	if data.IMAP.Port <= 0 {
		return errx.ErrEmailIMAPPort
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
