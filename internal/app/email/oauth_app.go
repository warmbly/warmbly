package email

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// ErrPartnershipOAuthAppRequired refuses a sign-in through this deployment's own client on the Partnership plan.
var ErrPartnershipOAuthAppRequired = errx.NewWithIdentifier(errx.Unprocessable, "partnership_oauth_app_required",
	"On the Partnership plan, Google and Microsoft mailboxes sign in through your own OAuth app. Add it under Settings > Mailbox sign-in, or connect the mailbox with an app password.")

// OAuthApps resolves a workspace's own OAuth client; oauthapp.Service satisfies it.
type OAuthApps interface {
	ConfigFor(ctx context.Context, orgID uuid.UUID, provider models.InboxProvider) (*oauth2.Config, *uuid.UUID, *errx.Error)
	ConfigByID(ctx context.Context, appID uuid.UUID) (*oauth2.Config, *errx.Error)
}

// PartnershipGate reports a workspace on the Partnership plan; feature.FeatureGateService satisfies it.
type PartnershipGate interface {
	HasPartnership(ctx context.Context, orgID uuid.UUID) (bool, *errx.Error)
}

// WireOAuthApps lets a workspace's own client carry its sign-ins.
func (s *emailService) WireOAuthApps(apps OAuthApps, gate PartnershipGate) {
	s.oauthApps, s.partnership = apps, gate
}

// RequireOwnOAuthApp refuses a flow that can only use this deployment's client on the Partnership plan.
func (s *emailService) RequireOwnOAuthApp(ctx context.Context, orgID uuid.UUID) *errx.Error {
	if s.partnership == nil {
		return nil
	}
	on, xerr := s.partnership.HasPartnership(ctx, orgID)
	if xerr != nil {
		return xerr
	}
	if on {
		return ErrPartnershipOAuthAppRequired
	}
	return nil
}

// signinConfig is the client a new consent for the workspace goes through, and its app id when it is the workspace's own.
func (s *emailService) signinConfig(ctx context.Context, orgID *uuid.UUID, provider models.InboxProvider) (*oauth2.Config, *uuid.UUID, *errx.Error) {
	if orgID != nil && s.oauthApps != nil {
		cfg, id, xerr := s.oauthApps.ConfigFor(ctx, *orgID, provider)
		if xerr != nil {
			return nil, nil, xerr
		}
		if cfg != nil {
			return cfg, id, nil
		}
	}
	if orgID != nil {
		if xerr := s.RequireOwnOAuthApp(ctx, *orgID); xerr != nil {
			return nil, nil, xerr
		}
	}
	cfg, xerr := s.oauthConfigFor(provider)
	return cfg, nil, xerr
}

// grantConfig is the client that issued a stored grant, which is the only one that can refresh it.
func (s *emailService) grantConfig(ctx context.Context, appID *uuid.UUID, provider models.InboxProvider) (*oauth2.Config, *errx.Error) {
	if appID == nil {
		return s.oauthConfigFor(provider)
	}
	if s.oauthApps == nil {
		return nil, errx.InternalError()
	}
	return s.oauthApps.ConfigByID(ctx, *appID)
}

// WorkspaceGrantToken draws an access token for a mailbox whose grant a workspace client issued; handled is false for any other mailbox.
func (s *emailService) WorkspaceGrantToken(ctx context.Context, accountID uuid.UUID) (*oauth2.Token, bool, *errx.Error) {
	creds, xerr := s.emailRepository.GetOAuthCredentials(ctx, accountID)
	if xerr == errx.ErrNotFound || (xerr == nil && (creds == nil || creds.OAuthAppID == nil)) {
		return nil, false, nil
	}
	if xerr != nil {
		return nil, true, xerr
	}
	tok, xerr := s.OAuthAccessToken(ctx, accountID)
	return tok, true, xerr
}
