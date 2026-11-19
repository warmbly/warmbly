package connectlink

import (
	"context"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
	"github.com/warmbly/warmbly/internal/pkg/displayname"
)

// PartnerLinks is the reseller side of a connect link a partner asked for; every other link has none.
type PartnerLinks interface {
	// LoginHint pre-fills the provider's sign-in for a partner's link, "" for any other.
	LoginHint(ctx context.Context, linkID uuid.UUID) string
	// Connected hands the partner the mailbox its link connected.
	Connected(ctx context.Context, linkID uuid.UUID, acc *models.Email)
	// ReturnURL is where a partner's link sends the person after the provider; ok is false for any other link.
	ReturnURL(ctx context.Context, linkID uuid.UUID, status string) (string, bool)
}

// WirePartnerLinks connects the partner API's links; the two services are built apart.
func (s *Service) WirePartnerLinks(p PartnerLinks) { s.partner = p }

// Partner return statuses.
const (
	ReturnConnected = "connected"
	ReturnCancelled = "cancelled"
	ReturnError     = "error"
)

// CreateSingle mints a link that connects one mailbox through one provider and lives for ttl, in creator's name.
func (s *Service) CreateSingle(ctx context.Context, orgID, creator uuid.UUID, label string, provider models.InboxProvider, ttl time.Duration) (*models.CreatedClientConnectLink, *errx.Error) {
	base := s.d.BaseURL(ctx, orgID)
	if base == "" {
		return nil, ErrUnavailable
	}
	if provider != models.InboxProviderGoogle && provider != models.InboxProviderOutlook {
		return nil, ErrProviderNotAllowed
	}
	if !s.providerAvailable(provider) {
		return nil, ErrProviderUnavailable
	}
	token, hash, err := newToken()
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	link, xerr := s.d.Repo.Create(ctx, &models.ClientConnectLink{
		OrganizationID:   orgID,
		CreatedBy:        &creator,
		Label:            displayname.CleanOr(label, displayname.Workspace, ""),
		TokenHash:        hash,
		AllowedProviders: []string{string(provider)},
		MaxMailboxes:     1,
		TagIDs:           []uuid.UUID{},
		ExpiresAt:        s.d.Now().Add(ttl),
	})
	if xerr != nil {
		return nil, xerr
	}
	return &models.CreatedClientConnectLink{Link: *s.withStatus(link), URL: URL(base, token)}, nil
}

// withLoginHint adds a partner's address hint to a provider's sign-in URL.
func withLoginHint(authURL, hint string) string {
	if hint == "" {
		return authURL
	}
	u, err := url.Parse(authURL)
	if err != nil {
		return authURL
	}
	q := u.Query()
	q.Set("login_hint", hint)
	u.RawQuery = q.Encode()
	return u.String()
}

// partnerReturn is the partner's return URL for a finished sign-in, "" for a link no partner asked for.
func (s *Service) partnerReturn(ctx context.Context, linkID uuid.UUID, res *Result, xerr *errx.Error) string {
	if s.partner == nil {
		return ""
	}
	status := ReturnError
	switch {
	case xerr == nil && res != nil && res.Account != nil:
		status = ReturnConnected
	case xerr == ErrSigninDeclined:
		status = ReturnCancelled
	}
	u, ok := s.partner.ReturnURL(ctx, linkID, status)
	if !ok {
		return ""
	}
	return u
}
