// Package oauthapp holds a workspace's own Google and Microsoft OAuth clients,
// which every mailbox sign-in the workspace starts goes through.
package oauthapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

var (
	ErrProvider   = errx.NewWithIdentifier(errx.BadRequest, "oauth_app_provider", "The provider must be gmail or outlook.")
	ErrClientID   = errx.NewWithIdentifier(errx.BadRequest, "oauth_app_client_id", "That is not a client ID for this provider. Google client IDs end in .apps.googleusercontent.com; Microsoft ones are the Application (client) ID, a GUID.")
	ErrSecret     = errx.NewWithIdentifier(errx.BadRequest, "oauth_app_client_secret", "A client secret is required for a new app.")
	ErrRedirect   = errx.NewWithIdentifier(errx.BadRequest, "oauth_app_redirect_url", "The redirect URL must be an https address with a path and no fragment.")
	ErrRejected   = errx.NewWithIdentifier(errx.Unprocessable, "oauth_app_rejected", "The provider did not accept this client ID and secret. Check both, and that the secret has not expired.")
	ErrInUse      = errx.NewWithIdentifier(errx.Conflict, "oauth_app_in_use", "Mailboxes are signed in through this app. Remove them or sign them in through another app first; a new secret for the same client ID can be saved at any time.")
	ErrNotFound   = errx.NewWithIdentifier(errx.NotFound, "oauth_app_not_found", "This workspace has no OAuth app for that provider.")
	ErrUnreadable = errx.NewWithIdentifier(errx.Internal, "oauth_app_unreadable", "This workspace's OAuth app could not be read.")
	ErrNoSecret   = errx.NewWithIdentifier(errx.Unprocessable, "oauth_app_secret_missing", "This workspace's OAuth app has no client secret saved, which happens after a workspace import without credentials. Save the secret again under Settings > Mailbox sign-in.")
)

// Service manages workspace OAuth clients and resolves the client a sign-in or refresh uses.
type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]*models.MailboxOAuthApp, *errx.Error)
	Save(ctx context.Context, orgID, userID uuid.UUID, provider models.InboxProvider, in models.MailboxOAuthAppInput) (*models.MailboxOAuthApp, *errx.Error)
	Delete(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) *errx.Error
	// ConfigFor returns the workspace's client for provider and its id, or nil when it has none.
	ConfigFor(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (*oauth2.Config, *uuid.UUID, *errx.Error)
	// ConfigByID returns the client that issued a recorded grant.
	ConfigByID(ctx context.Context, appID uuid.UUID) (*oauth2.Config, *errx.Error)
	// Callbacks is this deployment's own callback per provider, which a client registers unless it forwards from its own.
	Callbacks() map[models.InboxProvider]string
}

type service struct {
	repo     repository.MailboxOAuthAppRepository
	cipher   cipher.CipherService
	defaults *config.Oauth2Inbox
	probe    func(ctx context.Context, cfg *oauth2.Config) error
}

// New returns the service. defaults carries this deployment's scopes, endpoints and callback.
func New(repo repository.MailboxOAuthAppRepository, cipherSvc cipher.CipherService, defaults *config.Oauth2Inbox) Service {
	return &service{repo: repo, cipher: cipherSvc, defaults: defaults, probe: probeClient}
}

func internalErr(err error, msg string) *errx.Error {
	errs.CaptureException(fmt.Errorf("%s: %w", msg, err))
	return errx.InternalError()
}

func (s *service) base(provider models.InboxProvider) *oauth2.Config {
	if s.defaults == nil {
		return nil
	}
	switch provider {
	case models.InboxProviderGoogle:
		return s.defaults.Google
	case models.InboxProviderOutlook:
		return s.defaults.Outlook
	}
	return nil
}

