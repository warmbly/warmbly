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
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce), nil
}

func (s *emailService) OAuthConnectWithCode(ctx context.Context, userID string, orgID *uuid.UUID, provider models.InboxProvider, code string) (*models.Email, *errx.Error) {
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
	acc, xerr := s.emailRepository.NewOauthAccount(ctx, userID, models.NewOauthAccount{
		OrganizationID: orgID,
		Allowance:      allowance,
		Provider:       provider,
		Name:           name,
		Email:          owner.Email,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		ExpiresAt:      tok.Expiry,
	})
	if xerr != nil {
		return nil, xerr
	}
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
