package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/delegation"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

// ConsentRequest is the body of POST /v1/store/domains/:id/microsoft-consent.
type ConsentRequest struct {
	// OrderID is the order the approval is started from, which the administrator lands back on; optional.
	OrderID string `json:"order_id"`
}

// ConsentLink is where a purchased domain's Microsoft 365 administrator approves Warmbly, or Consented when nothing is left to approve.
type ConsentLink struct {
	URL       *string    `json:"url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Consented bool       `json:"consented"`
}

// MicrosoftConsent opens the tenant consent for a purchased domain. The
// administrator lands back on this dashboard: on the order it was started
// from when that is the workspace's, else on the domains page.
func (s *Service) MicrosoftConsent(ctx context.Context, orgID, userID, domainID uuid.UUID, in ConsentRequest) (*ConsentLink, error) {
	d, err := s.domain(ctx, orgID, domainID)
	if err != nil {
		return nil, err
	}
	if s.Delegator == nil {
		return nil, errx.NewWithIdentifier(errx.Unprocessable, delegation.ErrIDNotConfigured, "Microsoft 365 connections are not set up on this instance.")
	}
	back := strings.TrimRight(config.DashboardBaseURL(ctx), "/") + "/app/emails/domains"
	if id, perr := uuid.Parse(strings.TrimSpace(in.OrderID)); perr == nil {
		if o, err := s.Repo.GetOrder(ctx, orgID, id); err == nil && o != nil {
			back = strings.TrimRight(config.DashboardBaseURL(ctx), "/") + "/app/emails/buy/orders/" + o.ID.String()
		}
	}
	c, xerr := s.Delegator.StartPurchasedMicrosoftConsent(ctx, orgID, userID, d.Name, back)
	if xerr != nil {
		return nil, xerr
	}
	if c.Consented {
		return &ConsentLink{Consented: true}, nil
	}
	u, at := c.URL, c.ExpiresAt.UTC()
	return &ConsentLink{URL: &u, ExpiresAt: &at}, nil
}
