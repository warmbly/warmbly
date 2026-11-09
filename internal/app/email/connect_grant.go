package email

import (
	"context"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// A workspace that runs its own OAuth app can hand Warmbly a sign-in that app
// already completed, so the mailbox owner signs in once, on the workspace's
// own site. Only a grant the workspace's app issued is accepted: refreshing or
// redeeming it needs that app's secret, which only the backend holds.

var (
	ErrGrantAppRequired = errx.NewWithIdentifier(errx.Unprocessable, "connect_grant_app_required",
		"Save your own OAuth app for this provider first. Warmbly accepts only sign-ins your own app issued.")
	ErrGrantInput = errx.NewWithIdentifier(errx.BadRequest, "connect_grant_input",
		"Send either refresh_token, or code with the redirect_uri it was issued for.")
	ErrGrantRedirect = errx.NewWithIdentifier(errx.BadRequest, "connect_grant_redirect_uri",
		"redirect_uri must be the https callback the code was issued for.")
	ErrGrantRefused = errx.NewWithIdentifier(errx.Unprocessable, "connect_grant_refused",
		"The provider refused this sign-in: the token was revoked or expired, the code was already used or issued for another redirect_uri, or it came from a different app than the one saved for this workspace.")
	ErrGrantOffline = errx.NewWithIdentifier(errx.Unprocessable, "connect_grant_offline",
		"The sign-in carries no refresh token, so the mailbox would stop working within the hour. Ask for offline access: access_type=offline and prompt=consent on Google, the offline_access scope on Microsoft.")
	ErrGrantWrongMailbox = errx.NewWithIdentifier(errx.Forbidden, "connect_grant_wrong_mailbox",
		"This sign-in is for a different mailbox than the email you named.")
	ErrGrantMailboxNotAllowed = errx.NewWithIdentifier(errx.Forbidden, "connect_grant_mailbox_not_allowed",
		"This sign-in renews a mailbox outside the mailboxes this credential may act on.")
)

func validGrantRedirect(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == ""
}

// redeemGrant turns a handed-over grant into a fresh token, through the workspace's own app only.
func (s *emailService) redeemGrant(ctx context.Context, orgID uuid.UUID, g models.MailboxOAuthGrant) (*oauth2.Token, *oauth2.Config, *uuid.UUID, *errx.Error) {
	if !models.ValidMailboxOAuthAppProvider(g.Provider) {
		return nil, nil, nil, errx.ErrEmailOnboardProvider
	}
	refresh, code := strings.TrimSpace(g.RefreshToken), strings.TrimSpace(g.Code)
	if (refresh == "") == (code == "") || len(refresh) > 4096 || len(code) > 4096 {
		return nil, nil, nil, ErrGrantInput
	}
	if s.oauthApps == nil {
		return nil, nil, nil, ErrGrantAppRequired
	}
	cfg, appID, xerr := s.oauthApps.ConfigFor(ctx, orgID, g.Provider)
	if xerr != nil {
		return nil, nil, nil, xerr
	}
	if cfg == nil {
		return nil, nil, nil, ErrGrantAppRequired
	}
	var tok *oauth2.Token
	var err error
	if code != "" {
		redirect := strings.TrimSpace(g.RedirectURI)
		if !validGrantRedirect(redirect) {
			return nil, nil, nil, ErrGrantRedirect
		}
		exchange := *cfg
		exchange.RedirectURL = redirect
		var opts []oauth2.AuthCodeOption
		if v := strings.TrimSpace(g.CodeVerifier); v != "" {
			opts = append(opts, oauth2.VerifierOption(v))
		}
		tok, err = exchange.Exchange(ctx, code, opts...)
	} else {
		tok, err = cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh}).Token()
		// Google keeps the refresh token it was given; Microsoft hands back a new one.
		if err == nil && tok.RefreshToken == "" {
			tok.RefreshToken = refresh
		}
	}
	if err != nil {
		return nil, nil, nil, ErrGrantRefused
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, nil, nil, ErrGrantOffline
	}
	if xerr := checkGrantedScopes(ctx, g.Provider, cfg.Scopes, tok); xerr != nil {
		return nil, nil, nil, xerr
	}
	return tok, cfg, appID, nil
}

// ConnectWithGrant connects the mailbox a handed-over sign-in belongs to, or renews its token when the workspace already has it.
func (s *emailService) ConnectWithGrant(ctx context.Context, userID string, orgID uuid.UUID, g models.MailboxOAuthGrant, allow func(uuid.UUID) bool) (*models.Email, bool, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
	tok, _, appID, xerr := s.redeemGrant(ctx, orgID, g)
	if xerr != nil {
		return nil, false, xerr
	}
	owner, xerr := fetchInboxOwner(ctx, g.Provider, tok.AccessToken)
	if xerr != nil {
		return nil, false, xerr
	}
	if want := strings.TrimSpace(g.Email); want != "" && !strings.EqualFold(want, strings.TrimSpace(owner.Email)) {
		return nil, false, ErrGrantWrongMailbox
	}
	if g.Provider == models.InboxProviderGoogle && personalGmail(owner.Email) {
		return nil, false, errx.ErrEmailOnboardGooglePersonal
	}
	existing, xerr := s.findExisting(ctx, userID, &orgID, owner.Email)
	if xerr != nil {
		return nil, false, xerr
	}
	if existing != nil {
		// A caller held to some mailboxes renews only those.
		if allow != nil && !allow(existing.ID) {
			return nil, false, ErrGrantMailboxNotAllowed
		}
		acc, xerr := s.renewWithGrant(ctx, orgID, existing, g.Provider, tok, appID)
		return acc, true, xerr
	}
	allowance, xerr := s.guardInboxLimit(ctx, &orgID)
	if xerr != nil {
		return nil, false, xerr
	}
	acc, xerr := s.createOAuthMailbox(ctx, userID, &orgID, allowance, g.Provider, tok, owner, appID)
	// A concurrent hand-over for the same address connected it first; this one renews that mailbox.
	if xerr == errx.ErrEmailOnboardAlreadyExists {
		existing, ferr := s.findExisting(ctx, userID, &orgID, owner.Email)
		if ferr != nil {
			return nil, false, ferr
		}
		if existing != nil {
			if allow != nil && !allow(existing.ID) {
				return nil, false, ErrGrantMailboxNotAllowed
			}
			acc, xerr := s.renewWithGrant(ctx, orgID, existing, g.Provider, tok, appID)
			return acc, true, xerr
		}
	}
	return acc, false, xerr
}

// renewWithGrant replaces the token of a mailbox the workspace already has, for the same address and provider.
func (s *emailService) renewWithGrant(ctx context.Context, orgID uuid.UUID, existing *models.EmailRef, provider models.InboxProvider, tok *oauth2.Token, appID *uuid.UUID) (*models.Email, *errx.Error) {
	if models.InboxProvider(existing.Provider) != provider || existing.AuthMethod != models.MailAuthOAuth || existing.Managed {
		return nil, errx.ErrEmailOnboardAlreadyExists
	}
	account, xerr := s.emailRepository.GetByID(ctx, existing.ID)
	if xerr != nil {
		return nil, xerr
	}
	if account == nil || account.OrganizationID == nil || *account.OrganizationID != orgID {
		return nil, errx.ErrNotFound
	}
	if err := s.emailRepository.ReplaceBoxToken(ctx, account.ID, tok.AccessToken, tok.RefreshToken, tok.Expiry, recordedScopes(tok), appID); err != nil {
		return nil, errx.InternalError()
	}
	s.captureSendIdentityList(ctx, account, tok)
	return s.reconnectAccount(ctx, account.ID)
}
