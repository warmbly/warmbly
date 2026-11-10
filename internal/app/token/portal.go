package token

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/observability/errs"
)

// PortalResolver names the agency holding host as its verified portal domain and where its clients sign in; nil when none.
type PortalResolver func(ctx context.Context, host string) (*uuid.UUID, models.PortalSignIn, error)

// ErrPortalSignInElsewhere refuses a sign-in on a portal whose agency signs clients in on the main address.
func ErrPortalSignInElsewhere() *errx.Error {
	where := "the main dashboard address"
	if origin := config.PrimaryDashboardOrigin(); origin != "" {
		where = origin
	}
	return errx.NewWithIdentifier(errx.Forbidden, "portal_sign_in_elsewhere",
		"This agency's clients sign in at "+where+", which then returns them here.")
}

type portalAgencyKey struct{}

// WithPortalAgency binds every session minted under ctx to agencyID's portal, whatever origin the request came from.
func WithPortalAgency(ctx context.Context, agencyID uuid.UUID) context.Context {
	return context.WithValue(ctx, portalAgencyKey{}, agencyID)
}

// PortalAgencyFromContext is the binding WithPortalAgency put on ctx.
func PortalAgencyFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(portalAgencyKey{}).(uuid.UUID)
	return id, ok && id != uuid.Nil
}

// WirePortalResolver attaches the verified portal lookup sign-in binds sessions with.
func (s *tokenService) WirePortalResolver(r PortalResolver) { s.portals = r }

// portalBinding is the agency a session minted under ctx is held to: an explicit binding, else the
// verified portal origin of the request. A portal origin that cannot be resolved refuses the sign-in.
func (s *tokenService) portalBinding(ctx context.Context) (*uuid.UUID, *errx.Error) {
	if id, ok := PortalAgencyFromContext(ctx); ok {
		return &id, nil
	}
	origin := config.PortalOrigin(config.DashboardOriginFromContext(ctx))
	if origin == "" {
		return nil, nil
	}
	if s.portals == nil {
		return nil, errx.InternalError()
	}
	id, signIn, err := s.portals(ctx, strings.TrimPrefix(origin, "https://"))
	if err != nil {
		errs.CaptureException(err)
		return nil, errx.InternalError()
	}
	if id == nil || *id == uuid.Nil {
		return nil, errx.InternalError()
	}
	// Such a portal receives its sessions only through the main address's handoff, which binds explicitly.
	if signIn == models.PortalSignInWarmbly {
		return nil, ErrPortalSignInElsewhere()
	}
	return id, nil
}
