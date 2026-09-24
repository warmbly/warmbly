package email

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// Brokered OAuth: consent on this deployment's OAuth app for a linked instance; the grant stays here.

func (s *emailService) OAuthAuthorizeURL(provider models.InboxProvider, state string) (string, *errx.Error) {
	cfg, xerr := s.oauthConfigFor(provider)
	if xerr != nil {
		return "", xerr
	}
	return cfg.AuthCodeURL(state, authCodeOptions(provider, "")...), nil
}

func (s *emailService) OAuthConnectWithCode(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider, code string) (*models.Email, *errx.Error) {
	ctx, cancel := detach(ctx, connectBudget)
	defer cancel()
	if code = strings.TrimSpace(code); code == "" {
		return nil, errx.ErrEmailOnboardCode
	}
	allowance, xerr := s.guardInboxLimit(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	cfg, xerr := s.oauthConfigFor(provider)
	if xerr != nil {
		return nil, xerr
	}
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return nil, errx.ErrEmailOnboardExchange
	}

	// The same two guards the first-party connect applies. Without them a
	// brokered mailbox could be stored after a consent the person half granted,
	// or with no refresh token at all, and it would read as connected until its
	// first send failed days later.
	if xerr := checkGrantedScopes(ctx, provider, cfg.Scopes, tok); xerr != nil {
		return nil, xerr
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, errx.New(errx.BadRequest,
			"The provider did not return a long-lived token for this mailbox, so it would stop working within the hour. "+
				"Remove Warmbly's access in your account settings and connect it again.")
	}

	owner, xerr := fetchInboxOwner(ctx, provider, tok.AccessToken)
	if xerr != nil {
		return nil, xerr
	}
	if existing, xerr := s.findExisting(ctx, userID, orgID, owner.Email); xerr != nil {
		return nil, xerr
	} else if existing != nil {
		return nil, errx.ErrEmailOnboardAlreadyExists
	}
	name := strings.TrimSpace(owner.Name)
	if name == "" {
		name = deriveNameFromEmail(owner.Email)
	}
	acc, xerr := s.emailRepository.NewOauthAccount(ctx, userID, models.NewOauthAccount{
		OrganizationID: orgID,
		Allowance:      allowance,
		Provider:       provider,
		Name:           name,
		Email:          owner.Email,
		MailHost:       oauthMailHost(provider, owner.Email),
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		ExpiresAt:      tok.Expiry,
	})
	if xerr != nil {
		return nil, xerr
	}
	s.captureSendIdentity(ctx, acc, tok)
	s.syncWarmupPoolMembership(ctx, acc)
	s.publishAccountEvent(ctx, pubsub.EventAccountConnected, acc)
	s.dispatchAccountConnected(ctx, orgID, acc)
	s.loadAccountBestEffort(ctx, acc.ID)
	return acc, nil
}

// OAuthAccessToken refreshes and re-seals the grant when within two minutes of expiry.
func (s *emailService) OAuthAccessToken(ctx context.Context, accountID uuid.UUID) (*oauth2.Token, *errx.Error) {
	acc, xerr := s.emailRepository.GetByID(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	if acc == nil || acc.AuthMethod == models.MailAuthDelegated {
		return nil, errx.ErrEmailCredentials
	}
	creds, xerr := s.emailRepository.GetOAuthCredentials(ctx, accountID)
	if xerr != nil {
		return nil, xerr
	}
	current := &oauth2.Token{AccessToken: creds.AccessToken, RefreshToken: creds.RefreshToken, Expiry: creds.ExpiresAt}
	if current.AccessToken != "" && time.Until(current.Expiry) > 2*time.Minute {
		return current, nil
	}
	// The client config is only needed to refresh.
	cfg, xerr := s.oauthConfigFor(models.InboxProvider(acc.Provider))
	if xerr != nil {
		return nil, xerr
	}
	fresh, err := cfg.TokenSource(ctx, current).Token()
	if err != nil {
		log.Warn().Err(err).Str("account_id", accountID.String()).Msg("brokered token refresh failed")
		return nil, errx.ErrEmailCredentials
	}
	if fresh.RefreshToken == "" {
		fresh.RefreshToken = current.RefreshToken
	}
	if err := s.emailRepository.RefreshBoxToken(ctx, accountID, fresh.AccessToken, fresh.RefreshToken, fresh.Expiry); err != nil {
		log.Warn().Err(err).Str("account_id", accountID.String()).Msg("brokered token persist failed")
	}
	return fresh, nil
}