// present fills the callback the client has to register.
func (s *service) present(a *models.MailboxOAuthApp) *models.MailboxOAuthApp {
	if !a.CustomRedirect {
		if b := s.base(a.Provider); b != nil {
			a.RedirectURL = b.RedirectURL
		}
	}
	return a
}

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]*models.MailboxOAuthApp, *errx.Error) {
	apps, err := s.repo.ListMailboxOAuthApps(ctx, orgID)
	if err != nil {
		return nil, internalErr(err, "list oauth apps")
	}
	for _, a := range apps {
		s.present(a)
	}
	return apps, nil
}

func validClientID(provider models.InboxProvider, id string) bool {
	if id == "" || len(id) > 512 || strings.ContainsAny(id, " \t\r\n") {
		return false
	}
	if provider == models.InboxProviderGoogle {
		return strings.HasSuffix(id, ".apps.googleusercontent.com")
	}
	_, err := uuid.Parse(id)
	return err == nil
}

func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.Host != "" && u.User == nil &&
		u.Path != "" && u.Path != "/" && u.Fragment == "" && !strings.Contains(raw, "#")
}

func (s *service) Save(ctx context.Context, orgID, userID uuid.UUID, provider models.InboxProvider, in models.MailboxOAuthAppInput) (*models.MailboxOAuthApp, *errx.Error) {
	if !models.ValidMailboxOAuthAppProvider(provider) {
		return nil, ErrProvider
	}
	base := s.base(provider)
	if base == nil {
		return nil, errx.InternalError()
	}
	clientID := strings.TrimSpace(in.ClientID)
	if !validClientID(provider, clientID) {
		return nil, ErrClientID
	}
	name, xerr := displayname.Validate("App name", in.AppName, displayname.Workspace, false)
	if xerr != nil {
		return nil, xerr
	}
	redirect := strings.TrimSpace(in.RedirectURL)
	if redirect == base.RedirectURL {
		redirect = ""
	}
	if redirect != "" && !validRedirect(redirect) {
		return nil, ErrRedirect
	}

	current, err := s.repo.GetMailboxOAuthApp(ctx, orgID, provider)
	if err != nil {
		return nil, internalErr(err, "get oauth app")
	}
	secret := strings.TrimSpace(in.ClientSecret)
	if len(secret) > 1024 {
		return nil, ErrSecret
	}
	if secret == "" {
		// Keeping the stored secret is only meaningful for the client that owns it.
		if current == nil || current.ClientID != clientID || current.ClientSecret == "" {
			return nil, ErrSecret
		}
		if secret, xerr = s.open(ctx, orgID, current.ClientSecret); xerr != nil {
			return nil, xerr
		}
	}

	cfg := s.configFrom(base, clientID, secret, redirect)
	if err := s.probe(ctx, cfg); err != nil {
		if errors.Is(err, errClientRejected) {
			return nil, ErrRejected
		}
		// An unreachable provider says nothing about the client; the first sign-in will.
		errs.CaptureException(fmt.Errorf("probe oauth client: %w", err))
	}

	sealed, xerr := s.seal(ctx, orgID, secret)
	if xerr != nil {
		return nil, xerr
	}
	by := userID
	saved, err := s.repo.UpsertMailboxOAuthApp(ctx, &models.MailboxOAuthApp{
		OrganizationID: orgID, Provider: provider, ClientID: clientID, ClientSecret: sealed,
		AppName: name, RedirectURL: redirect, CreatedBy: &by,
	})
	if errors.Is(err, repository.ErrMailboxOAuthAppInUse) {
		return nil, ErrInUse
	}
	if err != nil {
		if repository.IsForeignKeyViolation(err) {
			return nil, errx.ErrNoOrganization
		}
		return nil, internalErr(err, "upsert oauth app")
	}
	saved.ClientSecret = ""
	return s.present(saved), nil
}

