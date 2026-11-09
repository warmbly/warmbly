package email

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// ErrConnectLinkWrongMailbox refuses a sign-in for an address other than the one the link names.
var ErrConnectLinkWrongMailbox = errx.NewWithIdentifier(errx.Forbidden, "connect_link_wrong_mailbox",
	"This link is for a different mailbox. Sign in with the address it was created for.")

// LinkSigninURL opens a sign-in for a link through the client the workspace's sign-ins use.
func (s *emailService) LinkSigninURL(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider, state, verifier, loginHint string) (string, *uuid.UUID, *errx.Error) {
	if !models.ValidMailboxOAuthAppProvider(provider) {
		return "", nil, errx.ErrEmailOnboardProvider
	}
	if verifier == "" {
		return "", nil, errx.InternalError()
	}
	cfg, appID, xerr := s.signinConfig(ctx, &orgID, provider)
	if provider == models.InboxProviderGoogle && appID == nil && !config.GoogleOAuthConnect() &&
		(xerr == nil || xerr == errx.ErrEmailOnboardGoogleNotConfigured) {
		return "", nil, errx.ErrEmailOnboardGoogleOAuthDisabled
	}
	if xerr != nil {
		return "", nil, xerr
	}
	if _, xerr := s.guardInboxLimit(ctx, &orgID); xerr != nil {
		return "", nil, xerr
	}
	opts := append(authCodeOptions(provider, loginHintOrEmpty(loginHint)), oauth2.S256ChallengeOption(verifier))
	return cfg.AuthCodeURL(state, opts...), appID, nil
}

// LinkSigninFinish redeems a link's code with the client it started with and connects the mailbox.
func (s *emailService) LinkSigninFinish(ctx context.Context, userID string, orgID uuid.UUID, provider models.InboxProvider, appID *uuid.UUID, code, verifier, expectedEmail string) (*models.Email, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
	if code = strings.TrimSpace(code); code == "" {
		return nil, errx.ErrEmailOnboardCode
	}
	if verifier == "" {
		return nil, errx.ErrEmailOnboardState
	}
	// A plan change after the link opened still holds: this deployment's client is refused here too.
	if appID == nil {
		if xerr := s.RequireOwnOAuthApp(ctx, orgID); xerr != nil {
			return nil, xerr
		}
	}
	allowance, xerr := s.guardInboxLimit(ctx, &orgID)
	if xerr != nil {
		return nil, xerr
	}
	cfg, xerr := s.grantConfig(ctx, appID, provider)
	if xerr != nil {
		return nil, xerr
	}
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, errx.ErrEmailOnboardExchange
	}
	if xerr := checkGrantedScopes(ctx, provider, cfg.Scopes, tok); xerr != nil {
		return nil, xerr
	}
	owner, xerr := fetchInboxOwner(ctx, provider, tok.AccessToken)
	if xerr != nil {
		return nil, xerr
	}
	if expectedEmail != "" && !strings.EqualFold(strings.TrimSpace(owner.Email), strings.TrimSpace(expectedEmail)) {
		return nil, ErrConnectLinkWrongMailbox
	}
	if provider == models.InboxProviderGoogle && personalGmail(owner.Email) {
		return nil, errx.ErrEmailOnboardGooglePersonal
	}
	if existing, xerr := s.findExisting(ctx, userID, &orgID, owner.Email); xerr != nil {
		return nil, xerr
	} else if existing != nil {
		return nil, errx.ErrEmailOnboardAlreadyExists
	}
	return s.createOAuthMailbox(ctx, userID, &orgID, allowance, provider, tok, owner, appID)
}