func (s *service) Delete(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) *errx.Error {
	if !models.ValidMailboxOAuthAppProvider(provider) {
		return ErrProvider
	}
	deleted, err := s.repo.DeleteMailboxOAuthApp(ctx, orgID, provider)
	if errors.Is(err, repository.ErrMailboxOAuthAppInUse) {
		return ErrInUse
	}
	if err != nil {
		return internalErr(err, "delete oauth app")
	}
	if !deleted {
		return ErrNotFound
	}
	return nil
}

func (s *service) ConfigFor(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (*oauth2.Config, *uuid.UUID, *errx.Error) {
	a, err := s.repo.GetMailboxOAuthApp(ctx, orgID, provider)
	if err != nil {
		return nil, nil, internalErr(err, "get oauth app")
	}
	if a == nil {
		return nil, nil, nil
	}
	cfg, xerr := s.build(ctx, a)
	if xerr != nil {
		return nil, nil, xerr
	}
	id := a.ID
	return cfg, &id, nil
}

func (s *service) ConfigByID(ctx context.Context, appID uuid.UUID) (*oauth2.Config, *errx.Error) {
	a, err := s.repo.GetMailboxOAuthAppByID(ctx, appID)
	if err != nil {
		return nil, internalErr(err, "get oauth app by id")
	}
	if a == nil {
		return nil, ErrNotFound
	}
	return s.build(ctx, a)
}

func (s *service) build(ctx context.Context, a *models.MailboxOAuthApp) (*oauth2.Config, *errx.Error) {
	base := s.base(a.Provider)
	if base == nil {
		return nil, ErrProvider
	}
	if a.ClientSecret == "" {
		return nil, ErrNoSecret
	}
	secret, xerr := s.open(ctx, a.OrganizationID, a.ClientSecret)
	if xerr != nil {
		return nil, xerr
	}
	return s.configFrom(base, a.ClientID, secret, a.RedirectURL), nil
}

func (s *service) configFrom(base *oauth2.Config, clientID, secret, redirect string) *oauth2.Config {
	cfg := *base
	cfg.Scopes = append([]string(nil), base.Scopes...)
	cfg.ClientID, cfg.ClientSecret = clientID, secret
	if redirect != "" {
		cfg.RedirectURL = redirect
	}
	return &cfg
}

func (s *service) seal(ctx context.Context, orgID uuid.UUID, plain string) (string, *errx.Error) {
	if s.cipher == nil {
		return "", errx.InternalError()
	}
	c, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		return "", internalErr(err, "oauth app cipher")
	}
	out, err := c.Encrypt(ctx, plain)
	if err != nil {
		return "", internalErr(err, "seal oauth app secret")
	}
	return out, nil
}

func (s *service) open(ctx context.Context, orgID uuid.UUID, sealed string) (string, *errx.Error) {
	if s.cipher == nil {
		return "", errx.InternalError()
	}
	c, err := s.cipher.Cipher(ctx, orgID)
	if err != nil {
		errs.CaptureException(fmt.Errorf("oauth app cipher: %w", err))
		return "", ErrUnreadable
	}
	out, err := c.Decrypt(ctx, sealed)
	if err != nil || out == "" {
		errs.CaptureException(fmt.Errorf("open oauth app secret: %w", err))
		return "", ErrUnreadable
	}
	return out, nil
}

var errClientRejected = errors.New("client rejected")

// probeClient redeems a code that cannot exist: invalid_client means the pair is wrong, anything else that it is known.
func probeClient(ctx context.Context, cfg *oauth2.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{Timeout: 8 * time.Second})
	_, err := cfg.Exchange(ctx, "warmbly-client-check")
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode == "invalid_client" || re.ErrorCode == "unauthorized_client" {
			return errClientRejected
		}
		return nil
	}
	return err
}

func (s *service) Callbacks() map[models.InboxProvider]string {
	out := map[models.InboxProvider]string{}
	for _, p := range models.MailboxOAuthAppProviders {
		if b := s.base(p); b != nil {
			out[p] = b.RedirectURL
		}
	}
	return out
}
